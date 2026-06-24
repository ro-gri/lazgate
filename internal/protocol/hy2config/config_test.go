package hy2config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultsAndRuntimePreserveIntegration(t *testing.T) {
	c := Defaults()
	if err := Validate(c); err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"auth": map[string]any{"type": "http", "http": map[string]any{"url": "http://127.0.0.1/auth"}}, "trafficStats": map[string]any{"secret": "private"}, "acme": map[string]any{"email": "owner@example.com", "domains": []string{"old.example"}}, "ech": map[string]any{"keyPath": "/keys/ech"}}
	c.BlockedCountries = []string{"FI", "RU"}
	c.Obfs.Type = "gecko"
	c.Obfs.Password = "secret"
	c.Masquerade.Type = "string"
	c.Masquerade.Content = "hello"
	rendered, err := c.Runtime(base, "new.example")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rendered)
	s := string(raw)
	for _, value := range []string{"private", "owner@example.com", "/keys/ech", "new.example", "reject(127.0.0.0/8)", "reject(169.254.0.0/16)", "reject(geoip:fi)", "reject(geoip:ru)", `"maxIdleTimeout":"30s"`, `"maxIncomingStreams":1024`, `"disableUDP":false`} {
		if !strings.Contains(s, value) {
			t.Fatalf("missing %s: %s", value, s)
		}
	}
}
func TestInvalidSettings(t *testing.T) {
	for name, mutate := range map[string]func(*Config){"port": func(c *Config) { c.ListenPort = 0 }, "window": func(c *Config) { c.QUIC.MaxStreamReceiveWindow = 1 }, "timeout": func(c *Config) { c.QUIC.MaxIdleTimeout = "1s" }, "country": func(c *Config) { c.BlockedCountries = []string{"XX"} }, "gecko": func(c *Config) { c.Obfs.Type = "gecko"; c.Obfs.Password = "secret"; c.Obfs.MaxPacketSize = 2 }, "proxy": func(c *Config) { c.Masquerade.Type = "proxy"; c.Masquerade.URL = "file:///etc/passwd" }, "dns": func(c *Config) { c.Resolver.Type = "tls"; c.Resolver.Addr = "1.1.1.1" }, "bandwidth": func(c *Config) { c.Bandwidth.Up = "-20 mbps" }, "ports": func(c *Config) { c.Sniff.TCPPorts = "443-80" }, "duplicate exits": func(c *Config) { c.Outbounds = append(c.Outbounds, c.Outbounds[0]) }} {
		t.Run(name, func(t *testing.T) {
			c := Defaults()
			mutate(&c)
			if Validate(c) == nil {
				t.Fatal("accepted invalid settings")
			}
		})
	}
}
func TestImportPreservesNondefaultSettings(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(`{"listen":":8443","quic":{"maxIdleTimeout":"20s"},"bandwidth":{"up":"200 mbps","down":"200 mbps"},"obfs":{"type":"salamander","salamander":{"password":"secret"}},"resolver":{"type":"tls","tls":{"addr":"1.1.1.1:853","sni":"cloudflare-dns.com","timeout":"10s"}},"acl":{"inline":["reject(geoip:ir)","reject(geosite:ir)"]},"outbounds":[{"name":"v4","type":"direct","direct":{"mode":4,"bindDevice":"eth0"}}]}`), &raw); err != nil {
		t.Fatal(err)
	}
	c, err := FromRuntime(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.ListenPort != 8443 || c.QUIC.MaxIdleTimeout != "20s" || c.Obfs.Password != "secret" || c.Resolver.Addr != "1.1.1.1:853" || c.Outbounds[0].Mode != "4" || len(c.BlockedCountries) != 1 || len(c.ACLRules) != 1 || c.ProtectInternal {
		t.Fatalf("bad import: %+v", c)
	}
}
