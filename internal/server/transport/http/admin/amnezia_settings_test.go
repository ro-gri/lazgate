package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestAmneziaSettingsSaveAndPublicStatus(t *testing.T) {
	raw, err := store.OpenSQLite(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	st, err := store.WrapSecrets(raw, "test-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "AWG", Type: model.NodeTypeNativeAmneziaWG})
	if err != nil {
		t.Fatal(err)
	}
	header := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	c, err := st.CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig{NodeID: node.ID, EndpointHost: "vpn.example.com", ServerPrivateKey: "private-secret", ServerPublicKey: "public", HeaderProtectionKey: header, H1: "1", H2: "2", H3: "3", H4: "4", S1: 12, S2: 12, S3: 12, S4: 12})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st}
	body := map[string]any{"config_version_ms": c.ConfigVersionMS, "endpoint_host": "vpn.example.com", "endpoint_port": 51900, "dns": "9.9.9.9", "server_ipv4": "10.8.1.1", "prefix": 24, "mtu": 1280, "persistent_keepalive": 0, "jc": 4, "jmin": 40, "jmax": 70, "s1": 12, "s2": 12, "s3": 12, "s4": 12, "h1": "1", "h2": "2", "h3": "3", "h4": "4", "i1": "<r 2><b 0x1234>", "i2": "", "i3": "", "i4": "", "i5": "", "route_mode": "all", "ipv6_mode": "off", "timings": map[string]string{"RekeyTimeout": "2-5"}}
	request := func(method string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		a.amneziaSettings(w, httptest.NewRequest(method, "/", bytes.NewReader(data)), node.ID)
		return w
	}
	w := request("PUT")
	if w.Code != 202 {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), header) || strings.Contains(w.Body.String(), "private-secret") {
		t.Fatal("response exposes secrets")
	}
	saved, _ := st.GetAmneziaWGNodeConfig(node.ID)
	if saved.ListenPort != 51900 || saved.HeaderProtectionKey != header || saved.I1 != "<r 2><b 0x1234>" {
		t.Fatal("settings not saved")
	}
	crypted, _ := raw.GetAmneziaWGNodeConfig(node.ID)
	if !strings.HasPrefix(crypted.HeaderProtectionKey, "enc:") || !strings.HasPrefix(crypted.ServerPrivateKey, "enc:") {
		t.Fatal("secrets not encrypted")
	}
	if w := request("PUT"); w.Code != 409 {
		t.Fatal("stale update accepted")
	}
	body["config_version_ms"] = saved.ConfigVersionMS
	body["i1"] = "<r 0>"
	if w := request("PUT"); w.Code != 400 {
		t.Fatalf("invalid CPS accepted: %s", w.Body.String())
	}
	status := request("GET")
	if status.Code != 200 || strings.Contains(status.Body.String(), header) {
		t.Fatal("invalid status response")
	}
}
