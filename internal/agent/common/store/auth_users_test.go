package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestNativeAuthIdentity(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	user := AuthUser{ConnectionID: "connection", NativeAuthID: "public-key", NativeMeta: json.RawMessage(`{}`)}
	if err := db.UpsertAuthUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if id, err := db.GetConnectionIDByNativeAuthID(ctx, "public-key"); err != nil || id != "connection" {
		t.Fatalf("identity mapping: %q, %v", id, err)
	}
	got, err := db.GetAuthUserByNativeAuthID(ctx, user.NativeAuthID)
	if err != nil || got.ConnectionID != user.ConnectionID || string(got.NativeMeta) != "{}" {
		t.Fatalf("lookup: %+v, %v", got, err)
	}
	user.ConnectionID = "another"
	if err := db.UpsertAuthUser(ctx, user); err == nil {
		t.Fatal("duplicate native auth ID accepted")
	}
	if err := db.DeleteAuthUser(ctx, got.ConnectionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetAuthUserByNativeAuthID(ctx, got.NativeAuthID); err != ErrNotFound {
		t.Fatalf("deleted identity: %v", err)
	}
}
