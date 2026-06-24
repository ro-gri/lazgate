package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestManualNativeAmneziaWGRegistrationIsRejected(t *testing.T) {
	app := &App{}
	request := httptest.NewRequest(http.MethodPost, "/admin/api/v1/nodes", strings.NewReader(`{"name":"manual","type":"native_amneziawg"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.nodes(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("manual registration returned %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "provisioning endpoint") {
		t.Fatalf("missing supported installation flow: %s", response.Body.String())
	}
}

func TestNodeDetailsExposeOnlyPublicAmneziaSettings(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	node, err := st.CreateNode(model.Node{Name: "AWG", Type: model.NodeTypeNativeAmneziaWG})
	if err != nil {
		t.Fatal(err)
	}
	config := model.AmneziaWGNodeConfig{NodeID: node.ID, EndpointHost: "vpn.example.com", EndpointPort: 51820, ListenPort: 51820, ServerIPv4: "10.8.1.1", ClientIPv4CIDR: "10.8.1.0/24", MTU: 1280, JC: 5, DNS: "1.1.1.1", ServerPrivateKey: "secret-server-private", ServerPublicKey: "server-public", HeaderProtectionKey: "secret-header-key"}
	config.JMin, config.JMax = 48, 272
	config.S1, config.S2, config.S3, config.S4 = 124, 16, 23, 20
	config.H1, config.H2, config.H3, config.H4 = "1", "2", "3", "4"
	if _, err := st.CreateAmneziaWGNodeConfig(config); err != nil {
		t.Fatal(err)
	}
	app := &App{store: st}
	response := httptest.NewRecorder()
	app.nodeDetails(response, httptest.NewRequest(http.MethodGet, "/admin/api/v1/nodes/"+node.ID, nil), node.ID)
	if response.Code != http.StatusOK {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
	for _, secret := range []string{config.ServerPrivateKey, config.HeaderProtectionKey, "server_private_key", `"header_protection_key":`} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	var payload struct {
		Settings struct {
			EndpointHost           string `json:"endpoint_host"`
			HeaderProtectionKeySet bool   `json:"header_protection_key_set"`
			JC                     int    `json:"jc"`
		} `json:"amnezia_settings"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Settings.EndpointHost != config.EndpointHost || payload.Settings.JC != 5 || !payload.Settings.HeaderProtectionKeySet {
		t.Fatalf("settings not populated: %+v", payload.Settings)
	}
	after, err := st.GetAmneziaWGNodeConfig(node.ID)
	if err != nil || after.ServerPrivateKey != config.ServerPrivateKey || after.HeaderProtectionKey != config.HeaderProtectionKey {
		t.Fatal("reading settings changed keys")
	}
}
