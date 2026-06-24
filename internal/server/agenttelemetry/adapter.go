package agenttelemetry

import (
	"context"
	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/queue"
	"laz/internal/server/agentprotocol"
)

type Adapter[R any] struct {
	handler Handler
	store   Store
}

func NewAdapter[R any](store Store, transport queue.Store, allowed func(string) bool) Adapter[R] {
	return Adapter[R]{handler: New(store, transport, allowed), store: store}
}
func (a Adapter[R]) HandleHeartbeat(_ context.Context, node *agentprotocol.StreamNode[*commonproto.AgentMessage, *R], report *commonproto.Heartbeat) {
	if id, ok := Identity(a.store, node.NodeID(), node.InstallationID()); ok {
		a.handler.HandleHeartbeat(id, report, nil)
	}
}
func (a Adapter[R]) HandleOnline(_ context.Context, node *agentprotocol.StreamNode[*commonproto.AgentMessage, *R], report *commonproto.OnlineReport) {
	if id, ok := Identity(a.store, node.NodeID(), node.InstallationID()); ok {
		a.handler.HandleOnline(id, report)
	}
}
func (a Adapter[R]) HandleUsageBatch(ctx context.Context, node *agentprotocol.StreamNode[*commonproto.AgentMessage, *R], msg *commonproto.AgentMessage) (*commonproto.UsageAck, bool) {
	id, ok := Identity(a.store, node.NodeID(), node.InstallationID())
	if !ok {
		return nil, false
	}
	return a.handler.HandleUsage(ctx, id, msg.GetRequestId(), msg.GetUsageBatch())
}
