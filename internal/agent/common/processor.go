package common

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"
	"laz/internal/util"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

type SyncTrigger string

const (
	SyncConnected SyncTrigger = "connected"
	SyncPeriodic  SyncTrigger = "periodic"
	SyncRefresh   SyncTrigger = "auth_refresh"
)

type SyncRequest struct {
	Trigger SyncTrigger
	Refresh *commonproto.AuthRefresh
}

type ProcessorConfig struct {
	Identity               Identity
	AgentVersion           string
	ProtocolVersion        string
	CredentialSyncInterval time.Duration
	TelemetryInterval      time.Duration
	HeartbeatInterval      time.Duration
	ReconnectMinBackoff    time.Duration
	ReconnectMaxBackoff    time.Duration
}

type StateStore interface {
	ObserveTraffic(context.Context, agentstore.TrafficSnapshot) error
	PrepareUsageRequest(context.Context, string, string, int) (agentstore.UsageBatch, bool, error)
	ListPreparedUsageRequests(context.Context, string) ([]agentstore.UsageBatch, error)
	AcknowledgeUsage(context.Context, string, []agentstore.AcknowledgedUsage) error
}

type StreamClient[C ServerConfigCommand, S ServerMessage[C]] interface {
	Run(context.Context, GRPCDelegate[C, S]) error
	SendHeartbeat(context.Context, *commonproto.Heartbeat) error
	SendOnline(context.Context, *commonproto.OnlineReport) error
	SendUsageBatch(context.Context, string, agentstore.UsageBatch) (*commonproto.UsageAck, error)
}

