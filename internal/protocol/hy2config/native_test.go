package hy2config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in: validate generated configs with the real binary in a disposable local lab container.
func TestNativeHysteriaConfiguration(t *testing.T) {
	container := os.Getenv("LAZ_HYSTERIA_TEST_CONTAINER")
	if container == "" {
		t.Skip("set LAZ_HYSTERIA_TEST_CONTAINER for native Hysteria validation")
	}
	for _, mode := range []string{"off", "salamander", "gecko", "geoip"} {
		t.Run(mode, func(t *testing.T) {
			c := Defaults()
			c.ListenPort = 34443
			c.Obfs.Type = mode
			if mode == "geoip" {
				c.Obfs.Type = "off"
				c.BlockedCountries = []string{"FI"}
			}
			c.Obfs.Password = "native-test-password"
			current := map[string]any{"tls": map[string]any{"cert": "/lab/tls.crt", "key": "/lab/tls.key"}, "auth": map[string]any{"type": "http", "http": map[string]any{"url": "http://127.0.0.1:28262/auth"}}}
			runtime, e := c.Runtime(current, "localhost")
			if e != nil {
				t.Fatal(e)
			}
			raw, e := json.Marshal(runtime)
			if e != nil {
				t.Fatal(e)
			}
			source := filepath.Join(t.TempDir(), "hysteria.json")
			if e = os.WriteFile(source, raw, 0600); e != nil {
				t.Fatal(e)
			}
			target := "/var/lib/lazgate-agent/laz-native-config-" + mode + ".json"
			if out, e := exec.Command("docker", "cp", source, container+":"+target).CombinedOutput(); e != nil {
				t.Fatalf("copy: %v %s", e, out)
			}
			if out, e := exec.Command("docker", "exec", container, "chown", "10001:10001", target).CombinedOutput(); e != nil {
				t.Fatalf("ownership: %v %s", e, out)
			}
			defer exec.Command("docker", "exec", container, "rm", "-f", target).Run()
			timeout := "2"
			if mode == "geoip" {
				timeout = "30"
			}
			out, e := exec.Command("docker", "exec", "--user", "10001", "--workdir", "/home/h2-laz-agent", container, "timeout", timeout, "/usr/local/bin/hysteria", "server", "-c", target).CombinedOutput()
			if e == nil || !strings.Contains(string(out), "server up and running") {
				t.Fatalf("native config failed: %v %s", e, out)
			}
		})
	}
}
