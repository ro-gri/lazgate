package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var applied int
	if err := st.db.QueryRow(`select is_applied from `+migrationTable+` where version_id = ?`, 1).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("expected migration 1 to be applied, got %d", applied)
	}

	if err := st.SetState(context.Background(), "key", "value"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetState(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if got != "value" {
		t.Fatalf("expected stored state value, got %q", got)
	}

	rows, err := st.db.Query(`pragma table_info(connection_traffic_state)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"observed_rx_bytes", "observed_tx_bytes", "acknowledged_rx_bytes", "acknowledged_tx_bytes", "unacknowledged_rx_bytes", "unacknowledged_tx_bytes", "sent_rx_bytes", "sent_tx_bytes", "request_id"} {
		if !columns[name] {
			t.Fatalf("connection_traffic_state column %q is missing", name)
		}
	}
}

func TestConnectionTrafficStateFreezesDeliveryAndKeepsNewTraffic(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.ObserveTraffic(ctx, TrafficSnapshot{Records: []UsageRecord{{ConnectionID: "connection-1", RXBytes: 100, TXBytes: 50}}}); err != nil {
		t.Fatal(err)
	}
	batch, ok, err := st.PrepareUsageRequest(ctx, "request-1", "node-1", 100)
	if err != nil || !ok || len(batch.Records) != 1 || batch.Records[0].RXBytes != 100 || batch.Records[0].TXBytes != 50 {
		t.Fatalf("unexpected prepared request: batch=%+v ok=%v err=%v", batch, ok, err)
	}
	if err := st.ObserveTraffic(ctx, TrafficSnapshot{Records: []UsageRecord{{ConnectionID: "connection-1", RXBytes: 125, TXBytes: 70}}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := st.ListPreparedUsageRequests(ctx, "node-1")
	if err != nil || len(prepared) != 1 || prepared[0].Records[0].RXBytes != 100 || prepared[0].Records[0].TXBytes != 50 {
		t.Fatalf("frozen request changed: %+v err=%v", prepared, err)
	}
	if err := st.AcknowledgeUsage(ctx, "request-1", []AcknowledgedUsage{{ConnectionID: "connection-1", RXBytes: 100, TXBytes: 50}}); err != nil {
		t.Fatal(err)
	}
	state, err := st.GetConnectionTrafficState(ctx, "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.AcknowledgedRXBytes != 100 || state.AcknowledgedTXBytes != 50 || state.UnacknowledgedRXBytes != 25 || state.UnacknowledgedTXBytes != 20 || state.RequestID != "" {
		t.Fatalf("unexpected finalized state: %+v", state)
	}

	// Replaying an acknowledgement after local finalization is idempotent.
	if err := st.AcknowledgeUsage(ctx, "request-1", []AcknowledgedUsage{{ConnectionID: "connection-1", RXBytes: 100, TXBytes: 50}}); err != nil {
		t.Fatal(err)
	}
	stateAfterReplay, _ := st.GetConnectionTrafficState(ctx, "connection-1")
	if stateAfterReplay != state {
		t.Fatalf("replayed acknowledgement changed state: before=%+v after=%+v", state, stateAfterReplay)
	}
}

func TestConnectionTrafficStateRejectsIncompleteAcknowledgement(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.ObserveTraffic(ctx, TrafficSnapshot{Records: []UsageRecord{{ConnectionID: "connection-1", RXBytes: 10}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.PrepareUsageRequest(ctx, "request-1", "node-1", 100); err != nil || !ok {
		t.Fatalf("prepare failed: ok=%v err=%v", ok, err)
	}
	if err := st.AcknowledgeUsage(ctx, "request-1", nil); err == nil {
		t.Fatal("incomplete acknowledgement must be rejected")
	}
	state, _ := st.GetConnectionTrafficState(ctx, "connection-1")
	if state.RequestID != "request-1" || state.SentRXBytes != 10 {
		t.Fatalf("rejected acknowledgement changed state: %+v", state)
	}
}

func TestConnectionTrafficStateHandlesRuntimeCounterReset(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, rx := range []int64{100, 110, 5, 8} {
		if err := st.ObserveTraffic(ctx, TrafficSnapshot{Records: []UsageRecord{{ConnectionID: "connection-1", RXBytes: rx}}}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := st.GetConnectionTrafficState(ctx, "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	if state.ObservedRXBytes != 8 || state.UnacknowledgedRXBytes != 118 {
		t.Fatalf("unexpected traffic after runtime reset: %+v", state)
	}
}
