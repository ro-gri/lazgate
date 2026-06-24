package provisioning

import (
	"slices"
	"testing"
)

func TestNormalizeServerIP(t *testing.T) {
	for input, want := range map[string]string{
		" 104.128.142.63 ":      "104.128.142.63",
		"2001:0db8:0:0:0:0:0:1": "2001:db8::1",
		"::ffff:104.128.142.63": "104.128.142.63",
	} {
		got, err := normalizeServerIP(input)
		if err != nil || got != want {
			t.Fatalf("normalizeServerIP(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"", "server.example", "https://1.1.1.1", "1.1.1.1:22", "127.1", "010.0.0.1", "[::1]", "fe80::1%eth0"} {
		if _, err := normalizeServerIP(input); err == nil {
			t.Fatalf("invalid IP accepted: %q", input)
		}
	}
}

func TestCountryMetadata(t *testing.T) {
	for input, want := range map[string]string{"fi": "FI", " NL ": "NL", "": ""} {
		got, err := normalizeServerRegion(input)
		if err != nil || got != want {
			t.Fatalf("normalizeServerRegion(%q) = %q, %v", input, got, err)
		}
	}
	for _, value := range []string{"auto", "Finland", "FI-Helsinki", "ZZ", "001"} {
		if _, err := normalizeServerRegion(value); err == nil {
			t.Fatalf("invalid region accepted: %q", value)
		}
	}
	codes := SupportedCountries()
	if !slices.Contains(codes, "FI") || !slices.Contains(codes, "NL") || len(codes) < 200 {
		t.Fatal("country catalog is incomplete")
	}
}

func TestHysteriaStateKeepsCanonicalIPAndCountry(t *testing.T) {
	command := Hysteria2Command{Request: Hysteria2Request{
		SSHHost: "::ffff:104.128.142.63", BootstrapPassword: "secret", NodeName: "Node", Region: "fi",
	}, ServerURL: "https://panel.example", AgentGRPCTarget: "panel.example:9443", AgentDownloadBase: "https://downloads.example"}
	state, err := InitialInstallState(ModeInstall, command)
	if err != nil {
		t.Fatal(err)
	}
	if state.Input.SSHHost != "104.128.142.63" || state.Input.Region != "FI" ||
		state.PublicDomain != "h2.104-128-142-63.sslip.io" {
		t.Fatal("installation state lost canonical IP/country or generated wrong domain")
	}
	command.Request.SSHHost = "server.example"
	if _, err := InitialInstallState(ModeInstall, command); err == nil {
		t.Fatal("hostname accepted in IP field")
	}
}