type ProcessorDelegate[C ServerConfigCommand, S ServerMessage[C]] interface {
	OpenStream(context.Context, grpc.ClientConnInterface) (Stream[S], error)
	SyncCredentials(context.Context, SyncRequest) error
	CollectTelemetry(context.Context) TelemetrySnapshot
	RuntimeStatus(context.Context) (string, error)
	ApplyServerConfig(context.Context, C) *commonproto.ServerConfigApplyResult
	ExecuteRuntimeCommand(context.Context, *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult
}

type Processor[C ServerConfigCommand, S ServerMessage[C]] struct {
	cfg       ProcessorConfig
	client    StreamClient[C, S]
	delegate  ProcessorDelegate[C, S]
	state     StateStore
	transport transportstore.Store
	usage     *usageQueue
	logger    *slog.Logger

	backoff      *util.Backoff
	syncMu       sync.Mutex
	trafficMu    sync.Mutex
	syncNow      chan syncInvocation
	heartbeatNow chan struct{}
	usageNow     chan struct{}
}

type syncInvocation struct {
	ctx     context.Context
	request SyncRequest
}

func NewProcessor[C ServerConfigCommand, S ServerMessage[C]](
	cfg ProcessorConfig,
	client StreamClient[C, S],
	delegate ProcessorDelegate[C, S],
	state StateStore,
	transport transportstore.Store,
) *Processor[C, S] {
	cfg = normalizeConfig(cfg)
	return &Processor[C, S]{
		cfg:          cfg,
		client:       client,
		delegate:     delegate,
		state:        state,
		transport:    transport,
		usage:        newUsageQueue(cfg.Identity, state, transport),
		logger:       cfg.Identity.Logger(),
		backoff:      util.NewBackoff(cfg.ReconnectMinBackoff, cfg.ReconnectMaxBackoff),
		syncNow:      make(chan syncInvocation, 1),
		heartbeatNow: make(chan struct{}, 1),
		usageNow:     make(chan struct{}, 1),
	}
}

func (p *Processor[C, S]) Run(ctx context.Context) error {
	if p.client == nil || p.delegate == nil || p.state == nil || p.transport == nil {
		return errors.New("agent processor dependencies are required")
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { p.connectionLoop(groupCtx); return nil })
	group.Go(func() error { p.syncLoop(groupCtx); return nil })
	group.Go(func() error {
		p.runTicker(groupCtx, p.cfg.CredentialSyncInterval, func(context.Context) {
			p.requestSync(groupCtx, SyncRequest{Trigger: SyncPeriodic})
		})
		return nil
	})
	group.Go(func() error { p.telemetryLoop(groupCtx); return nil })
	group.Go(func() error { p.heartbeatLoop(groupCtx); return nil })
	return group.Wait()
}

func (p *Processor[C, S]) OpenStream(ctx context.Context, conn grpc.ClientConnInterface) (Stream[S], error) {
	return p.delegate.OpenStream(ctx, conn)
}

func (p *Processor[C, S]) Connected(ctx context.Context) {
	p.backoff.Reset()
	p.requestSync(ctx, SyncRequest{Trigger: SyncConnected})
	requestSignal(p.heartbeatNow)
	requestSignal(p.usageNow)
}

func (p *Processor[C, S]) ApplyAuthRefresh(ctx context.Context, refresh *commonproto.AuthRefresh) error {
	return p.syncCredentials(ctx, SyncRequest{Trigger: SyncRefresh, Refresh: refresh})
}

func (p *Processor[C, S]) ApplyServerConfig(ctx context.Context, command C) *commonproto.ServerConfigApplyResult {
	return p.delegate.ApplyServerConfig(ctx, command)
}

func (p *Processor[C, S]) ExecuteRuntimeCommand(ctx context.Context, command *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult {
	return p.delegate.ExecuteRuntimeCommand(ctx, command)
}

func (p *Processor[C, S]) connectionLoop(ctx context.Context) {
	for ctx.Err() == nil {
		err := p.client.Run(ctx, p)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("gRPC stream ended")
		}
		delay := p.backoff.Next()
		p.logger.Error("gRPC stream failed", "event", "grpc_stream", "retry_after", delay, "error", err)
		sleepContext(ctx, delay)
	}
}

func (p *Processor[C, S]) syncLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case invocation := <-p.syncNow:
			runCtx := invocation.ctx
			if runCtx == nil {
				runCtx = ctx
			}
			if err := p.syncCredentials(runCtx, invocation.request); err != nil && runCtx.Err() == nil {
				p.logger.Error("credential sync failed", "event", "credential_sync", "trigger", invocation.request.Trigger, "error", err)
			}
		}
	}
}

func (p *Processor[C, S]) requestSync(ctx context.Context, request SyncRequest) {
	select {
	case p.syncNow <- syncInvocation{ctx: ctx, request: request}:
	case <-ctx.Done():
	}
}

func (p *Processor[C, S]) syncCredentials(ctx context.Context, request SyncRequest) error {
	p.syncMu.Lock()
	defer p.syncMu.Unlock()
	// Capture cumulative counters before a sync can remove credentials or peers.
	// A stats failure must not prevent desired-state convergence.
	if err := p.observeTelemetry(ctx); err != nil {
		p.logger.Error("pre-sync traffic collection failed", "event", "traffic_collect", "error", err)
	}
	err := p.delegate.SyncCredentials(ctx, request)
	p.flushUsage(ctx)
	return err
}

func (p *Processor[C, S]) collectTelemetry(ctx context.Context) {
	if err := p.observeTelemetry(ctx); err != nil {
		p.logger.Error("traffic collection failed", "event", "traffic_collect", "error", err)
	}
	p.flushUsage(ctx)
}

func (p *Processor[C, S]) observeTelemetry(ctx context.Context) error {
	p.trafficMu.Lock()
	defer p.trafficMu.Unlock()
	snapshot := p.delegate.CollectTelemetry(ctx)
	trafficErr := snapshot.TrafficError
	if trafficErr == nil {
		trafficErr = p.usage.Observe(ctx, snapshot.Traffic)
	}
	if snapshot.OnlineError != nil {
		p.logger.Error("online collection failed", "event", "online_collect", "error", snapshot.OnlineError)
	} else if snapshot.Online != nil {
		snapshot.Online.NodeId = p.cfg.Identity.NodeID
		if err := p.client.SendOnline(ctx, snapshot.Online); err != nil {
			p.logger.Error("online report send failed", "event", "online_send", "error", err)
		}
	}
	return trafficErr
}

