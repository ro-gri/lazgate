package server

import (
	"laz/internal/server/storage"
	"laz/internal/server/transportdb"
	"laz/internal/queue"
	"path/filepath"
	"testing"
)

func newTestServer(t *testing.T, cfg Config, store store.Store) *Server {
	t.Helper()
	transport, err := queue.OpenSQLite(filepath.Join(t.TempDir(), "transport.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { transport.Close() })
	events, err := transportdb.New(transport, cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := queue.WrapSecrets(transport, cfg.SecretKey)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(cfg, Dependencies{Store: store, Transport: protected, Events: events, Completer: events})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
