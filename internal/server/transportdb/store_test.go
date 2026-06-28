package transportdb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	transportstore "laz/internal/nodeproto/transport"
	"laz/internal/server/events"
	"laz/internal/server/model"
	"laz/internal/server/workqueue"
)

func TestCompleteStoresEventsAndMarksMessageAtomically(t *testing.T) {
	ctx := context.Background()
	transport, store := newTestStores(t)

	leased := enqueueAndLease(t, ctx, transport, "msg-1")
	if err := store.Complete(ctx, leased, workqueue.Result{
		Status: transportstore.StatusApplied,
		Output: []byte("ok"),
		Events: []model.Event{{
			Topic:       events.AdminTopic("admin"),
			Type:        "test.completed",
			EntityType:  "test",
			EntityID:    "entity-1",
			Message:     "done",
			PayloadJSON: `{"ok":true}`,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	pending := store.ListPendingEvents(events.AdminTopic("admin"), 10)
	if len(pending) != 1 {
		t.Fatalf("expected one pending event, got %#v", pending)
	}
	if pending[0].Type != "test.completed" || pending[0].PayloadJSON != `{"ok":true}` {
		t.Fatalf("unexpected event: %#v", pending[0])
	}

	leasedAgain, err := transport.LeasePending(ctx, "server", 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(leasedAgain) != 0 {
		t.Fatalf("expected completed message to be unavailable, got %#v", leasedAgain)
	}
}

func TestCompleteStoresRetryEventsAndRequeuesMessageAtomically(t *testing.T) {
	ctx := context.Background()
	transport, store := newTestStores(t)

	leased := enqueueAndLease(t, ctx, transport, "msg-2")
	retryAt := time.Now().UTC().Add(time.Millisecond)
	if err := store.Complete(ctx, leased, workqueue.Result{
		Status:  transportstore.StatusFailed,
		Output:  []byte("remote failed"),
		RetryAt: retryAt,
		Events: []model.Event{{
			Topic:      events.AdminTopic("admin"),
			Type:       "test.failed",
			EntityType: "test",
			EntityID:   "entity-2",
			Message:    "failed",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	pending := store.ListPendingEvents(events.AdminTopic("admin"), 10)
	if len(pending) != 1 || pending[0].Type != "test.failed" {
		t.Fatalf("expected failure event, got %#v", pending)
	}
	var status string
	var availableAtMS int64
	if err := transport.DB().QueryRow(`select status, available_at_ms from transport_outbox_messages where id = ?`, "msg-2").Scan(&status, &availableAtMS); err != nil {
		t.Fatal(err)
	}
	if status != string(transportstore.StatusPending) || availableAtMS < retryAt.UnixMilli() {
		t.Fatalf("expected pending retryable message, got status=%s available_at_ms=%d", status, availableAtMS)
	}
}

func newTestStores(t *testing.T) (*transportstore.SQLStore, *Store) {
	t.Helper()
	transport, err := transportstore.OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close() })
	store, err := New(transport, "")
	if err != nil {
		t.Fatal(err)
	}
	return transport, store
}

func enqueueAndLease(t *testing.T, ctx context.Context, transport *transportstore.SQLStore, id string) transportstore.Message {
	t.Helper()
	if err := transport.Enqueue(ctx, transportstore.Message{
		ID:          id,
		ActorID:     "server",
		Type:        "test_work",
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC().Add(-time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	leased, err := transport.LeasePending(ctx, "server", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(leased) != 1 {
		t.Fatalf("expected one leased message, got %#v", leased)
	}
	return leased[0]
}
