package queue

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresStoreLeaseAndAck(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("LAZ_TEST_POSTGRES_URL"))
	if baseURL == "" {
		t.Skip("set LAZ_TEST_POSTGRES_URL to run PostgreSQL integration tests")
	}
	admin, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("transport_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`create schema ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`drop schema if exists ` + schema + ` cascade`) })

	st, err := OpenPostgres(postgresURLWithSearchPath(t, baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	now := time.Now().UTC()
	message := Message{ID: "message-1", ActorID: "node-1", Type: "auth_refresh", Payload: []byte("payload"), Status: StatusPending, AvailableAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = st.Enqueue(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	leased, err := st.LeasePending(context.Background(), "node-1", 1, time.Minute)
	if err != nil || len(leased) != 1 || leased[0].ID != message.ID || string(leased[0].Payload) != "payload" {
		t.Fatalf("unexpected PostgreSQL lease: items=%+v err=%v", leased, err)
	}
	if err = st.MarkAcked(context.Background(), message.ID, []byte("applied")); err != nil {
		t.Fatal(err)
	}
	var dataType string
	if err = st.db.QueryRow(`select data_type from information_schema.columns where table_schema = $1 and table_name = 'transport_outbox_messages' and column_name = 'available_at_ms'`, schema).Scan(&dataType); err != nil {
		t.Fatal(err)
	}
	if dataType != "bigint" {
		t.Fatalf("available_at_ms type = %q, want bigint", dataType)
	}
}

func postgresURLWithSearchPath(t *testing.T, rawURL, schema string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	return u.String()
}
