package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"laz/internal/nodeproto"
	transportstore "laz/internal/nodeproto/transport"
	"laz/internal/server"
	"laz/internal/server/config"
	"laz/internal/server/storage"
	"laz/internal/server/transportdb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
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
	transportSQL, err := openTransportStore(cfg)
	if err != nil {
		log.Fatalf("open transport store: %v", err)
	}
	eventStore, err := transportdb.New(transportSQL, cfg.SecretKey)
	if err != nil {
		log.Fatalf("open server transport store: %v", err)
	}
	transport, err := transportstore.WrapSecrets(transportSQL, cfg.SecretKey)
	if err != nil {
		log.Fatalf("wrap transport secrets: %v", err)
	}
	defer transport.Close()

	srv := server.NewServerWithTransportEvents(st, transport, eventStore, eventStore, cfg.AdminToken, cfg.PublicBaseURL, cfg.WebPrefix)
	srv.SetAdminAuth(cfg.AdminToken, cfg.AdminTokenSHA256)
	srv.SetAppName(cfg.Name)
	if cfg.AgentGRPCCertFile != "" {
		raw, err := os.ReadFile(cfg.AgentGRPCCertFile)
		if err != nil {
			log.Fatalf("read agent grpc cert: %v", err)
		}
		srv.SetProvisioningAgentServerCertPEM(string(raw))
	}
	if cfg.BlankPagePath != "" {
		raw, err := os.ReadFile(cfg.BlankPagePath)
		if err != nil {
			log.Fatalf("read blank page: %v", err)
		}
		srv.SetBlankPageHTML(string(raw))
	}
	if cfg.AgentGRPCAddr != "" {
		go serveAgentGRPC(cfg, srv)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv.RunWorkers(ctx)
	go cleanupTransport(ctx, transport)
	go cleanupServerEvents(ctx, eventStore)

	log.Printf("laz listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, srv.Routes()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func cleanupTransport(ctx context.Context, transport transportstore.Store) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		_ = transport.Cleanup(ctx, transportstore.DefaultCleanupPolicy())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cleanupServerEvents(ctx context.Context, events *transportdb.Store) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		_ = events.Cleanup(ctx, transportdb.DefaultCleanupPolicy())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func openTransportStore(cfg config.Config) (*transportstore.SQLStore, error) {
	switch cfg.Storage {
	case "postgres", "postgresql":
		databaseURL := cfg.TransportDatabaseURL
		if databaseURL == "" {
			databaseURL = cfg.DatabaseURL
		}
		if databaseURL == "" {
			return nil, os.ErrInvalid
		}
		return transportstore.OpenPostgres(databaseURL)
	}
	path := cfg.TransportDataPath
	if path == "" {
		if cfg.DataPath != "" {
			path = filepath.Join(filepath.Dir(cfg.DataPath), "laz.transport.db")
		} else {
			path = "./data/laz.transport.db"
		}
	}
	return transportstore.OpenSQLite(path)
}

func serveAgentGRPC(cfg config.Config, srv *server.Server) {
	tlsConfig, err := agentGRPCTLSConfig(cfg)
	if err != nil {
		log.Fatalf("agent grpc tls config: %v", err)
	}
	listener, err := net.Listen("tcp", cfg.AgentGRPCAddr)
	if err != nil {
		log.Fatalf("agent grpc listen: %v", err)
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	nodeproto.RegisterAgentControlServer(grpcServer, srv.AgentControl())
	log.Printf("laz agent grpc listening on %s", cfg.AgentGRPCAddr)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("agent grpc serve: %v", err)
	}
}

func agentGRPCTLSConfig(cfg config.Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.AgentGRPCCertFile, cfg.AgentGRPCKeyFile)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	if cfg.AgentGRPCCAFile != "" {
		caRaw, err := os.ReadFile(cfg.AgentGRPCCAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caRaw) {
			return nil, os.ErrInvalid
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	} else {
		tlsConfig.ClientAuth = tls.RequireAnyClientCert
	}
	return tlsConfig, nil
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
