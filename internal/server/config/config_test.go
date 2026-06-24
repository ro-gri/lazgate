package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("LAZ_ADDR", "")
	t.Setenv("LAZ_NAME", "")
	t.Setenv("LAZ_STORAGE", "")
	t.Setenv("LAZ_DATA", "")
	t.Setenv("LAZ_AUTH_DATA", "")
	t.Setenv("LAZ_DATABASE_URL", "")
	t.Setenv("LAZ_AUTH_DATABASE_URL", "")
	t.Setenv("LAZ_TRANSPORT_DATABASE_URL", "")
	t.Setenv("LAZ_SECRET_KEY", "")
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
	if cfg.WebPrefix != "/admin" {
		t.Fatalf("unexpected web prefix %q", cfg.WebPrefix)
	}
	limits := cfg.RateLimits()
	if !limits.Enabled || limits.ConnectPerMinute != 30 || limits.VerifyPerMinute != 10 ||
		limits.QRPerMinute != 10 || limits.HPLinkPerMinute != 10 ||
		limits.PasswordSetupPerMinute != 5 || limits.AdminLoginPerMinute != 5 ||
		limits.UserLoginPerMinute != 5 || limits.RecoveryPerMinute != 5 {
		t.Fatalf("unexpected rate limits %#v", limits)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LAZ_ADDR", "0.0.0.0:9000")
	t.Setenv("LAZ_NAME", "Chamomile")
	t.Setenv("LAZ_STORAGE", "sqlite")
	t.Setenv("LAZ_DATA", "/tmp/app.db")
	t.Setenv("LAZ_AUTH_DATA", "/tmp/app.auth.db")
	t.Setenv("LAZ_DATABASE_URL", "postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable")
	t.Setenv("LAZ_AUTH_DATABASE_URL", "postgres://laz:secret@127.0.0.1:5432/laz_auth?sslmode=disable")
	t.Setenv("LAZ_TRANSPORT_DATABASE_URL", "postgres://laz:secret@127.0.0.1:5432/laz_transport?sslmode=disable")
	t.Setenv("LAZ_SECRET_KEY", "secret-key")
	t.Setenv("LAZ_PUBLIC_BASE_URL", "https://net.example")
	t.Setenv("LAZ_WEB_PREFIX", "/admin-secret")
	t.Setenv("LAZ_BLANK_PAGE_PATH", "/opt/blank.html")
	t.Setenv("LAZ_RATE_LIMIT_ENABLED", "false")
	t.Setenv("LAZ_RATE_LIMIT_CONNECT_PER_MINUTE", "31")
	t.Setenv("LAZ_RATE_LIMIT_VERIFY_PER_MINUTE", "11")
	t.Setenv("LAZ_RATE_LIMIT_QR_PER_MINUTE", "12")
	t.Setenv("LAZ_RATE_LIMIT_HP_LINK_PER_MINUTE", "13")
	t.Setenv("LAZ_RATE_LIMIT_PASSWORD_SETUP_PER_MINUTE", "6")
	t.Setenv("LAZ_RATE_LIMIT_ADMIN_LOGIN_PER_MINUTE", "7")
	t.Setenv("LAZ_RATE_LIMIT_USER_LOGIN_PER_MINUTE", "8")
	t.Setenv("LAZ_RATE_LIMIT_RECOVERY_PER_MINUTE", "9")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "0.0.0.0:9000" || cfg.Name != "Chamomile" || cfg.Storage != "sqlite" ||
		cfg.DataPath != "/tmp/app.db" || cfg.AuthDataPath != "/tmp/app.auth.db" ||
		cfg.DatabaseURL != "postgres://laz:secret@127.0.0.1:5432/laz?sslmode=disable" ||
		cfg.AuthDatabaseURL != "postgres://laz:secret@127.0.0.1:5432/laz_auth?sslmode=disable" ||
		cfg.TransportDatabaseURL != "postgres://laz:secret@127.0.0.1:5432/laz_transport?sslmode=disable" ||
		cfg.SecretKey != "secret-key" || cfg.PublicBaseURL != "https://net.example" ||
		cfg.WebPrefix != "/admin-secret" ||
		cfg.BlankPagePath != "/opt/blank.html" {
		t.Fatalf("unexpected config %#v", cfg)
	}
	limits := cfg.RateLimits()
	if limits.Enabled || limits.ConnectPerMinute != 31 || limits.VerifyPerMinute != 11 ||
		limits.QRPerMinute != 12 || limits.HPLinkPerMinute != 13 ||
		limits.PasswordSetupPerMinute != 6 || limits.AdminLoginPerMinute != 7 ||
		limits.UserLoginPerMinute != 8 || limits.RecoveryPerMinute != 9 {
		t.Fatalf("unexpected rate limits %#v", limits)
	}
}
