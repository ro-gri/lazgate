package app

import (
	"context"
	"encoding/json"
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
	transportstore "laz/internal/nodeproto/transport"
)

const protocolVersion = "agent-grpc-v1"

func Run(ctx context.Context, cfg agentconfig.Config, version string) error {
	st, err := agentstore.Open(cfg.StatePath)
	if err != nil {
		return err
	}
	defer st.Close()
	transport, err := transportstore.OpenSQLite(cfg.TransportPath)
	if err != nil {
		return err
	}
	defer transport.Close()
	stats := statsapi.New(cfg.Hysteria2.StatsURL, cfg.Hysteria2.StatsSecret)
	serverClient := connect.NewStream(cfg, version, transport)
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

	syncer := agentsync.New(st, serverClient, stats)
	usageQueue := &transportUsageQueue{nodeID: cfg.NodeID, local: st, transport: transport}
	trafficCollector := traffic.New(cfg.NodeID, stats, usageQueue)
	onlineCollector := online.New(cfg.NodeID, stats)
	executor := runtime.New(cfg.Hysteria2.ServiceName, stats, cfg.Runtime.LogLines)
	go func() {
		for ctx.Err() == nil {
			err := serverClient.Run(ctx, connect.StreamHandler{
				RefreshUserAuth: func(runCtx context.Context, refresh *nodeproto.AuthRefresh) error {
					if len(refresh.GetSnapshots()) > 0 {
						return syncer.ApplySnapshots(runCtx, refresh.GetSnapshots(), refresh.GetManifestStartedAt(), false)
					}
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
		flushUsage(runCtx, usageQueue, serverClient)
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
		_ = transport.RequeueExpiredLeases(runCtx, cfg.NodeID)
		_ = transport.Cleanup(runCtx, transportstore.DefaultCleanupPolicy())
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

type transportUsageQueue struct {
	nodeID    string
	local     *agentstore.DB
	transport transportstore.Store
}

func (q *transportUsageQueue) SaveUsageBatch(ctx context.Context, batch agentstore.UsageBatch) error {
	if len(batch.Records) == 0 {
		return nil
	}
	if err := q.local.SaveUsageBatch(ctx, batch); err != nil {
		return err
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	return q.transport.Enqueue(ctx, transportstore.Message{
		ID:          batch.BatchID,
		ActorID:     q.nodeID,
		Direction:   transportstore.DirectionOutbound,
		Type:        "traffic_batch",
		Payload:     raw,
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(24 * time.Hour),
	})
}

func (q *transportUsageQueue) List(ctx context.Context, limit int) ([]transportstore.Message, error) {
	return q.transport.LeasePending(ctx, q.nodeID, limit, time.Minute)
}

func (q *transportUsageQueue) Ack(ctx context.Context, batchID string, result []byte) error {
	if err := q.transport.MarkAcked(ctx, batchID, result); err != nil {
		return err
	}
	return q.local.DeleteUsageBatch(ctx, batchID)
}

func (q *transportUsageQueue) Fail(ctx context.Context, batchID string, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_ = q.transport.MarkFailed(ctx, batchID, msg, time.Now().UTC().Add(time.Minute))
}

func flushUsage(ctx context.Context, queue *transportUsageQueue, client *connect.StreamClient) {
	messages, err := queue.List(ctx, 100)
	if err != nil {
		return
	}
	for _, msg := range messages {
		if msg.Type != "traffic_batch" {
			continue
		}
		var batch agentstore.UsageBatch
		if err := json.Unmarshal(msg.Payload, &batch); err != nil {
			queue.Fail(ctx, msg.ID, err)
			continue
		}
		ack, err := client.SendUsageBatch(ctx, batch)
		if err != nil || !ack.Ok {
			if err == nil {
				err = errors.New("usage batch rejected")
			}
			queue.Fail(ctx, msg.ID, err)
			return
		}
		raw, _ := json.Marshal(ack)
		_ = queue.Ack(ctx, batch.BatchID, raw)
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
