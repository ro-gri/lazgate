package admin

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/protocol/hy2config"
	"laz/internal/server/hysteriasettings"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestHysteriaSettingsSaveSecretsConflictAndAcknowledgement(t *testing.T) {
	raw, err := store.OpenSQLite(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	st, err := store.WrapSecrets(raw, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "H2", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	settings := hy2config.Defaults()
	settings.Obfs.Type = "salamander"
	settings.Obfs.Password = "secret-obfs"
	settings.ListenPort = 443
	encoded, _ := json.Marshal(settings)
	c, err := st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "h2.example.com", EndpointPort: 18443, SettingsJSON: string(encoded), ObfsEnabled: true, ObfsType: "salamander", ObfsPassword: settings.Obfs.Password})
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "user", DisplayName: "User"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{Connection: model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolHysteria2}, Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "connection-secret", PasswordHash: "hash"}, Configs: []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Phone H2", Config: "old"}}})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st}
	request := func(method string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		a.hysteriaSettings(w, httptest.NewRequest(method, "/", bytes.NewReader(b)), node.ID)
		return w
	}
	get := request("GET", nil)
	if get.Code != 200 || strings.Contains(get.Body.String(), "secret-obfs") {
		t.Fatalf("%d %s", get.Code, get.Body.String())
	}
	var view hysteriasettings.View
	if err = json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	view.Settings.BlockedCountries = []string{"FI"}
	view.Settings.QUIC.MaxIdleTimeout = "20s"
	result := request("PUT", view.Input)
	if result.Code != 202 {
		t.Fatalf("%d %s", result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), "connection-secret") {
		t.Fatal("connection secret leaked")
	}
	configs := st.ListIssuedConfigs()
	if len(configs) != 1 || configs[0].ConnectionID != bundle.Connection.ID || !strings.Contains(configs[0].Config, "@h2.example.com:18443/") || !strings.Contains(configs[0].Config, "#Phone+H2") {
		t.Fatalf("client configuration not regenerated with the full name: %+v", configs)
	}
	saved, err := st.GetHysteria2NodeConfig(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ObfsPassword != "secret-obfs" || saved.AppliedConfigVersionMS != 0 {
		t.Fatal("lost secret or prematurely acknowledged")
	}
	decoded, err := hy2config.Decode(saved.SettingsJSON)
	if err != nil || decoded.ListenPort != 443 || decoded.QUIC.MaxIdleTimeout != "20s" {
		t.Fatalf("settings: %+v %v", decoded, err)
	}
	encrypted, _ := raw.GetHysteria2NodeConfig(node.ID)
	if !strings.HasPrefix(encrypted.SettingsJSON, "enc:v1:") {
		t.Fatal("unencrypted settings")
	}
	if w := request("PUT", view.Input); w.Code != 409 {
		t.Fatal("stale version accepted")
	}
	_ = st.SetHysteria2ConfigResult(node.ID, c.ConfigVersionMS, "")
	unchanged, _ := st.GetHysteria2NodeConfig(node.ID)
	if unchanged.AppliedConfigVersionMS != 0 {
		t.Fatal("stale ACK accepted")
	}
	_ = st.SetHysteria2ConfigResult(node.ID, saved.ConfigVersionMS, "")
	applied, _ := st.GetHysteria2NodeConfig(node.ID)
	if applied.AppliedConfigVersionMS != saved.ConfigVersionMS {
		t.Fatal("ACK not stored")
	}
	view.Version = saved.ConfigVersionMS
	view.Settings.QUIC.MaxIdleTimeout = "0s"
	if w := request("PUT", view.Input); w.Code != 400 {
		t.Fatalf("invalid config accepted %d %s", w.Code, w.Body.String())
	}
	if w := request("PUT", map[string]any{"config_version_ms": saved.ConfigVersionMS, "auth": map[string]any{"type": "password"}}); w.Code != 400 {
		t.Fatal("integration edit accepted")
	}
}
