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
agent_type: "amneziawg"
server_url: "https://example.com"
agent_grpc_target: "127.0.0.1:9443"
mtls:
  ca_file: "ca"
  cert_file: "cert"
  key_file: "key"
`

func TestLoadFileAllowsMissingHysteriaFields(t *testing.T) {
	cfg, err := LoadFile(writeConfigFile(t, validConfigYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NodeID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.AmneziaWG.ServiceName != "awg-quick@awg0" || cfg.AmneziaWG.AWGBinaryPath != "awg" || cfg.AmneziaWG.UseSudo || cfg.AmneziaWG.InterfaceName != "awg0" {
		t.Fatalf("unexpected AmneziaWG runtime defaults: %+v", cfg.AmneziaWG)
	}
}

func TestLoadFileUsesAgentLocalAmneziaWGRuntime(t *testing.T) {
	cfg, err := LoadFile(writeConfigFile(t, validConfigYAML+`amneziawg:
  service_name: "lab-awg"
  awg_binary_path: "/usr/local/bin/awg"
  use_sudo: true
  config_path: "/var/lib/lazgate-agent/awg/awg0.conf"
  interface_name: "awg7"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AmneziaWG.ServiceName != "lab-awg" || cfg.AmneziaWG.AWGBinaryPath != "/usr/local/bin/awg" || !cfg.AmneziaWG.UseSudo || cfg.AmneziaWG.InterfaceName != "awg7" {
		t.Fatalf("unexpected AmneziaWG runtime: %+v", cfg.AmneziaWG)
	}
	if cfg.AmneziaWG.ConfigPath != "/var/lib/lazgate-agent/awg/awg0.conf" {
		t.Fatalf("config path=%q", cfg.AmneziaWG.ConfigPath)
	}
}

func TestValidateAllowsUnsafeZeroHysteriaFields(t *testing.T) {
	cfg := WithDefaults(Config{Config: agentcommon.Config{
		NodeID:          "11111111-1111-4111-8111-111111111111",
		InstallationID:  "22222222-2222-4222-8222-222222222222",
		AgentType:       "amneziawg",
		ServerURL:       "https://example.com",
		AgentGRPCTarget: "127.0.0.1:9443",
		MTLS:            agentcommon.MTLS{CAFile: "ca", CertFile: "cert", KeyFile: "key"},
	}})
	if err := Validate(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFileRejectsMissingMTLSCertAndKey(t *testing.T) {
	_, err := LoadFile(writeConfigFile(t, `node_id: "11111111-1111-4111-8111-111111111111"
installation_id: "22222222-2222-4222-8222-222222222222"
agent_type: "amneziawg"
server_url: "https://example.com"
mtls:
  ca_file: "ca"
`))
	if err == nil || !strings.Contains(err.Error(), "mtls ca_file, cert_file, and key_file are required") {
		t.Fatalf("expected mtls trust material rejection, got %v", err)
	}
}

func TestLoadFileRejectsMissingMTLSCA(t *testing.T) {
	_, err := LoadFile(writeConfigFile(t, `node_id: "11111111-1111-4111-8111-111111111111"
installation_id: "22222222-2222-4222-8222-222222222222"
agent_type: "amneziawg"
server_url: "https://example.com"
mtls:
  cert_file: "cert"
  key_file: "key"
`))
	if err == nil || !strings.Contains(err.Error(), "mtls ca_file, cert_file, and key_file are required") {
		t.Fatalf("expected missing CA rejection, got %v", err)
	}
}

func TestLoadFileRejectsInvalidAgentGRPCTarget(t *testing.T) {
	_, err := LoadFile(writeConfigFile(t, `node_id: "11111111-1111-4111-8111-111111111111"
installation_id: "22222222-2222-4222-8222-222222222222"
agent_type: "amneziawg"
server_url: "https://example.com"
agent_grpc_target: "not-a-host-port"
mtls:
  ca_file: "ca"
  cert_file: "cert"
  key_file: "key"
`))
	if err == nil || !strings.Contains(err.Error(), "agent_grpc_target must be host:port") {
		t.Fatalf("expected agent_grpc_target rejection, got %v", err)
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

func TestRuntimeValidation(t *testing.T) {
	for _, runtimeYAML := range []string{
		"interface_name: '1234567890123456'",
		"interface_name: '../awg0'",
		"service_name: '--all'",
		"service_name: 'lab-awg;reboot'",
		"awg_binary_path: 'sudo awg'",
		"awg_binary_path: '--help'",
		"config_path: 'relative/awg0.conf'",
	} {
		if _, err := LoadFile(writeConfigFile(t, validConfigYAML+"amneziawg:\n  "+runtimeYAML+"\n")); err == nil {
			t.Fatalf("accepted %s", runtimeYAML)
		}
	}
	cfg, err := LoadFile(writeConfigFile(t, validConfigYAML+"amneziawg:\n  interface_name: awg7\n"))
	if err != nil || cfg.AmneziaWG.ServiceName != "awg-quick@awg7" {
		t.Fatalf("cfg=%+v err=%v", cfg.AmneziaWG, err)
	}
}
