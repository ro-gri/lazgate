package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	amneziaproto "laz/generated/nodeproto/amnezia"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	transportstore "laz/internal/queue"
	"laz/internal/server"
	"laz/internal/server/config"
	"laz/internal/server/model"
	"laz/internal/server/storage"
	"laz/internal/server/transportdb"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		log.Printf("server stopped with error: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	if cfg.Storage != "postgres" && cfg.Storage != "postgresql" {
		if cfg.DataPath != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.DataPath), 0o700); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}
		}
		if cfg.AuthDataPath != "" {
			if err := os.MkdirAll(filepath.Dir(cfg.AuthDataPath), 0o700); err != nil {
				return fmt.Errorf("create auth data dir: %w", err)
			}
		}
	}

	st, err := openStore(cfg)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		_ = st.Close()
	}()
	transportSQL, err := openTransportStore(cfg)
	if err != nil {
		return fmt.Errorf("open transport store: %w", err)
	}
	transportClosed := false
	defer func() {
		if !transportClosed {
			_ = transportSQL.Close()
		}
	}()
	eventStore, err := transportdb.New(transportSQL, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("open server transport store: %w", err)
	}
	transport, err := transportstore.WrapSecrets(transportSQL, cfg.SecretKey)
	if err != nil {
		return fmt.Errorf("wrap transport secrets: %w", err)
	}
	defer func() {
		_ = transport.Close()
		transportClosed = true
	}()

	srv, err := server.New(server.Config{
		PublicBaseURL: cfg.PublicBaseURL, AgentGRPCTarget: cfg.AgentGRPCTarget,
		AgentDownloadBase: cfg.AgentDownloadBase, WebPrefix: cfg.WebPrefix, SecretKey: cfg.SecretKey,
	}, server.Dependencies{Store: st, Transport: transport, Events: eventStore, Completer: eventStore})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}
	srv.SetRateLimits(cfg.RateLimits())
	srv.SetAppName(cfg.Name)
	if cfg.AgentGRPCCertFile != "" {
		raw, err := os.ReadFile(cfg.AgentGRPCCertFile)
		if err != nil {
			return fmt.Errorf("read agent grpc cert: %w", err)
		}
		srv.SetProvisioningAgentServerCertPEM(string(raw))
	}
	if cfg.BlankPagePath != "" {
		raw, err := os.ReadFile(cfg.BlankPagePath)
		if err != nil {
			return fmt.Errorf("read blank page: %w", err)
		}
		srv.SetBlankPageHTML(string(raw))
	}
	var agentServer *grpc.Server
	var agentListener net.Listener
	if cfg.AgentGRPCAddr != "" {
		agentServer, agentListener, err = newAgentGRPC(cfg, srv)
		if err != nil {
			return err
		}
		defer agentListener.Close()
	}
	httpServer := &http.Server{Addr: cfg.Addr, Handler: srv.Routes()}
	log.Printf("laz listening on %s", cfg.Addr)
	return serve(ctx, httpServer, agentServer, agentListener,
		func(ctx context.Context) { <-srv.RunWorkers(ctx) },
		func(ctx context.Context) { cleanupTransport(ctx, transport) },
		func(ctx context.Context) { cleanupServerEvents(ctx, eventStore) },
	)
}

func serve(ctx context.Context, httpServer *http.Server, agentServer *grpc.Server, agentListener net.Listener, background ...func(context.Context)) error {
	group, ctx := errgroup.WithContext(ctx)
	// Cancels long-lived HTTP handlers (including SSE) before draining connections.
	httpServer.BaseContext = func(net.Listener) context.Context { return ctx }
	group.Go(func() error {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	})
	if agentServer != nil {
		group.Go(func() error {
			err := agentServer.Serve(agentListener)
			if errors.Is(err, grpc.ErrServerStopped) {
				return nil
			}
			return err
		})
	}
	for _, task := range background {
		group.Go(func() error { task(ctx); return nil })
	}
	group.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var shutdown errgroup.Group
		shutdown.Go(func() error {
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				_ = httpServer.Close()
				return fmt.Errorf("HTTP shutdown: %w", err)
			}
			return nil
		})
		if agentServer != nil {
			shutdown.Go(func() error {
				done := make(chan struct{})
				go func() { agentServer.GracefulStop(); close(done) }()
				select {
				case <-done:
				case <-shutdownCtx.Done():
					agentServer.Stop()
					<-done
				}
				return nil
			})
		}
		return shutdown.Wait()
	})
	return group.Wait()
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

func newAgentGRPC(cfg config.Config, srv *server.Server) (*grpc.Server, net.Listener, error) {
	tlsConfig, err := agentGRPCTLSConfig(cfg, srv.AgentInstallationStore())
	if err != nil {
		return nil, nil, fmt.Errorf("agent grpc tls config: %w", err)
	}
	listener, err := net.Listen("tcp", cfg.AgentGRPCAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("agent grpc listen: %w", err)
	}
	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))
	hysteriaproto.RegisterAgentServer(grpcServer, srv.HysteriaAgent())
	amneziaproto.RegisterAgentServer(grpcServer, srv.AmneziaAgent())
	log.Printf("laz agent grpc listening on %s", cfg.AgentGRPCAddr)
	return grpcServer, listener, nil
}

type agentInstallationStore interface {
	GetActiveAgentInstallationByCertificateFingerprint(fingerprint string) (model.AgentInstallation, error)
}

func agentGRPCTLSConfig(cfg config.Config, installations agentInstallationStore) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.AgentGRPCCertFile, cfg.AgentGRPCKeyFile)
	if err != nil {
		return nil, err
	}
	if installations == nil {
		return nil, fmt.Errorf("agent installation store is required")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) != 1 {
				return fmt.Errorf("agent must present exactly one leaf certificate")
			}
			leaf, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}
			if leaf.IsCA || !leaf.NotBefore.Before(time.Now()) || !leaf.NotAfter.After(time.Now()) {
				return fmt.Errorf("agent certificate is not currently valid")
			}
			hasClientAuth := false
			for _, usage := range leaf.ExtKeyUsage {
				if usage == x509.ExtKeyUsageClientAuth {
					hasClientAuth = true
					break
				}
			}
			if !hasClientAuth {
				return fmt.Errorf("agent certificate lacks client authentication usage")
			}
			fingerprint := fmt.Sprintf("%x", sha256.Sum256(leaf.Raw))
			if _, err := installations.GetActiveAgentInstallationByCertificateFingerprint(fingerprint); err != nil {
				return fmt.Errorf("unknown, revoked, or inactive agent certificate")
			}
			return nil
		},
	}, nil
}

func openStore(cfg config.Config) (store.Store, error) {
	var st store.Store
	var err error
	switch cfg.Storage {
	case "sqlite":
		if cfg.AuthDataPath == "" {
			return nil, fmt.Errorf("LAZ_AUTH_DATA is required: auth storage must use a separate SQLite database")
		}
		st, err = store.OpenSQLiteWithAuth(cfg.DataPath, cfg.AuthDataPath)
	case "postgres", "postgresql":
		if cfg.DatabaseURL == "" {
			return nil, os.ErrInvalid
		}
		if cfg.AuthDatabaseURL == "" {
			return nil, fmt.Errorf("LAZ_AUTH_DATABASE_URL is required: auth storage must use a separate PostgreSQL database")
		}
		st, err = store.OpenPostgresWithAuth(cfg.DatabaseURL, cfg.AuthDatabaseURL)
	default:
		return nil, os.ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	wrapped, err := store.WrapSecrets(st, cfg.SecretKey)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return wrapped, nil
}
