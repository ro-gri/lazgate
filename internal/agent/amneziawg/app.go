package amneziawg

import (
	"context"
	"log"
	"time"

	amneziaproto "laz/generated/nodeproto/amnezia"
	agentcommon "laz/internal/agent/common"
	agentstore "laz/internal/agent/common/store"

	awgsync "laz/internal/agent/amneziawg/sync"

	agentconfig "laz/internal/legacy/agent/amneziawg/config"
	"laz/internal/legacy/agent/amneziawg/reconcile"
	awgruntime "laz/internal/legacy/agent/amneziawg/runtime"
	awgtools "laz/internal/legacy/agent/amneziawg/tools"
	transportstore "laz/internal/queue"
)

const protocolVersion = "amnezia-agent-grpc-v1"

func Run(ctx context.Context, cfg agentconfig.Config, version string) error {
	st, transport, closeStores, err := openStores(cfg)
	if err != nil {
		return err
	}
	defer closeStores()

	identity := cfg.Identity()
	logger := identity.Logger()
	serverClient := agentcommon.NewProtocolClient[*amneziaproto.EditServerConfig, *amneziaproto.AuthManifestResponse, *amneziaproto.ServerMessage](identity, cfg.Config, version, protocolVersion, transport)
	tools := awgtools.New(
		awgtools.WithInterfaceName(cfg.AmneziaWG.InterfaceName),
		awgtools.WithConfigPath(cfg.AmneziaWG.ConfigPath),
		awgtools.WithHostConfigPath("/etc/amnezia/amneziawg/"+cfg.AmneziaWG.InterfaceName+".conf"),
		awgtools.WithServiceName(cfg.AmneziaWG.ServiceName),
		awgtools.WithAWGBinaryPath(cfg.AmneziaWG.AWGBinaryPath),
		awgtools.WithSudo(cfg.AmneziaWG.UseSudo),
		awgtools.WithLogLines(cfg.Runtime.LogLines),
	)
	if err := tools.Restore(ctx); err != nil {
		return err
	}

	reconciler := reconcile.New(tools)
	syncer := awgsync.New(st, reconciler, serverClient, tools)
	delegate := newDelegate(
		syncer,
		reconciler,
		awgruntime.New(tools),
		agentcommon.NewTelemetryCollector(telemetrySource{tools: tools, freshness: 3 * time.Minute}, st),
		awgruntime.NewRuntimeCollector(tools),
		logger,
	)
	processor := agentcommon.NewProcessor[*amneziaproto.EditServerConfig, *amneziaproto.ServerMessage](
		processorConfig(identity, cfg, version), serverClient, delegate, st, transport,
	)
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
