package provisioning

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNormalizeUsesInternalTrafficStatsAddress(t *testing.T) {
	input := normalize(Hysteria2Request{})
	if input.TrafficStatsListen != "127.0.0.1:25413" {
		t.Fatalf("unexpected stats address: %q", input.TrafficStatsListen)
	}
}

func TestInstallationRequestsExcludeInternalSettings(t *testing.T) {
	for _, tc := range []struct {
		request any
		fields  []string
	}{
		{Hysteria2Request{}, []string{"TrafficStatsEnabled"}},
		{AWGRequest{}, []string{"InterfaceName", "InstallerVersion"}},
	} {
		for _, field := range tc.fields {
			if _, ok := reflect.TypeOf(tc.request).FieldByName(field); ok {
				t.Errorf("%T exposes internal setting %s", tc.request, field)
			}
		}
	}
}

func TestInstallAgentScriptUsesSplitHysteriaArtifact(t *testing.T) {
	script := renderHysteriaAgentInstallScript(agentInstallScriptInput{
		AgentDownloadBase: "https://example.com/download",
		AgentConfig:       "config: value",
		CAPEM:             "ca",
		ServerCAPEM:       "server-ca",
		NodeCertPEM:       "node-cert",
		NodeKeyPEM:        "node-key",
	})

	want := []string{
		`agent_asset="lazgate-hysteria-agent-linux-$asset_arch"`,
		`curl -fsSL -o "$tmp/lazgate-hysteria-agent" 'https://example.com/download'/"$agent_asset"`,
		`grep " $agent_asset$" checksums.txt`,
		`sudo install -m 755 "$tmp/lazgate-hysteria-agent" /usr/local/bin/lazgate-hysteria-agent`,
		`ExecStart=/usr/local/bin/lazgate-hysteria-agent --config /etc/lazgate-agent/config.yaml`,
	}
	for _, needle := range want {
		if !strings.Contains(script, needle) {
			t.Fatalf("install script missing %q:\n%s", needle, script)
		}
	}
	if strings.Contains(script, "lazgate-agent-linux-") || strings.Contains(script, "/usr/local/bin/lazgate-agent --config") {
		t.Fatalf("install script still references legacy combined agent artifact or binary:\n%s", script)
	}
}

func TestAgentConfigAlwaysIncludesServerTrustAnchor(t *testing.T) {
	r := &runner{input: Hysteria2InstallInput{ServerURL: "https://laz.example", AgentGRPCTarget: "laz.example:9443", TrafficStatsListen: "127.0.0.1:25413"}}
	config := r.agentConfig("node", "installation", "hysteria2", "hysteria", "stats")
	for _, field := range []string{
		`ca_file: "/etc/lazgate-agent/server-ca.crt"`,
		`cert_file: "/etc/lazgate-agent/node.crt"`,
		`key_file: "/etc/lazgate-agent/node.key"`,
	} {
		if !strings.Contains(config, field) {
			t.Fatalf("agent config lacks required mTLS field %q: %s", field, config)
		}
	}
	if err := (&runner{installer: &Installer{}}).installAgent(nil, nil, "node", "installation", "hysteria2", "hysteria", "stats", mtlsFiles{}); err == nil || !strings.Contains(err.Error(), "server CA material is required") {
		t.Fatalf("expected missing server trust material rejection, got %v", err)
	}
}

func TestHysteriaProvisioningRejectsInvalidServerTrustBeforeBootstrap(t *testing.T) {
	command := Hysteria2Command{Request: Hysteria2Request{BootstrapPassword: "bootstrap-secret"}}
	for _, tt := range []struct {
		name string
		pem  string
	}{
		{name: "missing", pem: ""},
		{name: "malformed", pem: "not a PEM certificate"},
		{name: "non-certificate PEM", pem: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-key")}))},
		{name: "CA certificate", pem: testServerCertificatePEM(t, true, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Now().Add(time.Hour))},
		{name: "client-only certificate", pem: testServerCertificatePEM(t, false, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, time.Now().Add(time.Hour))},
		{name: "no extended key usage certificate", pem: testServerCertificatePEM(t, false, nil, time.Now().Add(time.Hour))},
		{name: "any extended key usage certificate", pem: testServerCertificatePEM(t, false, []x509.ExtKeyUsage{x509.ExtKeyUsageAny}, time.Now().Add(time.Hour))},
		{name: "expired certificate", pem: testServerCertificatePEM(t, false, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Now().Add(-time.Minute))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			installer := New(nil)
			installer.SetAgentServerCertPEM(tt.pem)

			// These calls have no reachable SSH target; a trust error proves validation
			// occurred before bootstrap or any remote mutation was attempted.
			install, err := installer.InstallHysteria2(nil, command)
			if err == nil || len(install.Steps) != 0 {
				t.Fatalf("install did not stop before bootstrap: result=%+v err=%v", install, err)
			}
			attach, err := installer.AttachHysteria2(nil, command)
			if err == nil || len(attach.Steps) != 0 {
				t.Fatalf("attach did not stop before bootstrap: result=%+v err=%v", attach, err)
			}
			bootstrap, err := installer.BootstrapHysteriaInstall(nil, ModeInstall, command)
			if err == nil || len(bootstrap.Steps) != 0 {
				t.Fatalf("bootstrap did not stop before remote actions: state=%+v err=%v", bootstrap, err)
			}
		})
	}
}

