package audit

import (
	"context"
	"testing"

	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/model"
	"laz/internal/server/storage"
)

func TestRecordUsesPrincipal(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	recorder := New(st)
	ctx := adminauthsvc.WithPrincipal(context.Background(), adminauthsvc.Principal{Name: "admin"})

	log, err := recorder.Record(ctx, Event{
		Action:     "accounts.create",
		EntityType: "account",
		EntityID:   "usr_1",
		Details:    map[string]string{"username": "qwerty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if log.Actor != "admin" || log.Action != "accounts.create" || log.EntityID != "usr_1" {
		t.Fatalf("unexpected audit log: %+v", log)
	}
	if log.Details != `{"username":"qwerty"}` {
		t.Fatalf("unexpected details: %s", log.Details)
	}

	items := st.ListAuditLogs()
	if len(items) != 1 {
		t.Fatalf("expected one audit log, got %d", len(items))
	}
}

func TestRecordWithoutPrincipalUsesUnknown(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	log, err := New(st).Record(context.Background(), Event{Action: "x", EntityType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if log.Actor != "unknown" {
		t.Fatalf("unexpected actor: %q", log.Actor)
	}
	if log.CreatedAt.IsZero() || log.ID == "" {
		t.Fatalf("expected stored audit metadata: %+v", log)
	}
}

func TestRecordRejectsUnmarshalableDetails(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(st).Record(context.Background(), Event{
		Action:     "x",
		EntityType: "test",
		Details:    map[string]any{"bad": func() {}},
	})
	if err == nil {
		t.Fatal("expected details marshal error")
	}
}

var _ = model.AuditLog{}
