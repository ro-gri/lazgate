package hysteria2

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"time"

	hysteriaproto "laz/generated/nodeproto/hysteria"
	agentcommon "laz/internal/agent/common"
	agentstore "laz/internal/agent/common/store"
	agentsync "laz/internal/agent/hysteria2/sync"
	"laz/internal/legacy/agent/hysteria2/auth"
	agentconfig "laz/internal/legacy/agent/hysteria2/config"
	"laz/internal/legacy/agent/hysteria2/reconcile"
	"laz/internal/legacy/agent/hysteria2/runtime"
	"laz/internal/legacy/agent/hysteria2/statsapi"
	transportstore "laz/internal/queue"
)

const protocolVersion = "agent-grpc-v1"

func Run(ctx context.Context, cfg agentconfig.Config, version string) error {
	st, transport, closeStores, err := openStores(cfg)
	if err != nil {
		return err
	}
	defer closeStores()

	identity := cfg.Identity()
	logger := identity.Logger()
	stats := statsapi.New(cfg.Hysteria2.StatsURL, cfg.Hysteria2.StatsSecret)
	serverClient := agentcommon.NewProtocolClient[*hysteriaproto.EditServerConfig, *hysteriaproto.AuthManifestResponse, *hysteriaproto.ServerMessage](identity, cfg.Config, version, protocolVersion, transport)
	syncer := agentsync.New(st, serverClient, stats)
	delegate := newDelegate(
		syncer,
		reconcile.New(reconcile.Options{NodeID: cfg.NodeID, ConfigPath: cfg.Hysteria2.ConfigPath, ServiceName: cfg.Hysteria2.ServiceName, StatsURL: cfg.Hysteria2.StatsURL, StatsSecret: cfg.Hysteria2.StatsSecret}),
		runtime.New(cfg.Hysteria2.ServiceName, stats, cfg.Runtime.LogLines),
		agentcommon.NewTelemetryCollector(telemetrySource{stats: stats}, st),
		runtime.NewRuntimeCollector(cfg.Hysteria2.ServiceName),
		logger,
	)
	processor := agentcommon.NewProcessor[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage](
		processorConfig(identity, cfg, version), serverClient, delegate, st, transport,
	)

	authServer, err := startAuthServer(ctx, cfg, st, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := authServer.Shutdown(context.Background()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("auth server shutdown failed", "event", "auth_shutdown", "error", err)
		}
	}()

	return processor.Run(ctx)
}

func processorConfig(identity agentcommon.Identity, cfg agentconfig.Config, version string) agentcommon.ProcessorConfig {
	return agentcommon.ProcessorConfig{
		Identity:               identity,
		AgentVersion:           version,
		ProtocolVersion:        protocolVersion,
		CredentialSyncInterval: time.Duration(cfg.Sync.AuthSyncIntervalSeconds) * time.Second,
		TelemetryInterval:      time.Duration(cfg.Sync.TelemetryCollectIntervalSeconds) * time.Second,
		HeartbeatInterval:      time.Duration(cfg.Sync.HeartbeatIntervalSeconds) * time.Second,
		ReconnectMinBackoff:    time.Duration(cfg.Sync.ReconnectMinBackoffSeconds) * time.Second,
		ReconnectMaxBackoff:    time.Duration(cfg.Sync.ReconnectMaxBackoffSeconds) * time.Second,
	}
}

func openStores(cfg agentconfig.Config) (*agentstore.DB, transportstore.Store, func(), error) {
	st, err := agentstore.Open(cfg.StatePath)
	if err != nil {
		return nil, nil, nil, err
	}
	transport, err := transportstore.OpenSQLite(cfg.TransportPath)
	if err != nil {
		logCloseError("state_store", st.Close())
		return nil, nil, nil, err
	}
	return st, transport, func() {
		logCloseError("transport_store", transport.Close())
		logCloseError("state_store", st.Close())
	}, nil
}

func logCloseError(component string, err error) {
	if err != nil {
		log.Printf("component=agent event=close status=error target=%q error=%q", component, err)
	}
}

func startAuthServer(ctx context.Context, cfg agentconfig.Config, st *agentstore.DB, logger *slog.Logger) (*http.Server, error) {
	authServer := &http.Server{Handler: auth.New(st).Handler()}
	listener, err := net.Listen("tcp", cfg.Hysteria2.AuthListen)
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Error("auth listener close failed", "event", "auth_listener_close", "error", err)
		}
	}()
	go func() {
		logger.Info("auth listener started", "event", "auth_listen", "addr", cfg.Hysteria2.AuthListen)
		if err := authServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && ctx.Err() == nil {
			logger.Error("auth listener failed", "event", "auth_listen", "error", err)
		}
	}()
	return authServer, nil
}
