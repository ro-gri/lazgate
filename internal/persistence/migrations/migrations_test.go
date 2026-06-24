package migrations_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	agentstore "laz/internal/agent/common/store"
	"laz/internal/persistence"
	"laz/internal/persistence/migrations"
	"laz/internal/queue"
)

func TestAgentAndQueueShareSQLiteFile(t *testing.T) {
	for _, queueFirst := range []bool{false, true} {
		name := "agent-first"
		if queueFirst {
			name = "queue-first"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shared.db")
			var agent *agentstore.DB
			var messages *queue.SQLStore
			openAgent := func() {
				var err error
				agent, err = agentstore.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { agent.Close() })
			}
			openQueue := func() {
				var err error
				messages, err = queue.OpenSQLite(path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { messages.Close() })
			}
			if queueFirst {
				openQueue()
				openAgent()
			} else {
				openAgent()
				openQueue()
			}
			ctx := context.Background()
			if err := agent.UpsertAuthUser(ctx, agentstore.AuthUser{ConnectionID: "connection", NativeAuthID: "native", NativeMeta: []byte(`{}`)}); err != nil {
				t.Fatal(err)
			}
			if err := messages.Enqueue(ctx, queue.Message{ID: "message", ActorID: "agent", Type: "test", Payload: []byte("payload")}); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"agent_store_goose_db_version", "transport_goose_db_version"} {
				var count int
				if err := messages.DB().QueryRow(`select count(*) from ` + table + ` where version_id = 1 and is_applied = true`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("%s: count=%d error=%v", table, count, err)
				}
			}
		})
	}
}

func TestProvidersUseIndependentMigrationSetsOnSameDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for i := 0; i < 2; i++ {
		for _, table := range []string{"first", "second"} {
			files := fstest.MapFS{"sql/00001_initial.sql": {Data: []byte("-- +goose Up\ncreate table " + table + " (id integer);\n-- +goose Down\ndrop table " + table + ";\n")}}
			if err := migrations.ApplyWithTable(db, files, "sql", persistence.DialectSQLite, table+"_versions"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range []string{"first", "second"} {
		if _, err := db.Exec("insert into " + table + " values(1)"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndependentProvidersCanRunConcurrently(t *testing.T) {
	for _, name := range []string{"one", "two"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			files := fstest.MapFS{"sql/00001_initial.sql": {Data: []byte("-- +goose Up\ncreate table " + name + " (id integer);\n-- +goose Down\ndrop table " + name + ";\n")}}
			if err := migrations.ApplyWithTable(db, files, "sql", persistence.DialectSQLite, name+"_versions"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("insert into " + name + " values(1)"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnsupportedDialectRejected(t *testing.T) {
	if err := migrations.Apply(nil, fstest.MapFS{}, ".", persistence.Dialect("invalid")); err == nil {
		t.Fatal("unsupported dialect accepted")
	}
}
