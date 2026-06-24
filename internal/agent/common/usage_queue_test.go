package common

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"
)

func TestUsageQueueRepairsMissingOutboxAndFinalizesAuthoritativeAck(t *testing.T) {
	ctx := context.Background()
	state, err := agentstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	transport, err := transportstore.OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	if err := state.ObserveTraffic(ctx, agentstore.TrafficSnapshot{Records: []agentstore.UsageRecord{{ConnectionID: "connection-1", RXBytes: 10, TXBytes: 20}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := state.PrepareUsageRequest(ctx, "request-1", "node-1", 100); err != nil || !ok {
		t.Fatalf("prepare: ok=%v err=%v", ok, err)
	}
	queue := newUsageQueue(Identity{NodeID: "node-1"}, state, transport)
	if err := queue.EnsureOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	messages, err := transport.LeasePending(ctx, "node-1", 10, time.Minute)
	if err != nil || len(messages) != 1 || messages[0].ID != "request-1" {
		t.Fatalf("recovered messages=%+v err=%v", messages, err)
	}
	batch, err := decodeUsageBatch(messages[0])
	if err != nil || batch.RequestID != "request-1" || len(batch.Records) != 1 {
		t.Fatalf("decoded batch=%+v err=%v", batch, err)
	}
	ack := &commonproto.UsageAck{Ok: true, Records: []*commonproto.AcknowledgedUsage{{ConnectionId: "connection-1", RxBytes: 100, TxBytes: 200}}}
	if err := queue.Ack(ctx, "request-1", ack); err != nil {
		t.Fatal(err)
	}
	traffic, err := state.GetConnectionTrafficState(ctx, "connection-1")
	if err != nil || traffic.AcknowledgedRXBytes != 100 || traffic.AcknowledgedTXBytes != 200 || traffic.RequestID != "" {
		t.Fatalf("unexpected traffic state=%+v err=%v", traffic, err)
	}
}

func TestUsageQueueStagesAllObservedConnections(t *testing.T) {
	ctx := context.Background()
	state, err := agentstore.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	transport, err := transportstore.OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	snapshot := agentstore.TrafficSnapshot{Records: make([]agentstore.UsageRecord, 0, 201)}
	for index := 0; index < 201; index++ {
		snapshot.Records = append(snapshot.Records, agentstore.UsageRecord{
			ConnectionID: fmt.Sprintf("connection-%03d", index),
			RXBytes:      1,
		})
	}
	if err := state.ObserveTraffic(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	queue := newUsageQueue(Identity{NodeID: "node-1"}, state, transport)
	messages, err := queue.List(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("staged messages = %d, want 3", len(messages))
	}
	var records int
	for _, message := range messages {
		batch, err := decodeUsageBatch(message)
		if err != nil {
			t.Fatal(err)
		}
		records += len(batch.Records)
	}
	if records != 201 {
		t.Fatalf("staged records = %d, want 201", records)
	}
}
