package agenttelemetry

import (
	"context"
	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	"laz/internal/server/model"
	"laz/internal/queue"
	"laz/internal/server/agentprotocol"
	"testing"
)

type adapterStore struct {
	usageStore
	heartbeats, online int
}

func (*adapterStore) GetAgentInstallation(id string) (model.AgentInstallation, error) {
	return model.AgentInstallation{ID: id, NodeInstanceID: "instance"}, nil
}
func (*adapterStore) GetNodeInstance(string) (model.NodeInstance, error) {
	return model.NodeInstance{ID: "instance", NodeID: "node"}, nil
}
func (s *adapterStore) UpsertNodeRuntime(model.NodeRuntime) error { s.heartbeats++; return nil }
func (s *adapterStore) UpsertNodeOnlineConnections(string, []model.NodeOnlineConnection) error {
	s.online++
	return nil
}
func testAdapter[R any](t *testing.T) {
	ctx := context.Background()
	store := &adapterStore{}
	adapter := NewAdapter[R](store, queue.NopStore{}, nil)
	node := agentprotocol.NewStreamNode[*commonproto.AgentMessage, *R]("node", "installation", func() {}, 1, 1)
	adapter.HandleHeartbeat(ctx, node, &commonproto.Heartbeat{NodeId: "node"})
	adapter.HandleOnline(ctx, node, &commonproto.OnlineReport{NodeId: "node", AtMs: 1})
	ack, ok := adapter.HandleUsageBatch(ctx, node, &commonproto.AgentMessage{RequestId: "request", UsageBatch: &commonproto.UsageBatch{NodeId: "node", Records: []*commonproto.UsageRecord{{ConnectionId: "connection-1", RxBytes: 1}}}})
	if store.heartbeats != 1 || store.online != 1 || store.applied != 1 || !ok || !ack.GetOk() {
		t.Fatalf("counts %d %d %d ack %v %v", store.heartbeats, store.online, store.applied, ack, ok)
	}
	blocked := NewAdapter[R](store, queue.NopStore{}, func(string) bool { return false })
	blocked.HandleHeartbeat(ctx, node, &commonproto.Heartbeat{NodeId: "node"})
	blocked.HandleOnline(ctx, node, &commonproto.OnlineReport{NodeId: "node"})
	if store.heartbeats != 1 || store.online != 1 {
		t.Fatal("node restriction ignored")
	}
	wrong := agentprotocol.NewStreamNode[*commonproto.AgentMessage, *R]("other-node", "installation", func() {}, 1, 1)
	adapter.HandleOnline(ctx, wrong, &commonproto.OnlineReport{NodeId: "other-node"})
	if store.online != 1 {
		t.Fatal("installation mismatch ignored")
	}
}
func TestHysteriaTelemetryAdapter(t *testing.T) { testAdapter[hysteriaproto.ServerMessage](t) }
func TestAmneziaTelemetryAdapter(t *testing.T)  { testAdapter[amneziaproto.ServerMessage](t) }
