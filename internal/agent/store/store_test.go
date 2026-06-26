package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
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
}

func TestOpenMigratesLegacyInlineSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`create table auth_users (
  user_id text primary key,
  credential_id text not null unique,
  username text not null unique,
  password_hash text not null,
  expires_at_ms integer,
  quota_limit_bytes integer,
  last_known_global_usage_bytes integer,
  quota_guard_overage_bytes integer,
  payload_json text not null default ''
);
create table sync_state (
  key text primary key,
  value text not null
);
create table pending_usage_batches (
  batch_id text primary key,
  payload_json text not null,
  created_at_ms integer not null
);
insert into sync_state(key, value) values('cursor', '42');`)
	if closeErr := db.Close(); err != nil {
		t.Fatal(err)
	} else if closeErr != nil {
		t.Fatal(closeErr)
	}

	st, err := Open(path)
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
	got, err := st.GetState(context.Background(), "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if got != "42" {
		t.Fatalf("expected legacy sync state to survive migration, got %q", got)
	}
}
