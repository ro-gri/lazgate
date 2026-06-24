package connections

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	transportstore "laz/internal/queue"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
	"laz/internal/server/workqueue"

	"golang.org/x/crypto/bcrypt"
)

func TestNativeConnectionCreationPersistsBundleAndQueuesRefresh(t *testing.T) {
	tests := []struct {
		name     string
		protocol model.Protocol
		nodeType model.NodeType
		prepare  func(*testing.T, *store.SQLStore, model.Node)
		verify   func(*testing.T, *store.SQLStore, ConnectionResult)
	}{
		{
			name: "Hysteria2", protocol: model.ProtocolHysteria2, nodeType: model.NodeTypeNativeHy2,
			prepare: func(t *testing.T, st *store.SQLStore, node model.Node) {
				if _, err := st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "h2.example", EndpointPort: 443}); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, st *store.SQLStore, result ConnectionResult) {
				material, err := st.GetHysteria2ConnectionMaterial(result.Connection.ID)
				if err != nil || material.Password != "test-password" {
					t.Fatalf("unexpected Hysteria material: %+v err=%v", material, err)
				}
				if err = bcrypt.CompareHashAndPassword([]byte(material.PasswordHash), []byte(material.Password)); err != nil {
					t.Fatalf("Hysteria password hash does not match password: %v", err)
				}
				projected, err := st.ListHysteriaSyncConnections(context.Background(), result.Connection.NodeID, "")
				if err != nil || len(projected) != 1 || projected[0].Material != material.PasswordHash {
					t.Fatalf("Hysteria manifest projection does not reuse stored hash: %+v err=%v", projected, err)
				}
				if len(result.Configs) != 1 || !strings.HasPrefix(result.Configs[0].Config, "hy2://") {
					t.Fatalf("unexpected Hysteria config: %+v", result.Configs)
				}
			},
		},
		{
			name: "AmneziaWG", protocol: model.ProtocolAmneziaWG, nodeType: model.NodeTypeNativeAmneziaWG,
			prepare: func(t *testing.T, st *store.SQLStore, node model.Node) {
				if _, err := st.CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig{NodeID: node.ID, EndpointHost: "awg.example", EndpointPort: 51820, ListenPort: 51820, ServerIPv4: "10.8.1.1", ClientIPv4CIDR: "10.8.1.0/24", FirstClientIPv4: "10.8.1.2", LastClientIPv4: "10.8.1.4", DNS: "1.1.1.1", MTU: 1376, PersistentKeepalive: 25, ClientRouteAllowedIPs: "0.0.0.0/0", ServerPrivateKey: "server-private", ServerPublicKey: "server-public", JC: 4, JMin: 40, JMax: 70, S1: 16, S2: 97, S3: 33, S4: 22, H1: "100", H2: "200", H3: "300", H4: "400", I1: "<r 11>", I2: "<r 22>", I3: "<r 33>", I4: "<r 44>", I5: "<r 55>"}); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, st *store.SQLStore, result ConnectionResult) {
				material, err := st.GetAmneziaWGConnectionMaterial(result.Connection.ID)
				if err != nil || material.ConnectionID != result.Connection.ID || material.AssignedIPv4 != "10.8.1.2" || material.ClientPrivateKey == "" || material.ClientPublicKey == "" || material.PresharedKey == "" {
					t.Fatalf("unexpected AmneziaWG material: %+v err=%v", material, err)
				}
				if len(result.Configs) != 1 || !strings.Contains(result.Configs[0].Config, "Address = 10.8.1.2/32") || !strings.Contains(result.Configs[0].Config, "Jc = 4") || !strings.Contains(result.Configs[0].Config, "I5 = <r 55>") {
					t.Fatalf("unexpected AmneziaWG config: %+v", result.Configs)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.OpenSQLite(t.TempDir() + "/laz.db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			transport, err := transportstore.OpenSQLite(t.TempDir() + "/transport.db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = transport.Close() })
			account, err := st.CreateAccount(model.Account{Username: "account", DisplayName: "Account"})
			if err != nil {
				t.Fatal(err)
			}
			node, err := st.CreateNode(model.Node{Name: "node-" + tc.name, Type: tc.nodeType})
			if err != nil {
				t.Fatal(err)
			}
			tc.prepare(t, st, node)
			service := New(st, func() (string, error) { return "test-password", nil })
			service.SetTransport(transport)

			result, err := service.CreateConnection(context.Background(), ConnectionInput{AccountID: account.ID, Node: node, Protocol: tc.protocol, Name: "Phone"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Connection.ID == "" || result.Connection.AppliedStatus != model.ConnectionStatusAbsent || !result.Connection.Pending() {
				t.Fatalf("unexpected new connection: %+v", result.Connection)
			}
			if len(result.Configs) != 1 || result.Configs[0].Name != "Phone "+node.Name {
				t.Fatalf("unexpected generated config name: %+v", result.Configs)
			}
			if _, err := service.SelectEnrollmentNodes(nil); err == nil {
				t.Fatal("enrollment must require explicit node IDs")
			}
			tc.verify(t, st, result)

			messages, err := transport.LeasePending(context.Background(), "server", 10, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || messages[0].Type != workqueue.TypeNodeAuthRefresh {
				t.Fatalf("unexpected outbox: %+v", messages)
			}
			var payload workqueue.AuthRefreshPayload
			if err = json.Unmarshal(messages[0].Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.NodeID != node.ID || payload.AccountID != account.ID || payload.SnapshotVersionMS != result.Connection.DesiredVersionMS {
				t.Fatalf("unexpected refresh payload: %+v", payload)
			}
		})
	}
}

func TestDefaultConfigName(t *testing.T) {
	for _, tc := range []struct{ region, connection, server, want string }{
		{"FI", "Phone", "Reserve", "Phone 🇫🇮 Reserve"},
		{"NL-Amsterdam", "Mac", "Primary", "Mac 🇳🇱 Primary"},
		{"", "Phone", "Reserve", "Phone Reserve"},
		{"unknown", "Phone", "Reserve", "Phone Reserve"},
		{" fi ", " Phone ", " Reserve ", "Phone 🇫🇮 Reserve"},
	} {
		if got := ConfigName(model.Node{Name: tc.server, Region: tc.region}, tc.connection); got != tc.want {
			t.Errorf("region %q: got %q, want %q", tc.region, got, tc.want)
		}
	}
}
