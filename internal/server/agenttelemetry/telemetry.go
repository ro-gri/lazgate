package agenttelemetry

import (
	"context"
	"errors"
	"math"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/model"
	transportstore "laz/internal/queue"
	"laz/internal/server/agentprotocol"
	"laz/internal/util"

	"google.golang.org/protobuf/proto"
)

type Store interface {
	GetAgentInstallation(id string) (model.AgentInstallation, error)
	GetNodeInstance(id string) (model.NodeInstance, error)
	UpsertNodeRuntime(model.NodeRuntime) error
	UpsertNodeOnlineConnections(nodeInstanceID string, connections []model.NodeOnlineConnection) error
	ApplyUsageRecords(requestID, nodeInstanceID string, records []model.UsageRecord) ([]model.ConnectionUsageTotal, bool, error)
}

type Handler struct {
	store       Store
	transport   transportstore.Store
	nodeAllowed func(string) bool
}

func New(st Store, transport transportstore.Store, nodeAllowed func(string) bool) Handler {
	if transport == nil {
		transport = transportstore.NopStore{}
	}
	return Handler{store: st, transport: transport, nodeAllowed: nodeAllowed}
}

func Identity(st Store, nodeID, installationID string) (agentprotocol.TelemetryIdentity, bool) {
	if st == nil {
		return agentprotocol.TelemetryIdentity{}, false
	}
	installation, err := st.GetAgentInstallation(installationID)
	if err != nil {
		return agentprotocol.TelemetryIdentity{}, false
	}
	instance, err := st.GetNodeInstance(installation.NodeInstanceID)
	if err != nil || instance.NodeID != nodeID {
		return agentprotocol.TelemetryIdentity{}, false
	}
	return agentprotocol.TelemetryIdentity{NodeID: nodeID, InstallationID: installation.ID, NodeInstanceID: instance.ID}, true
}

func (h Handler) HandleHeartbeat(id agentprotocol.TelemetryIdentity, heartbeat *commonproto.Heartbeat, after func()) bool {
	if heartbeat == nil || heartbeat.GetNodeId() != id.NodeID || !h.allowed(id.NodeID) || h.store == nil {
		return false
	}
	runtime := model.NodeRuntime{
		AgentInstallationID: id.InstallationID,
		NodeInstanceID:      id.NodeInstanceID,
		AgentStatus:         "online",
		LastHeartbeatAt:     time.Now().UTC(),
		AgentVersion:        heartbeat.GetAgentVersion(),
		ProtocolVersion:     heartbeat.GetProtocolVersion(),
		ServiceStatus:       heartbeat.GetRuntimeStatus(),
	}
	_ = h.store.UpsertNodeRuntime(runtime)
	if after != nil {
		after()
	}
	return true
}

func (h Handler) HandleOnline(id agentprotocol.TelemetryIdentity, report *commonproto.OnlineReport) bool {
	if report == nil || report.GetNodeId() != id.NodeID || !h.allowed(id.NodeID) || h.store == nil {
		return false
	}
	connections := make([]model.NodeOnlineConnection, 0, len(report.GetConnections()))
	for _, item := range report.GetConnections() {
		connections = append(connections, model.NodeOnlineConnection{NodeInstanceID: id.NodeInstanceID, ConnectionID: item.GetConnectionId(), NativeAuthID: item.GetNativeAuthId(), Count: int(item.GetCount()), LastSeenAt: util.FromUnixMilli(report.GetAtMs())})
	}
	return h.store.UpsertNodeOnlineConnections(id.NodeInstanceID, connections) == nil
}

func (h Handler) HandleUsage(ctx context.Context, id agentprotocol.TelemetryIdentity, requestID string, batch *commonproto.UsageBatch) (*commonproto.UsageAck, bool) {
	if requestID == "" || !h.allowed(id.NodeID) || h.store == nil {
		return nil, false
	}
	if processed, ok, err := h.transport.IsProcessed(ctx, id.NodeID, requestID); err == nil && ok {
		ack := &commonproto.UsageAck{}
		if proto.Unmarshal(processed.Result, ack) == nil {
			return ack, true
		}
	}
	records, err := decodeUsage(requestID, batch, id)
	if err != nil {
		return &commonproto.UsageAck{Ok: false}, true
	}
	totals, _, err := h.store.ApplyUsageRecords(requestID, id.NodeInstanceID, records)
	if err != nil {
		return &commonproto.UsageAck{Ok: false}, true
	}
	ack := &commonproto.UsageAck{Ok: true, Records: make([]*commonproto.AcknowledgedUsage, 0, len(totals))}
	for _, total := range totals {
		ack.Records = append(ack.Records, &commonproto.AcknowledgedUsage{ConnectionId: total.ConnectionID, RxBytes: total.RXBytes, TxBytes: total.TXBytes})
	}
	raw, err := proto.Marshal(ack)
	if err != nil {
		return &commonproto.UsageAck{Ok: false}, true
	}
	if err := h.transport.RecordProcessed(ctx, id.NodeID, requestID, "traffic_batch", transportstore.StatusApplied, raw, ""); err != nil {
		return &commonproto.UsageAck{Ok: false}, true
	}
	return ack, true
}

func decodeUsage(requestID string, batch *commonproto.UsageBatch, id agentprotocol.TelemetryIdentity) ([]model.UsageRecord, error) {
	if batch == nil {
		return nil, errors.New("empty usage report")
	}
	if batch.GetNodeId() != id.NodeID {
		return nil, errors.New("usage report node mismatch")
	}
	records := make([]model.UsageRecord, 0, len(batch.GetRecords()))
	seen := make(map[string]struct{}, len(batch.GetRecords()))
	for _, record := range batch.GetRecords() {
		if record.GetConnectionId() == "" || record.GetTxBytes() < 0 || record.GetRxBytes() < 0 {
			return nil, errors.New("invalid usage record")
		}
		if record.GetTxBytes() > math.MaxInt64-record.GetRxBytes() {
			return nil, errors.New("usage values overflow")
		}
		if _, duplicate := seen[record.GetConnectionId()]; duplicate {
			return nil, errors.New("duplicate connection in usage report")
		}
		seen[record.GetConnectionId()] = struct{}{}
		records = append(records, model.UsageRecord{RequestID: requestID, NodeInstanceID: id.NodeInstanceID, FromMS: batch.GetFromMs(), ToMS: batch.GetToMs(), ConnectionID: record.GetConnectionId(), TXBytes: record.GetTxBytes(), RXBytes: record.GetRxBytes()})
	}
	if len(records) == 0 {
		return nil, errors.New("usage report has no records")
	}
	return records, nil
}

func (h Handler) allowed(nodeID string) bool {
	return h.nodeAllowed == nil || h.nodeAllowed(nodeID)
}
