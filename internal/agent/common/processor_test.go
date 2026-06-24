package common

import (
	"context"
	"sync"
	"testing"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"

	"google.golang.org/grpc"
)

type processorTestClient struct {
	heartbeats chan *commonproto.Heartbeat
	online     chan *commonproto.OnlineReport
}

func (c *processorTestClient) Run(ctx context.Context, delegate GRPCDelegate[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage]) error {
	delegate.Connected(ctx)
	<-ctx.Done()
	return ctx.Err()
}

func (c *processorTestClient) SendHeartbeat(_ context.Context, heartbeat *commonproto.Heartbeat) error {
	c.heartbeats <- heartbeat
	return nil
}

func (c *processorTestClient) SendOnline(_ context.Context, report *commonproto.OnlineReport) error {
	if c.online != nil {
		c.online <- report
	}
	return nil
}

func (*processorTestClient) SendUsageBatch(context.Context, string, agentstore.UsageBatch) (*commonproto.UsageAck, error) {
	return &commonproto.UsageAck{Ok: true}, nil
}

type processorTestDelegate struct {
	mu       sync.Mutex
	requests []SyncRequest
	synced   chan struct{}
	traffic  agentstore.TrafficSnapshot
	online   *commonproto.OnlineReport
}

func (*processorTestDelegate) OpenStream(context.Context, grpc.ClientConnInterface) (Stream[*hysteriaproto.ServerMessage], error) {
	return nil, nil
}

func (d *processorTestDelegate) SyncCredentials(_ context.Context, request SyncRequest) error {
	d.mu.Lock()
	d.requests = append(d.requests, request)
	d.mu.Unlock()
	select {
	case d.synced <- struct{}{}:
	default:
	}
	return nil
}

func (d *processorTestDelegate) CollectTelemetry(context.Context) TelemetrySnapshot {
	return TelemetrySnapshot{Traffic: d.traffic, Online: d.online}
}

func (*processorTestDelegate) RuntimeStatus(context.Context) (string, error) {
	return "active", nil
}

func (*processorTestDelegate) ApplyServerConfig(_ context.Context, command *hysteriaproto.EditServerConfig) *commonproto.ServerConfigApplyResult {
	return &commonproto.ServerConfigApplyResult{DesiredRevision: command.GetRevision(), Status: "applied"}
}

func (*processorTestDelegate) ExecuteRuntimeCommand(_ context.Context, command *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult {
	return &commonproto.RuntimeCommandResult{CommandId: command.GetId(), Status: "ok"}
}

type processorTestState struct {
	snapshots chan agentstore.TrafficSnapshot
}

func (s processorTestState) ObserveTraffic(_ context.Context, snapshot agentstore.TrafficSnapshot) error {
	if s.snapshots != nil {
		s.snapshots <- snapshot
	}
	return nil
}
func (processorTestState) PrepareUsageRequest(context.Context, string, string, int) (agentstore.UsageBatch, bool, error) {
	return agentstore.UsageBatch{}, false, nil
}
func (processorTestState) ListPreparedUsageRequests(context.Context, string) ([]agentstore.UsageBatch, error) {
	return nil, nil
}
func (processorTestState) AcknowledgeUsage(context.Context, string, []agentstore.AcknowledgedUsage) error {
	return nil
}

func TestProcessorRunsConnectedSyncAndHeartbeat(t *testing.T) {
	client := &processorTestClient{heartbeats: make(chan *commonproto.Heartbeat, 2)}
	delegate := &processorTestDelegate{synced: make(chan struct{}, 2)}
	processor := NewProcessor[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage](ProcessorConfig{
		Identity:               Identity{NodeID: "node-1", AgentType: "hysteria2"},
		AgentVersion:           "test",
		ProtocolVersion:        "test-v1",
		CredentialSyncInterval: time.Hour,
		TelemetryInterval:      time.Hour,
		HeartbeatInterval:      time.Hour,
		ReconnectMinBackoff:    time.Hour,
		ReconnectMaxBackoff:    time.Hour,
	}, client, delegate, processorTestState{}, transportstore.NopStore{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- processor.Run(ctx) }()

	select {
	case <-delegate.synced:
	case <-time.After(time.Second):
		t.Fatal("connected credential sync was not started")
	}
	select {
	case heartbeat := <-client.heartbeats:
		if heartbeat.GetNodeId() != "node-1" || heartbeat.GetRuntimeStatus() != "active" {
			t.Fatalf("unexpected heartbeat: %+v", heartbeat)
		}
	case <-time.After(time.Second):
		t.Fatal("connected heartbeat was not sent")
	}

	delegate.mu.Lock()
	request := delegate.requests[0]
	delegate.mu.Unlock()
	if request.Trigger != SyncConnected || request.Refresh != nil {
		t.Fatalf("unexpected sync request: %+v", request)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("processor.Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("processor did not stop")
	}
}

func TestProcessorPassesAuthRefreshToCredentialSync(t *testing.T) {
	delegate := &processorTestDelegate{synced: make(chan struct{}, 1)}
	processor := NewProcessor[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage](ProcessorConfig{}, &processorTestClient{heartbeats: make(chan *commonproto.Heartbeat, 1)}, delegate, processorTestState{}, transportstore.NopStore{})
	refresh := &commonproto.AuthRefresh{NodeId: "node-1", AccountId: "account-1", SnapshotVersionMs: 42}

	if err := processor.ApplyAuthRefresh(context.Background(), refresh); err != nil {
		t.Fatalf("ApplyAuthRefresh() error = %v", err)
	}
	delegate.mu.Lock()
	request := delegate.requests[0]
	delegate.mu.Unlock()
	if request.Trigger != SyncRefresh || request.Refresh != refresh {
		t.Fatalf("unexpected sync request: %+v", request)
	}
}

func TestProcessorStampsIdentityOnTelemetry(t *testing.T) {
	identity := Identity{NodeID: "node-1", AgentType: "hysteria2"}
	client := &processorTestClient{heartbeats: make(chan *commonproto.Heartbeat, 1), online: make(chan *commonproto.OnlineReport, 1)}
	delegate := &processorTestDelegate{
		synced:  make(chan struct{}, 1),
		traffic: agentstore.TrafficSnapshot{Records: []agentstore.UsageRecord{{ConnectionID: "connection-1", TXBytes: 1}}},
		online:  &commonproto.OnlineReport{NodeId: "wrong-node"},
	}
	state := processorTestState{snapshots: make(chan agentstore.TrafficSnapshot, 1)}
	processor := NewProcessor[*hysteriaproto.EditServerConfig, *hysteriaproto.ServerMessage](
		ProcessorConfig{Identity: identity}, client, delegate, state, transportstore.NopStore{},
	)

	processor.collectTelemetry(context.Background())
	if snapshot := <-state.snapshots; len(snapshot.Records) != 1 {
		t.Fatalf("traffic snapshot = %+v", snapshot)
	}

	if report := <-client.online; report.GetNodeId() != identity.NodeID {
		t.Fatalf("online node ID = %q, want %q", report.GetNodeId(), identity.NodeID)
	}
}
