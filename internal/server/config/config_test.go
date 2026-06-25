package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LAZ_ADDR", "")
	t.Setenv("LAZ_NAME", "")
	t.Setenv("LAZ_STORAGE", "")
	t.Setenv("LAZ_DATA", "")
	t.Setenv("LAZ_DATABASE_URL", "")
	t.Setenv("LAZ_SECRET_KEY", "")
	t.Setenv("LAZ_ADMIN_TOKEN", "")
	t.Setenv("LAZ_PUBLIC_BASE_URL", "")
	t.Setenv("LAZ_WEB_PREFIX", "")
	t.Setenv("LAZ_BLANK_PAGE_PATH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:8088" {
		t.Fatalf("unexpected addr %q", cfg.Addr)
	}
	if cfg.Name != "Chamomile" {
		t.Fatalf("unexpected name %q", cfg.Name)
	}
	if cfg.Storage != "sqlite" {
		t.Fatalf("unexpected storage %q", cfg.Storage)
	}
	if cfg.DataPath != "./data/laz.db" {
		t.Fatalf("unexpected data path %q", cfg.DataPath)
	}
	if cfg.AdminToken != "change-me" {
		t.Fatalf("unexpected admin token %q", cfg.AdminToken)
	}
	if cfg.WebPrefix != "/admin" {
		t.Fatalf("unexpected web prefix %q", cfg.WebPrefix)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LAZ_ADDR", "0.0.0.0:9000")
	t.Setenv("LAZ_NAME", "Chamomile")
	t.Setenv("LAZ_STORAGE", "sqlite")
	t.Setenv("LAZ_DATA", "/tmp/app.db")
	t.Setenv("LAZ_DATABASE_URL", "postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable")
	t.Setenv("LAZ_SECRET_KEY", "secret-key")
	t.Setenv("LAZ_ADMIN_TOKEN", "secret")
	t.Setenv("LAZ_PUBLIC_BASE_URL", "https://net.example")
	t.Setenv("LAZ_WEB_PREFIX", "/admin-secret")
	t.Setenv("LAZ_BLANK_PAGE_PATH", "/opt/blank.html")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "0.0.0.0:9000" || cfg.Name != "Chamomile" || cfg.Storage != "sqlite" ||
		cfg.DataPath != "/tmp/app.db" ||
		cfg.DatabaseURL != "postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable" ||
		cfg.SecretKey != "secret-key" || cfg.AdminToken != "secret" ||
		cfg.PublicBaseURL != "https://net.example" || cfg.WebPrefix != "/admin-secret" ||
		cfg.BlankPagePath != "/opt/blank.html" {
		t.Fatalf("unexpected config %#v", cfg)
	}
}