func (p *Processor[C, S]) telemetryLoop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.TelemetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.usageNow:
			runWithTimeout(ctx, p.cfg.TelemetryInterval, p.flushUsage)
		case <-ticker.C:
			runWithTimeout(ctx, p.cfg.TelemetryInterval, p.collectTelemetry)
		}
	}
}

func (p *Processor[C, S]) flushUsage(ctx context.Context) {
	messages, err := p.usage.List(ctx, 100)
	if err != nil {
		return
	}
	for _, msg := range messages {
		batch, err := decodeUsageBatch(msg)
		if err != nil {
			p.usage.Fail(ctx, msg.ID, err)
			continue
		}
		ack, err := p.client.SendUsageBatch(ctx, msg.ID, batch)
		if err != nil || !ack.GetOk() {
			if err == nil {
				err = errors.New("usage batch rejected")
			}
			p.usage.Fail(ctx, msg.ID, err)
			return
		}
		if err := p.usage.Ack(ctx, msg.ID, ack); err != nil {
			p.logger.Error("traffic acknowledgment failed", "event", "traffic_ack", "request_id", msg.ID, "error", err)
		}
	}
}

func (p *Processor[C, S]) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.heartbeatNow:
			p.sendHeartbeatWithTimeout(ctx)
		case <-ticker.C:
			p.sendHeartbeatWithTimeout(ctx)
		}
	}
}

func (p *Processor[C, S]) sendHeartbeatWithTimeout(ctx context.Context) {
	runWithTimeout(ctx, p.cfg.HeartbeatInterval, p.sendHeartbeat)
}

func (p *Processor[C, S]) sendHeartbeat(ctx context.Context) {
	_ = p.transport.RequeueExpiredLeases(ctx, p.cfg.Identity.NodeID)
	_ = p.transport.Cleanup(ctx, transportstore.DefaultCleanupPolicy())
	runtimeStatus, err := p.delegate.RuntimeStatus(ctx)
	if err != nil {
		p.logger.Error("runtime status collection failed", "event", "runtime_status", "error", err)
		runtimeStatus = "unknown"
	}
	heartbeat := commonproto.Heartbeat{
		NodeId:          p.cfg.Identity.NodeID,
		AgentVersion:    p.cfg.AgentVersion,
		ProtocolVersion: p.cfg.ProtocolVersion,
		RuntimeStatus:   runtimeStatus,
	}
	if err := p.client.SendHeartbeat(ctx, &heartbeat); err != nil {
		p.logger.Error("heartbeat send failed", "event", "heartbeat", "error", err)
	}
}

func (p *Processor[C, S]) runTicker(ctx context.Context, interval time.Duration, fn func(context.Context)) {
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
}

func normalizeConfig(cfg ProcessorConfig) ProcessorConfig {
	if cfg.CredentialSyncInterval <= 0 {
		cfg.CredentialSyncInterval = 30 * time.Second
	}
	if cfg.TelemetryInterval <= 0 {
		cfg.TelemetryInterval = 30 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	if cfg.ReconnectMinBackoff <= 0 {
		cfg.ReconnectMinBackoff = time.Second
	}
	if cfg.ReconnectMaxBackoff < cfg.ReconnectMinBackoff {
		cfg.ReconnectMaxBackoff = cfg.ReconnectMinBackoff
	}
	return cfg
}

func runWithTimeout(ctx context.Context, timeout time.Duration, fn func(context.Context)) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fn(runCtx)
}

func requestSignal(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func sleepContext(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
