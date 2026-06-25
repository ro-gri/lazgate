package provisioning

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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