func TestServerTrustValidationAcceptsUsableServerCertificate(t *testing.T) {
	installer := New(nil)
	installer.SetAgentServerCertPEM(testServerCertificatePEM(t, false, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, time.Now().Add(time.Hour)))
	if err := installer.validateAgentServerTrustMaterial(); err != nil {
		t.Fatalf("usable server certificate rejected: %v", err)
	}
}

func testServerCertificatePEM(t *testing.T, isCA bool, usages []x509.ExtKeyUsage, notAfter time.Time) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "laz.example"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestInitialInstallStateDoesNotPersistBootstrapCredentials(t *testing.T) {
	const password = "root-password-for-bootstrap-only"
	state, err := InitialInstallState("install", Hysteria2Command{
		Request: Hysteria2Request{
			SSHHost:            "203.0.113.10",
			SSHPort:            22,
			BootstrapUser:      "root",
			BootstrapPassword:  password,
			NodeName:           "test-node",
			MasqueradeURL:      "https://example.com/",
			TrafficStatsListen: "127.0.0.1:25413",
		},
		ServerURL:         "https://laz.example.com",
		AgentGRPCTarget:   "laz.example.com:9443",
		AgentDownloadBase: "https://github.com/ro-gri/lazgate/releases/latest/download",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Input.TrafficStatsEnabled {
		t.Fatal("installation must enable traffic stats without an API parameter")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	if strings.Contains(serialized, "bootstrap_password") || strings.Contains(serialized, password) || strings.Contains(serialized, "bootstrap_user") {
		t.Fatalf("bootstrap credentials leaked into async install state: %s", serialized)
	}
}

func TestRestoreAttachManagedHysteriaConfigOnlyRestoresManagedKeys(t *testing.T) {
	current := `
listen: :8443
auth:
  type: http
  http:
    url: http://127.0.0.1:28262/auth
trafficStats:
  listen: 127.0.0.1:25413
  secret: laz-secret
masquerade:
  type: proxy
  proxy:
    url: https://changed.example.com/
`
	backup := `
listen: :8443
auth:
  type: password
  password: old-secret
obfs:
  type: salamander
trafficStats:
  listen: 127.0.0.1:9000
  secret: old-stats
`
	restored, err := restoreAttachManagedHysteriaConfig(current, backup)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(restored), &cfg); err != nil {
		t.Fatal(err)
	}
	auth := cfg["auth"].(map[string]any)
	if auth["type"] != "password" || auth["password"] != "old-secret" {
		t.Fatalf("auth was not restored from backup: %#v", auth)
	}
	traffic := cfg["trafficStats"].(map[string]any)
	if traffic["listen"] != "127.0.0.1:9000" || traffic["secret"] != "old-stats" {
		t.Fatalf("trafficStats was not restored from backup: %#v", traffic)
	}
	masquerade := cfg["masquerade"].(map[string]any)
	proxy := masquerade["proxy"].(map[string]any)
	if proxy["url"] != "https://changed.example.com/" {
		t.Fatalf("non-managed current config was not preserved: %#v", masquerade)
	}
	if cfg["obfs"] != nil {
		t.Fatalf("non-managed backup-only key leaked into current config: %#v", cfg["obfs"])
	}
}

func TestRestoreAttachManagedHysteriaConfigDeletesMissingBackupKeys(t *testing.T) {
	current := `
auth:
  type: http
trafficStats:
  listen: 127.0.0.1:25413
masquerade:
  type: proxy
`
	backup := `
masquerade:
  type: proxy
`
	restored, err := restoreAttachManagedHysteriaConfig(current, backup)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(restored), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["auth"]; ok {
		t.Fatalf("auth should be deleted when backup did not have it: %s", restored)
	}
	if _, ok := cfg["trafficStats"]; ok {
		t.Fatalf("trafficStats should be deleted when backup did not have it: %s", restored)
	}
}

func TestPatchExistingHysteriaConfigPreservesTopLevelSettings(t *testing.T) {
	raw := `
listen: :8443
acme:
  domains:
    - hy2.example.com
auth:
  type: password
  password: old-secret
obfs:
  type: salamander
  salamander:
    password: old-obfs
masquerade:
  type: proxy
  proxy:
    url: https://example.com/
    rewriteHost: true
`
	patched, port, hasStaticUsers, err := patchExistingHysteriaConfig(raw, "127.0.0.1:25413", "stats-secret")
	if err != nil {
		t.Fatal(err)
	}
	if port != 8443 {
		t.Fatalf("unexpected listen port %d", port)
	}
	if !hasStaticUsers {
		t.Fatal("expected static auth warning")
	}
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(patched), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["obfs"] == nil || cfg["masquerade"] == nil || cfg["acme"] == nil {
		t.Fatalf("top-level settings were not preserved: %s", patched)
	}
	if strings.Contains(patched, "old-secret") {
		t.Fatalf("old static auth secret leaked into patched config: %s", patched)
	}
	auth := cfg["auth"].(map[string]any)
	if auth["type"] != "http" {
		t.Fatalf("auth was not patched: %#v", auth)
	}
	traffic := cfg["trafficStats"].(map[string]any)
	if traffic["listen"] != "127.0.0.1:25413" || traffic["secret"] != "stats-secret" {
		t.Fatalf("trafficStats was not patched: %#v", traffic)
	}
}
