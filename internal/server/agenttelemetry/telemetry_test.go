package agenttelemetry

import (
	"context"
	"path/filepath"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/agentprotocol"
	transportstore "laz/internal/queue"
	"laz/internal/server/model"
)

type usageStore struct {
	applied int
	totalRX int64
	totalTX int64
}

func (*usageStore) GetAgentInstallation(string) (model.AgentInstallation, error) {
	return model.AgentInstallation{}, nil
}
func (*usageStore) GetNodeInstance(string) (model.NodeInstance, error) {
	return model.NodeInstance{}, nil
}
func (*usageStore) UpsertNodeRuntime(model.NodeRuntime) error { return nil }
func (*usageStore) UpsertNodeOnlineConnections(string, []model.NodeOnlineConnection) error {
	return nil
}
func (s *usageStore) ApplyUsageRecords(_, _ string, records []model.UsageRecord) ([]model.ConnectionUsageTotal, bool, error) {
	s.applied++
	for _, record := range records {
		s.totalRX += record.RXBytes
		s.totalTX += record.TXBytes
	}
	return []model.ConnectionUsageTotal{{ConnectionID: "connection-1", RXBytes: s.totalRX, TXBytes: s.totalTX}}, false, nil
}

func TestUsageInboxReplaysStoredAcknowledgement(t *testing.T) {
	ctx := context.Background()
	transport, err := transportstore.OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	store := &usageStore{}
	handler := New(store, transport, nil)
	identity := agentprotocol.TelemetryIdentity{NodeID: "node-1", NodeInstanceID: "instance-1"}
	batch := &commonproto.UsageBatch{NodeId: "node-1", Records: []*commonproto.UsageRecord{{ConnectionId: "connection-1", RxBytes: 10, TxBytes: 20}}}

	first, ok := handler.HandleUsage(ctx, identity, "request-1", batch)
	if !ok || !first.GetOk() || len(first.GetRecords()) != 1 {
		t.Fatalf("unexpected first ack: %+v ok=%v", first, ok)
	}
	second, ok := handler.HandleUsage(ctx, identity, "request-1", batch)
	if !ok || !second.GetOk() || second.GetRecords()[0].GetRxBytes() != 10 || second.GetRecords()[0].GetTxBytes() != 20 {
		t.Fatalf("unexpected replayed ack: %+v ok=%v", second, ok)
	}
	if store.applied != 1 {
		t.Fatalf("usage side effect executions = %d, want 1", store.applied)
	}
}
