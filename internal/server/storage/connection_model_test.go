package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"laz/internal/server/model"
)

func TestConnectionDataModelSchema(t *testing.T) {
	st := openTargetSQLite(t)
	assertSQLiteColumns(t, st, "accounts", []string{"id", "username", "display_name", "status", "created_at", "updated_at"})

	expected := []string{
		"accounts", "nodes", "node_instances", "agent_installations",
		"connections", "hysteria2_node_configs", "hysteria2_connection_materials",
		"amneziawg_node_configs", "amneziawg_connection_materials", "issued_configs",
		"account_portal_tokens", "connection_subscription_tokens", "short_links",
		"node_runtime", "node_online_connections", "usage_records", "connection_usage_totals",
	}
	for _, table := range expected {
		if !sqliteTableExists(t, st, table) {
			t.Fatalf("target table %q is missing", table)
		}
	}
	removed := []string{
		"clients", "client_credentials", "access_tokens", "node_online_clients",
		"amneziawg_peer_materials", "amneziawg_server_profiles", "amneziawg_ipv4_pools",
		"amneziawg_docker_profiles", "usage_batches", "event_log",
	}
	for _, table := range removed {
		if sqliteTableExists(t, st, table) {
			t.Fatalf("removed table %q still exists", table)
		}
	}

	assertSQLiteColumns(t, st, "connections", []string{
		"id", "account_id", "node_id", "name", "protocol", "desired_status",
		"applied_status", "desired_version_ms", "applied_version_ms", "last_error_id",
		"created_at", "updated_at",
	})
	assertSQLiteColumns(t, st, "usage_records", []string{"request_id", "connection_id", "node_instance_id", "from_ms", "to_ms", "received_at_ms", "tx_bytes", "rx_bytes"})
	assertSQLiteColumns(t, st, "node_online_connections", []string{"node_instance_id", "native_auth_id", "connection_id", "count", "first_seen_at", "last_seen_at"})
	assertSQLiteColumns(t, st, "hysteria2_connection_materials", []string{"connection_id", "password_encrypted", "password_hash", "created_at", "updated_at"})

	// Username is deliberately not a key: different accounts may use the same display login.
	if _, err := st.CreateAccount(model.Account{Username: "same", DisplayName: "One"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(model.Account{Username: "same", DisplayName: "Two"}); err != nil {
		t.Fatalf("duplicate username must be accepted: %v", err)
	}
}

func TestConnectionLifecycleAndHysteriaProjection(t *testing.T) {
	st := openTargetSQLite(t)
	account := mustCreateAccount(t, st, "alice")
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "h2")
	if _, err := st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "hy.example", EndpointPort: 443}); err != nil {
		t.Fatal(err)
	}

	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection:        model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "secret-password", PasswordHash: "secret-password-hash"},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Phone", Config: "hysteria2://secret-password@hy.example:443"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection := bundle.Connection
	if connection.DesiredStatus != model.ConnectionStatusActive || connection.AppliedStatus != model.ConnectionStatusAbsent || !connection.Pending() {
		t.Fatalf("unexpected initial lifecycle: %+v", connection)
	}

	_, err = st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Phone"))
	if !errors.Is(err, ErrDuplicateConnection) {
		t.Fatalf("expected duplicate active connection, got %v", err)
	}

	projected, err := st.ListHysteriaSyncConnections(context.Background(), node.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 1 || projected[0].ConnectionID != connection.ID || !projected[0].Active || projected[0].Material != "secret-password-hash" {
		t.Fatalf("unexpected Hysteria projection: %+v", projected)
	}

	held, err := st.UpdateConnectionDesiredStatus(connection.ID, model.ConnectionStatusHeld)
	if err != nil {
		t.Fatal(err)
	}
	projected, err = st.ListHysteriaSyncConnections(context.Background(), node.ID, "")
	if err != nil || len(projected) != 1 || projected[0].Active {
		t.Fatalf("held connection must project as inactive: %+v, %v", projected, err)
	}
	finalized, err := st.FinalizeConnectionsForAuthSnapshot(node.ID, account.ID, held.DesiredVersionMS)
	if err != nil || len(finalized) != 1 || finalized[0].AppliedStatus != model.ConnectionStatusHeld || finalized[0].Pending() {
		t.Fatalf("failed to finalize hold: %+v, %v", finalized, err)
	}

	deleted, err := st.UpdateConnectionDesiredStatus(connection.ID, model.ConnectionStatusDeleted)
	if err != nil {
		t.Fatal(err)
	}
	// The partial unique index releases the name as soon as deletion is desired.
	if _, err = st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Phone")); err != nil {
		t.Fatalf("connection name must be reusable after desired delete: %v", err)
	}
	if _, err = st.FinalizeConnectionsForAuthSnapshot(node.ID, account.ID, deleted.DesiredVersionMS); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range st.ListIssuedConfigs() {
		if cfg.ConnectionID == connection.ID && cfg.Status != model.StatusDeleted {
			t.Fatalf("deleted connection config remains active: %+v", cfg)
		}
	}
}

