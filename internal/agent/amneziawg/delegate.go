package amneziawg

import (
	"context"
	"log/slog"

	amneziaproto "laz/generated/nodeproto/amnezia"
	agentcommon "laz/internal/agent/common"

	"laz/internal/legacy/agent/amneziawg/reconcile"
	agentruntime "laz/internal/legacy/agent/amneziawg/runtime"

	"google.golang.org/grpc"
)

func newDelegate(syncer agentcommon.CredentialSync[*amneziaproto.CredentialChange], reconciler *reconcile.Reconciler, executor *agentruntime.Executor, telemetryCollector *agentcommon.TelemetryCollector, runtimeCollector *agentruntime.RuntimeCollector, logger *slog.Logger) *agentcommon.Delegate[*amneziaproto.EditServerConfig, *amneziaproto.ServerMessage] {
	return agentcommon.NewDelegate(agentcommon.DelegateConfig[*amneziaproto.EditServerConfig, *amneziaproto.ServerMessage]{
		Component: "amneziawg",
		OpenStream: func(ctx context.Context, conn grpc.ClientConnInterface) (agentcommon.Stream[*amneziaproto.ServerMessage], error) {
			return amneziaproto.NewAgentClient(conn).Connect(ctx)
		},
		Syncer:             syncer,
		Reconciler:         reconciler,
		Executor:           executor,
		TelemetryCollector: telemetryCollector,
		RuntimeCollector:   runtimeCollector,
		Logger:             logger,
	})
}
