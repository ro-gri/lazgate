package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"laz/internal/common/config"
	"laz/internal/server"
	"laz/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if cfg.Storage != "postgres" && cfg.Storage != "postgresql" && cfg.DataPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.DataPath), 0o700); err != nil {
			log.Fatalf("create data dir: %v", err)
		}
	}

	st, err := openStore(cfg)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	srv := server.NewServer(st, cfg.AdminToken, cfg.PublicBaseURL, cfg.WebPrefix)
	srv.SetAdminAuth(cfg.AdminToken, cfg.AdminTokenSHA256)
	srv.SetAppName(cfg.Name)
	if cfg.BlankPagePath != "" {
		raw, err := os.ReadFile(cfg.BlankPagePath)
		if err != nil {
			log.Fatalf("read blank page: %v", err)
		}
		srv.SetBlankPageHTML(string(raw))
	}

	log.Printf("laz listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func openStore(cfg config.Config) (store.Store, error) {
	var st store.Store
	var err error
	switch cfg.Storage {
	case "sqlite":
		st, err = store.OpenSQLite(cfg.DataPath)
	case "postgres", "postgresql":
		if cfg.DatabaseURL == "" {
			return nil, os.ErrInvalid
		}
		st, err = store.OpenPostgres(cfg.DatabaseURL)
	default:
		return nil, os.ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	return store.WrapSecrets(st, cfg.SecretKey)
}
