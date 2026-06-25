package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"laz/internal/agent/auth"
	agentconfig "laz/internal/agent/config"
	"laz/internal/agent/connect"
	"laz/internal/agent/hysteria2/statsapi"
	"laz/internal/agent/online"
	"laz/internal/agent/runtime"
	agentstore "laz/internal/agent/store"
	agentsync "laz/internal/agent/sync"
	"laz/internal/agent/traffic"
	"laz/internal/nodeproto"
)

const protocolVersion = "agent-grpc-v1"

func Run(ctx context.Context, cfg agentconfig.Config, version string) error {
	st, err := agentstore.Open(cfg.StatePath)
	if err != nil {
		return err
	}
	defer st.Close()
	stats := statsapi.New(cfg.Hysteria2.StatsURL, cfg.Hysteria2.StatsSecret)
	serverClient := connect.NewStream(cfg, version)
	authServer := &http.Server{Handler: auth.New(st, cfg.Quota.DefaultGuardOverageBytes).Handler()}
	listener, err := net.Listen("tcp", cfg.Hysteria2.AuthListen)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() {
		log.Printf("component=agent event=auth_listen status=ok node_id=%q addr=%q", cfg.NodeID, cfg.Hysteria2.AuthListen)
		if err := authServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("component=agent event=auth_listen status=error node_id=%q error=%q", cfg.NodeID, err)
		}
	}()
	defer authServer.Shutdown(context.Background())

	syncer := agentsync.New(st, serverClient)
	trafficCollector := traffic.New(cfg.NodeID, stats, st)
	onlineCollector := online.New(cfg.NodeID, stats)
	executor := runtime.New(cfg.Hysteria2.ServiceName, stats, cfg.Runtime.LogLines)
	go func() {
		for ctx.Err() == nil {
			err := serverClient.Run(ctx, connect.StreamHandler{
				RefreshUserAuth: func(runCtx context.Context, _ string, _ string) error {
					return syncer.RunOnce(runCtx, false)
				},
				ExecuteCommand: func(runCtx context.Context, command *nodeproto.RuntimeCommand) *nodeproto.RuntimeCommandResult {
					return executor.Execute(runCtx, command)
				},
			})
			if err != nil && ctx.Err() == nil {
				log.Printf("component=agent event=grpc_stream status=error node_id=%q error=%q", cfg.NodeID, err)
				time.Sleep(time.Duration(cfg.Sync.ReconnectMinBackoffSeconds) * time.Second)
			}
		}
	}()

	runTicker(ctx, time.Duration(cfg.Sync.AuthSyncIntervalSeconds)*time.Second, true, func(runCtx context.Context) {
		if err := syncer.RunOnce(runCtx, false); err != nil {
			log.Printf("component=agent event=auth_sync status=error node_id=%q error=%q", cfg.NodeID, err)
		}
	})
	runTicker(ctx, time.Duration(cfg.Sync.TrafficCollectIntervalSeconds)*time.Second, false, func(runCtx context.Context) {
		if _, err := trafficCollector.Collect(runCtx); err != nil {
			log.Printf("component=agent event=traffic_collect status=error node_id=%q error=%q", cfg.NodeID, err)
		}
		flushUsage(runCtx, st, serverClient)
	})
	runTicker(ctx, time.Duration(cfg.Sync.OnlineCollectIntervalSeconds)*time.Second, false, func(runCtx context.Context) {
		report, err := onlineCollector.Collect(runCtx)
		if err != nil {
			log.Printf("component=agent event=online_collect status=error node_id=%q error=%q", cfg.NodeID, err)
			return
		}
		_ = serverClient.SendOnline(runCtx, &nodeproto.OnlineReport{NodeId: report.NodeID, AtMs: report.AtMS, Clients: convertOnline(report.Clients)})
	})
	runTicker(ctx, time.Duration(cfg.Sync.HeartbeatIntervalSeconds)*time.Second, true, func(runCtx context.Context) {
		heartbeat := nodeproto.Heartbeat{
			NodeId:                cfg.NodeID,
			AgentVersion:          version,
			ProtocolVersion:       protocolVersion,
			HysteriaServiceStatus: hysteriaStatus(runCtx, cfg.Hysteria2.ServiceName),
		}
		if count, size, err := st.UsageQueueStats(runCtx); err == nil {
			heartbeat.PendingUsageBatchCount = int32(count)
			heartbeat.PendingUsageQueueSizeBytes = size
		}
		if err := serverClient.SendHeartbeat(runCtx, &heartbeat); err != nil {
			log.Printf("component=agent event=heartbeat status=error node_id=%q error=%q", cfg.NodeID, err)
		}
	})
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return authServer.Shutdown(shutdownCtx)
}

func runTicker(ctx context.Context, interval time.Duration, runNow bool, fn func(context.Context)) {
	go func() {
		if runNow {
			runWithTimeout(ctx, interval, fn)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runWithTimeout(ctx, interval, fn)
			}
		}
	}()
}

func runWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context)) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fn(runCtx)
}

func flushUsage(ctx context.Context, st *agentstore.DB, client *connect.StreamClient) {
	batches, err := st.ListUsageBatches(ctx, 100)
	if err != nil {
		return
	}
	for _, batch := range batches {
		ack, err := client.SendUsageBatch(ctx, batch)
		if err != nil || !ack.Ok {
			return
		}
		_ = st.DeleteUsageBatch(ctx, batch.BatchID)
	}
}

func hysteriaStatus(ctx context.Context, serviceName string) string {
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", serviceName).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func convertOnline(in []online.OnlineClient) []*nodeproto.OnlineClient {
	out := make([]*nodeproto.OnlineClient, 0, len(in))
	for _, item := range in {
		out = append(out, &nodeproto.OnlineClient{CredentialId: item.CredentialID, Count: int32(item.Count)})
	}
	return out
}