func TestAmneziaWGProjectionUsesConnectionIdentity(t *testing.T) {
	st := openTargetSQLite(t)
	account := mustCreateAccount(t, st, "bob")
	node := mustCreateNode(t, st, model.NodeTypeNativeAmneziaWG, "awg")
	if _, err := st.CreateAmneziaWGNodeConfig(testAmneziaWGNodeConfig(node.ID)); err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection: model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "TV", Protocol: model.ProtocolAmneziaWG},
		AmneziaWGMaterial: &model.AmneziaWGConnectionMaterial{
			ClientPublicKey: "client-public", ClientPrivateKey: "client-private", PresharedKey: "psk", AssignedIPv4: "10.8.1.2",
		},
		Configs: []model.IssuedConfig{{Kind: model.ConfigAmneziaConf, Name: "TV", Config: "[Interface]\nPrivateKey = client-private"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.ListAmneziaWGSyncConnections(context.Background(), node.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ConnectionID != bundle.Connection.ID || items[0].Material.Material.ConnectionID != bundle.Connection.ID || !items[0].Active {
		t.Fatalf("unexpected AmneziaWG projection: %+v", items)
	}
	if items[0].Material.Material.AssignedIPv4 != "10.8.1.2" || items[0].Material.NodeConfig.NodeID != node.ID {
		t.Fatalf("incomplete AmneziaWG projection: %+v", items[0].Material)
	}
}

func TestConnectionBundleEnforcesProtocolMaterialAndPersistsExternalReferenceAtomically(t *testing.T) {
	st := openTargetSQLite(t)
	account := mustCreateAccount(t, st, "bundle")
	h2 := mustCreateNode(t, st, model.NodeTypeNativeHy2, "bundle-h2")
	if _, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection: model.Connection{AccountID: account.ID, NodeID: h2.ID, Name: "Broken", Protocol: model.ProtocolHysteria2},
		Configs:    []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Broken", Config: "hy2://broken"}},
	}); err == nil {
		t.Fatal("native Hysteria2 bundle without material must be rejected")
	}
	if _, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection:        model.Connection{AccountID: account.ID, NodeID: h2.ID, Name: "Missing hash", Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "secret"},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Missing hash", Config: "hy2://broken"}},
	}); err == nil {
		t.Fatal("native Hysteria2 bundle without password hash must be rejected")
	}

	external := mustCreateNode(t, st, model.NodeTypeAmneziaAPI, "bundle-api")
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection:    model.Connection{AccountID: account.ID, NodeID: external.ID, Name: "External", Protocol: model.ProtocolAmneziaWG},
		AmneziaAPIRef: &model.AmneziaAPIConnectionRef{RemoteID: "remote-1", RemoteName: "External"},
		Configs:       []model.IssuedConfig{{Kind: model.ConfigAmneziaVPN, Name: "External", Config: "external-config"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := st.GetAmneziaAPIConnectionRef(bundle.Connection.ID)
	if err != nil || ref.ConnectionID != bundle.Connection.ID || ref.RemoteID != "remote-1" {
		t.Fatalf("external reference was not committed with connection bundle: %+v err=%v", ref, err)
	}
}

func TestTelemetryIsBoundToNodeInstanceAndConnection(t *testing.T) {
	st := openTargetSQLite(t)
	account := mustCreateAccount(t, st, "telemetry")
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "telemetry-h2")
	instance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	installation, err := st.CreateAgentInstallation(model.AgentInstallation{NodeInstanceID: instance.ID, AgentType: "hysteria2", CertificateFingerprintSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	installationRecord, err := st.LookupActiveAgentInstallation(strings.ToUpper(installation.CertificateFingerprintSHA256))
	if err != nil {
		t.Fatal(err)
	}
	if installationRecord.ID != installation.ID || installationRecord.NodeID != node.ID || installationRecord.AgentType != installation.AgentType {
		t.Fatalf("unexpected installation lookup projection: %+v", installationRecord)
	}
	connectionBundle, err := st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Laptop"))
	if err != nil {
		t.Fatal(err)
	}
	connection := connectionBundle.Connection

	firstSeen := time.Now().UTC().Add(-time.Minute)
	lastSeen := time.Now().UTC()
	if err = st.UpsertNodeOnlineConnections(instance.ID, []model.NodeOnlineConnection{{ConnectionID: connection.ID, NativeAuthID: "native", Count: 1, FirstSeenAt: firstSeen, LastSeenAt: lastSeen}}); err != nil {
		t.Fatal(err)
	}
	if err = st.UpsertNodeOnlineConnections(instance.ID, []model.NodeOnlineConnection{{ConnectionID: connection.ID, NativeAuthID: "native", Count: 2, LastSeenAt: lastSeen.Add(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	online := st.ListNodeOnlineConnections(instance.ID)
	if len(online) != 1 || online[0].ConnectionID != connection.ID || online[0].Count != 2 || !online[0].FirstSeenAt.Equal(firstSeen) {
		t.Fatalf("unexpected online state: %+v", online)
	}
	if err = st.UpsertNodeOnlineConnections(instance.ID, []model.NodeOnlineConnection{{NativeAuthID: "unknown-a", Count: 1, LastSeenAt: lastSeen}, {NativeAuthID: "unknown-b", Count: 2, LastSeenAt: lastSeen}}); err != nil {
		t.Fatal(err)
	}
	unknown := st.ListNodeOnlineConnections(instance.ID)
	if len(unknown) != 2 || unknown[0].ConnectionID != "" || unknown[1].ConnectionID != "" {
		t.Fatalf("unknown online: %+v", unknown)
	}

	totals, duplicate, err := st.ApplyUsageRecords("request-1", instance.ID, []model.UsageRecord{{ConnectionID: connection.ID, FromMS: 100, ToMS: 200, TXBytes: 10, RXBytes: 20}})
	if err != nil || duplicate {
		t.Fatalf("first usage report: duplicate=%v err=%v", duplicate, err)
	}
	if len(totals) != 1 || totals[0].TXBytes != 10 || totals[0].RXBytes != 20 {
		t.Fatalf("unexpected authoritative totals: %+v", totals)
	}
	totals, duplicate, err = st.ApplyUsageRecords("request-1", instance.ID, []model.UsageRecord{{ConnectionID: connection.ID, TXBytes: 999, RXBytes: 999}})
	if err != nil || !duplicate {
		t.Fatalf("repeated usage report must deduplicate: duplicate=%v err=%v", duplicate, err)
	}
	if len(totals) != 1 || totals[0].TXBytes != 10 || totals[0].RXBytes != 20 {
		t.Fatalf("duplicate changed authoritative totals: %+v", totals)
	}
	records := st.ListUsageRecords()
	if len(records) != 1 || records[0].ConnectionID != connection.ID || records[0].NodeInstanceID != instance.ID || records[0].FromMS != 100 || records[0].ToMS != 200 || records[0].TotalBytes() != 30 {
		t.Fatalf("unexpected usage records: %+v", records)
	}

	if err = st.UpsertNodeRuntime(model.NodeRuntime{AgentInstallationID: installation.ID, NodeInstanceID: instance.ID, AgentStatus: "online", ServiceStatus: "running"}); err != nil {
		t.Fatal(err)
	}
	runtime, err := st.GetNodeRuntime(installation.ID)
	if err != nil || runtime.NodeInstanceID != instance.ID || runtime.ServiceStatus != "running" {
		t.Fatalf("unexpected runtime: %+v, %v", runtime, err)
	}
	otherInstance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.11"})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpsertNodeRuntime(model.NodeRuntime{AgentInstallationID: installation.ID, NodeInstanceID: otherInstance.ID, AgentStatus: "online"}); err == nil {
		t.Fatal("runtime must not be attached to an instance different from its installation")
	}
	command, err := st.CreateRuntimeCommand(model.RuntimeCommand{AgentInstallationID: installation.ID, Type: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if pending := st.ListPendingRuntimeCommands(installation.ID); len(pending) != 1 || pending[0].ID != command.ID {
		t.Fatalf("runtime command is not bound to its agent installation: %+v", pending)
	}
	if err = st.CompleteRuntimeCommandForInstallation(installation.ID, command.ID, model.RuntimeCommandStatusCompleted, "done", ""); err != nil {
		t.Fatal(err)
	}
	if pending := st.ListPendingRuntimeCommands(installation.ID); len(pending) != 0 {
		t.Fatalf("completed runtime command remains pending: %+v", pending)
	}

	otherNode := mustCreateNode(t, st, model.NodeTypeNativeHy2, "other-h2")
	otherBundle, err := st.CreateConnectionBundle(testHysteriaBundle(account.ID, otherNode.ID, "Other"))
	if err != nil {
		t.Fatal(err)
	}
	otherConnection := otherBundle.Connection
	if err = st.UpsertNodeOnlineConnections(instance.ID, []model.NodeOnlineConnection{{ConnectionID: otherConnection.ID, NativeAuthID: "native", Count: 1, LastSeenAt: lastSeen}}); err == nil {
		t.Fatal("cross-node online connection must be rejected")
	}
	if _, _, err = st.ApplyUsageRecords("request-cross-node", instance.ID, []model.UsageRecord{{ConnectionID: otherConnection.ID, FromMS: 200, ToMS: 300, TXBytes: 1}}); err == nil {
		t.Fatal("cross-node usage must be rejected")
	}
}

func TestSensitiveConnectionDataEncryptedAtRest(t *testing.T) {
	raw := openTargetSQLite(t)
	wrapped, err := WrapSecrets(raw, "storage-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	account := mustCreateAccount(t, wrapped, "secure")
	node := mustCreateNode(t, wrapped, model.NodeTypeNativeHy2, "secure-h2")
	if _, err = wrapped.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.20", SSHKey: "PRIVATE SSH KEY"}); err != nil {
		t.Fatal(err)
	}
	if _, err = wrapped.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "secure.example", ObfsEnabled: true, ObfsPassword: "obfs-secret"}); err != nil {
		t.Fatal(err)
	}
	externalNode, _, err := wrapped.CreateAmneziaAPINode(model.Node{Name: "secure-amnezia-api", Type: model.NodeTypeAmneziaAPI}, model.AmneziaAPINodeConfig{BaseURL: "https://amnezia.example", APIKey: "api-secret"})
	if err != nil {
		t.Fatal(err)
	}
	awgConfig := testAmneziaWGNodeConfig("")
	awgConfig.ServerPrivateKey = "awg-server-private"
	awgNode, _, err := wrapped.CreateNativeAmneziaWGNode(model.Node{Name: "secure-awg", Type: model.NodeTypeNativeAmneziaWG}, awgConfig)
	if err != nil {
		t.Fatal(err)
	}
	awgBundle, err := wrapped.CreateConnectionBundle(model.ConnectionBundle{
		Connection: model.Connection{AccountID: account.ID, NodeID: awgNode.ID, Name: "Secure AWG", Protocol: model.ProtocolAmneziaWG},
		AmneziaWGMaterial: &model.AmneziaWGConnectionMaterial{
			ClientPublicKey: "awg-client-public", ClientPrivateKey: "awg-client-private", PresharedKey: "awg-psk", AssignedIPv4: "10.8.1.2",
		},
		Configs: []model.IssuedConfig{{Kind: model.ConfigAmneziaConf, Name: "Secure AWG", Config: "PrivateKey = awg-client-private"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := wrapped.CreateConnectionBundle(model.ConnectionBundle{
		Connection:        model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Secure", Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "vpn-secret", PasswordHash: "vpn-secret-hash"},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Secure", Config: "hysteria2://vpn-secret@secure.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	portal, err := wrapped.CreateAccountPortalToken(model.AccountPortalToken{AccountID: account.ID, Token: "portal-secret", TokenHash: "portal-hash"})
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := wrapped.CreateConnectionSubscriptionToken(model.ConnectionSubscriptionToken{ConnectionID: bundle.Connection.ID, Token: "subscription-secret", TokenHash: "subscription-hash"})
	if err != nil {
		t.Fatal(err)
	}
	if portal.Token != "portal-secret" || subscription.Token != "subscription-secret" || bundle.Hysteria2Material.Password != "vpn-secret" || bundle.Hysteria2Material.PasswordHash != "vpn-secret-hash" || bundle.Configs[0].Config == "" {
		t.Fatal("secure store did not return plaintext values")
	}

	assertEncryptedColumn(t, raw, `select ssh_key from node_instances limit 1`, "PRIVATE SSH KEY")
	assertEncryptedColumn(t, raw, `select obfs_password_encrypted from hysteria2_node_configs where node_id = ?`, "obfs-secret", node.ID)
	assertEncryptedColumn(t, raw, `select api_key_encrypted from amnezia_api_node_configs where node_id = ?`, "api-secret", externalNode.ID)
	assertEncryptedColumn(t, raw, `select server_private_key_encrypted from amneziawg_node_configs where node_id = ?`, "awg-server-private", awgNode.ID)
	assertEncryptedColumn(t, raw, `select client_private_key_encrypted from amneziawg_connection_materials where connection_id = ?`, "awg-client-private", awgBundle.Connection.ID)
	assertEncryptedColumn(t, raw, `select preshared_key_encrypted from amneziawg_connection_materials where connection_id = ?`, "awg-psk", awgBundle.Connection.ID)
	assertEncryptedColumn(t, raw, `select password_encrypted from hysteria2_connection_materials where connection_id = ?`, "vpn-secret", bundle.Connection.ID)
	var storedPasswordHash string
	if err = raw.queryRow(`select password_hash from hysteria2_connection_materials where connection_id = ?`, bundle.Connection.ID).Scan(&storedPasswordHash); err != nil || storedPasswordHash != "vpn-secret-hash" {
		t.Fatalf("unexpected stored Hysteria password hash: %q, %v", storedPasswordHash, err)
	}
	assertEncryptedColumn(t, raw, `select config_encrypted from issued_configs where connection_id = ?`, "hysteria2://vpn-secret@secure.example", bundle.Connection.ID)
	assertEncryptedColumn(t, raw, `select token_encrypted from account_portal_tokens where id = ?`, "portal-secret", portal.ID)
	assertEncryptedColumn(t, raw, `select token_encrypted from connection_subscription_tokens where id = ?`, "subscription-secret", subscription.ID)
}

func openTargetSQLite(t *testing.T) *SQLStore {
	t.Helper()
	st, err := OpenSQLite(t.TempDir() + "/laz.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mustCreateAccount(t *testing.T, st interface {
	CreateAccount(model.Account) (model.Account, error)
}, username string) model.Account {
	t.Helper()
	v, err := st.CreateAccount(model.Account{Username: username, DisplayName: username})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func mustCreateNode(t *testing.T, st interface {
	CreateNode(model.Node) (model.Node, error)
}, nodeType model.NodeType, name string) model.Node {
	t.Helper()
	v, err := st.CreateNode(model.Node{Name: name, Type: nodeType, Region: "FI-Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testAmneziaWGNodeConfig(nodeID string) model.AmneziaWGNodeConfig {
	return model.AmneziaWGNodeConfig{
		NodeID: nodeID, EndpointHost: "awg.example", EndpointPort: 51820, ListenPort: 51820,
		ServerIPv4: "10.8.1.1", ClientIPv4CIDR: "10.8.1.0/24", FirstClientIPv4: "10.8.1.2", LastClientIPv4: "10.8.1.254",
		DNS: "1.1.1.1", MTU: 1376, PersistentKeepalive: 25, ClientRouteAllowedIPs: "0.0.0.0/0",
		ServerPrivateKey: "server-private", ServerPublicKey: "server-public",
		JC: 4, JMin: 40, JMax: 70, S1: 16, S2: 97, S3: 33, S4: 22,
		H1: "100", H2: "200", H3: "300", H4: "400", I1: "<r 11>", I2: "<r 22>", I3: "<r 33>", I4: "<r 44>", I5: "<r 55>",
	}
}

func testHysteriaBundle(accountID, nodeID, name string) model.ConnectionBundle {
	return model.ConnectionBundle{
		Connection:        model.Connection{AccountID: accountID, NodeID: nodeID, Name: name, Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "password-" + name, PasswordHash: "password-hash-" + name},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: name, Config: "hy2://" + name}},
	}
}

func sqliteTableExists(t *testing.T, st *SQLStore, table string) bool {
	t.Helper()
	var count int
	if err := st.db.QueryRow(`select count(*) from sqlite_master where type = 'table' and name = ?`, table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func assertSQLiteColumns(t *testing.T, st *SQLStore, table string, expected []string) {
	t.Helper()
	rows, err := st.db.Query(`pragma table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, name)
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("columns for %s = %v, want %v", table, actual, expected)
	}
}

func assertEncryptedColumn(t *testing.T, st *SQLStore, query, plaintext string, args ...any) {
	t.Helper()
	var stored string
	if err := st.db.QueryRow(query, args...).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == plaintext || !strings.HasPrefix(stored, encryptedPrefix) {
		t.Fatalf("secret is not encrypted at rest: %q", stored)
	}
}
