package queue

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStoreLeaseRetryAckAndCleanup(t *testing.T) {
	ctx := context.Background()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertMigrationApplied(t, st, 1)

	now := time.Now().UTC()
	if err := st.Enqueue(ctx, Message{
		ID:          "msg-1",
		ActorID:     "node-1",
		Direction:   DirectionOutbound,
		Type:        "auth_refresh",
		Payload:     []byte("payload"),
		Status:      StatusPending,
		AvailableAt: now.Add(-time.Second),
		ExpiresAt:   now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	leased, err := st.LeasePending(ctx, "node-1", 10, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased) != 1 || leased[0].ID != "msg-1" {
		t.Fatalf("unexpected lease: %#v", leased)
	}
	time.Sleep(2 * time.Millisecond)
	if err := st.RequeueExpiredLeases(ctx, "node-1"); err != nil {
		t.Fatal(err)
	}
	leased, err = st.LeasePending(ctx, "node-1", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased) != 1 {
		t.Fatalf("expected re-leased message, got %#v", leased)
	}
	if err := st.MarkAcked(ctx, "msg-1", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	policy := DefaultCleanupPolicy()
	policy.AuthAckedTTL = time.Nanosecond
	if err := st.Cleanup(ctx, policy); err != nil {
		t.Fatal(err)
	}
	leased, err = st.LeasePending(ctx, "node-1", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased) != 0 {
		t.Fatalf("expected cleaned queue, got %#v", leased)
	}
}

func TestSQLiteStoreProcessedDedup(t *testing.T) {
	ctx := context.Background()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertMigrationApplied(t, st, 1)

	if err := st.RecordProcessed(ctx, "node-1", "req-1", "auth_refresh", StatusApplied, []byte("result"), ""); err != nil {
		t.Fatal(err)
	}
	processed, ok, err := st.IsProcessed(ctx, "node-1", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || processed.Type != "auth_refresh" || string(processed.Result) != "result" {
		t.Fatalf("unexpected processed record: ok=%v record=%#v", ok, processed)
	}
}

func TestSQLiteStoreEnqueueIfAbsentPreservesExistingMessageState(t *testing.T) {
	ctx := context.Background()
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	message := Message{ID: "traffic-1", ActorID: "node-1", Type: "traffic_batch", Payload: []byte("original")}
	if err := st.EnqueueIfAbsent(ctx, message); err != nil {
		t.Fatal(err)
	}
	leased, err := st.LeasePending(ctx, "node-1", 1, time.Minute)
	if err != nil || len(leased) != 1 {
		t.Fatalf("lease: messages=%+v err=%v", leased, err)
	}
	if err := st.EnqueueIfAbsent(ctx, Message{ID: "traffic-1", ActorID: "node-1", Type: "traffic_batch", Payload: []byte("replacement")}); err != nil {
		t.Fatal(err)
	}
	var status string
	var input string
	if err := st.db.QueryRow(`select status, input_json from transport_outbox_messages where actor_id = ? and id = ?`, "node-1", "traffic-1").Scan(&status, &input); err != nil {
		t.Fatal(err)
	}
	if status != string(StatusInFlight) || !strings.Contains(input, "b3JpZ2luYWw=") {
		t.Fatalf("existing message changed: status=%s input=%s", status, input)
	}
}

func assertMigrationApplied(t *testing.T, st *SQLStore, version int64) {
	t.Helper()
	var applied int
	if err := st.db.QueryRow(`select is_applied from `+migrationTable+` where version_id = ?`, version).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("expected migration %d to be applied, got %d", version, applied)
	}
}
