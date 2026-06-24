package amneziasettings

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func fixture(t *testing.T) (store.Store, model.AmneziaWGNodeConfig, model.Connection) {
	t.Helper()
	raw, err := store.OpenSQLite(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	st, err := store.WrapSecrets(raw, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "AWG", Type: model.NodeTypeNativeAmneziaWG})
	if err != nil {
		t.Fatal(err)
	}
	config, err := st.CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig{NodeID: node.ID, EndpointHost: "vpn.example.com", EndpointPort: 51820, ServerPrivateKey: "private", ServerPublicKey: "public", HeaderProtectionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), JC: 4, JMin: 40, JMax: 70, S1: 12, S2: 12, S3: 12, S4: 12, H1: "1", H2: "2", H3: "3", H4: "4"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "test"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{ExpectedNodeConfigVersionMS: config.ConfigVersionMS, Connection: model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolAmneziaWG}, AmneziaWGMaterial: &model.AmneziaWGConnectionMaterial{ClientPrivateKey: "client-private", ClientPublicKey: "client-public", PresharedKey: "psk", AssignedIPv4: "10.8.1.2"}, Configs: []model.IssuedConfig{{Kind: model.ConfigAmneziaConf, Name: "Phone", Config: "old", Slug: "awg-conf"}}})
	if err != nil {
		t.Fatal(err)
	}
	return st, config, bundle.Connection
}

func TestSaveReissuesProfilesAndTracksAppliedRevision(t *testing.T) {
	st, c, connection := fixture(t)
	version := c.ConfigVersionMS
	c.H1 = "100-200"
	c.I1 = "<r 2><b 0x0100>"
	c.DNS = "9.9.9.9"
	c.EndpointPort = 51900
	c.PersistentKeepalive = 0
	c.TimingsJSON = `{"RekeyTimeout":"3-7"}`
	c.RandomTrailers = true
	saved, err := Save(st, c, version)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfigVersionMS <= version || saved.AppliedConfigVersionMS >= saved.ConfigVersionMS {
		t.Fatal("wrong revision state")
	}
	loaded, err := st.GetAmneziaWGNodeConfig(c.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.H1 != c.H1 || loaded.HeaderProtectionKey != c.HeaderProtectionKey || loaded.ListenPort != 51900 {
		t.Fatal("configuration changed in storage")
	}
	var found bool
	for _, issued := range st.ListIssuedConfigs() {
		if issued.ConnectionID == connection.ID {
			found = true
			for _, value := range []string{"I1 = <r 2><b 0x0100>", "H1 = 100-200", "DNS = 9.9.9.9", "Endpoint = vpn.example.com:51900", "PersistentKeepalive = 0", "RekeyTimeout = 3-7"} {
				if !strings.Contains(issued.Config, value) {
					t.Fatalf("missing %s", value)
				}
			}
		}
	}
	if !found {
		t.Fatal("missing issued configuration")
	}
	if err := st.SetAmneziaWGConfigResult(c.NodeID, version, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _ = st.GetAmneziaWGNodeConfig(c.NodeID)
	if loaded.AppliedConfigVersionMS == saved.ConfigVersionMS {
		t.Fatal("stale ACK applied new revision")
	}
	if err := st.SetAmneziaWGConfigResult(c.NodeID, saved.ConfigVersionMS, ""); err != nil {
		t.Fatal(err)
	}
	loaded, _ = st.GetAmneziaWGNodeConfig(c.NodeID)
	if loaded.AppliedConfigVersionMS != saved.ConfigVersionMS {
		t.Fatal("missing ACK revision")
	}
	if _, err := Save(st, c, version); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
}

func TestRejectConflictingPoolAndInvalidParameters(t *testing.T) {
	for _, update := range []func(*model.AmneziaWGNodeConfig){func(c *model.AmneziaWGNodeConfig) { c.ServerIPv4 = "10.8.1.2" }, func(c *model.AmneziaWGNodeConfig) { c.ServerIPv4 = "10.9.0.1"; c.ClientIPv4CIDR = "10.9.0.0/24" }, func(c *model.AmneziaWGNodeConfig) { c.H1 = "2-4" }, func(c *model.AmneziaWGNodeConfig) { c.I1 = "<r 0>" }, func(c *model.AmneziaWGNodeConfig) { c.S1 = 11 }} {
		st, c, _ := fixture(t)
		version := c.ConfigVersionMS
		update(&c)
		if _, err := Save(st, c, version); err == nil {
			t.Fatal("invalid configuration accepted")
		}
		loaded, _ := st.GetAmneziaWGNodeConfig(c.NodeID)
		if loaded.ConfigVersionMS != version {
			t.Fatal("invalid update persisted")
		}
	}
}

func TestRouteExclusionUsesIPSet(t *testing.T) {
	routes, err := Routes("exclude", "10.0.0.0/8,192.168.0.0/16", "include", "2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"10.1.2.3", "192.168.1.1", "9.9.9.9", "2001:db8::1"} {
		contains := false
		for _, raw := range strings.Split(routes, ", ") {
			contains = contains || netip.MustParsePrefix(raw).Contains(netip.MustParseAddr(value))
		}
		want := value == "9.9.9.9" || value == "2001:db8::1"
		if contains != want {
			t.Fatalf("wrong route for %s", value)
		}
	}
}
