package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentcommon "laz/internal/agent/common"
)

const validConfigYAML = `node_id: "11111111-1111-4111-8111-111111111111"
installation_id: "22222222-2222-4222-8222-222222222222"
agent_type: "hysteria2"
server_url: "https://example.com"
agent_grpc_target:
mtls:
  ca_file: "ca"
  cert_file: "cert"
  key_file: "key"
`

func TestLoadFileRejectsPublicAuthListen(t *testing.T) {
	_, err := LoadFile(writeConfigFile(t, validConfigYAML+`hysteria2:
  auth_listen: "0.0.0.0:28262"
`))
	if err == nil || !strings.Contains(err.Error(), "hysteria2.auth_listen must stay on 127.0.0.1") {
		t.Fatalf("expected public auth listen rejection, got %v", err)
	}
}

func TestLoadFileRejectsNonLocalStatsURL(t *testing.T) {
	_, err := LoadFile(writeConfigFile(t, validConfigYAML+`hysteria2:
  stats_url: "http://192.0.2.10:25413"
`))
	if err == nil || !strings.Contains(err.Error(), "hysteria2.stats_url must stay on localhost") {
		t.Fatalf("expected non-local stats URL rejection, got %v", err)
	}
}

func TestValidateDefaults(t *testing.T) {
	cfg := WithDefaults(Config{
		Config: agentcommon.Config{
			NodeID:         "11111111-1111-4111-8111-111111111111",
			InstallationID: "22222222-2222-4222-8222-222222222222",
			AgentType:      "hysteria2",
			ServerURL:      "https://example.com/",
			MTLS:           agentcommon.MTLS{CAFile: "ca", CertFile: "cert", KeyFile: "key"},
		},
	})
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://example.com" || cfg.Hysteria2.AuthListen != "127.0.0.1:28262" || cfg.Hysteria2.StatsURL != "http://127.0.0.1:25413" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
