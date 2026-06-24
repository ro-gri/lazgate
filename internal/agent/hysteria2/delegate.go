package hysteria2

import (
	"context"
	"log/slog"

	hysteriaproto "laz/generated/nodeproto/hysteria"
	agentcommon "laz/internal/agent/common"
	"laz/internal/legacy/agent/hysteria2/reconcile"
	agentruntime "laz/internal/legacy/agent/hysteria2/runtime"

	"google.golang.org/grpc"
)

func newDelegate(syncer agentcommon.CredentialSync[*hysteriaproto.CredentialChange], reconciler *reconcile.Reconciler, executor *agentruntime.Executor, telemetryCollector *agentcommon.TelemetryCollector, runtimeCollector *agentruntime.RuntimeCollector, logger *slog.Logger) *agentcommon.Delegate[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage] {
	return agentcommon.NewDelegate(agentcommon.DelegateConfig[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage]{
		Component: "hysteria2",
		OpenStream: func(ctx context.Context, conn grpc.ClientConnInterface) (agentcommon.Stream[*hysteriaproto.ServerMessage], error) {
			return hysteriaproto.NewAgentClient(conn).Connect(ctx)
		},
		Syncer:             syncer,
		Reconciler:         reconciler,
		Executor:           executor,
		TelemetryCollector: telemetryCollector,
		RuntimeCollector:   runtimeCollector,
		Logger:             logger,
	})
}
