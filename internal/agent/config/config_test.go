package config

import "testing"

func TestValidateRejectsPublicAuthListen(t *testing.T) {
	cfg := WithDefaults(Config{
		NodeID:    "nod_1",
		ServerURL: "https://example.com",
		MTLS:      MTLS{CAFile: "ca", CertFile: "cert", KeyFile: "key"},
		Hysteria2: Hysteria2{AuthListen: "0.0.0.0:28262"},
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected public auth listen to be rejected")
	}
}

func TestValidateDefaults(t *testing.T) {
	cfg := WithDefaults(Config{
		NodeID:    "nod_1",
		ServerURL: "https://example.com/",
		MTLS:      MTLS{CAFile: "ca", CertFile: "cert", KeyFile: "key"},
	})
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://example.com" || cfg.Hysteria2.AuthListen != "127.0.0.1:28262" || cfg.Hysteria2.StatsURL != "http://127.0.0.1:25413" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}
