package store

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"laz/internal/server/model"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresConnectionDataModel(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("LAZ_TEST_POSTGRES_URL"))
	if baseURL == "" {
		t.Skip("set LAZ_TEST_POSTGRES_URL to run PostgreSQL integration tests")
	}
	adminDB, err := sql.Open("pgx", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err = adminDB.Ping(); err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(NewID("target"))
	if _, err = adminDB.Exec(`create schema ` + quotePostgresIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec(`drop schema if exists ` + quotePostgresIdentifier(schema) + ` cascade`) })

	st, err := OpenPostgres(postgresURLWithSchema(t, baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var applied bool
	if err = st.db.QueryRow(`select is_applied from goose_db_version where version_id = 1`).Scan(&applied); err != nil || !applied {
		t.Fatalf("target migration was not applied: applied=%v err=%v", applied, err)
	}
	for _, table := range []string{"connections", "node_instances", "hysteria2_connection_materials", "amneziawg_connection_materials", "account_portal_tokens", "connection_subscription_tokens", "node_online_connections", "usage_records", "connection_usage_totals"} {
		if !postgresTableExists(t, st.db, schema, table) {
			t.Fatalf("target table %q is missing", table)
		}
	}
	for _, table := range []string{"clients", "client_credentials", "access_tokens", "node_online_clients", "amneziawg_peer_materials", "amneziawg_docker_profiles"} {
		if postgresTableExists(t, st.db, schema, table) {
			t.Fatalf("removed table %q still exists", table)
		}
	}
	for table, column := range map[string]string{
		"connections":            "desired_version_ms",
		"hysteria2_node_configs": "config_version_ms",
		"usage_records":          "received_at_ms",
	} {
		var dataType string
		if err = st.db.QueryRow(`select data_type from information_schema.columns where table_schema = $1 and table_name = $2 and column_name = $3`, schema, table, column).Scan(&dataType); err != nil {
			t.Fatal(err)
		}
		if dataType != "bigint" {
			t.Fatalf("%s.%s type = %q, want bigint", table, column, dataType)
		}
	}

	account := mustCreateAccount(t, st, "postgres")
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "postgres-h2")
	createdBundle, err := st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Phone"))
	if err != nil {
		t.Fatal(err)
	}
	created := createdBundle.Connection
	if _, err = st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Phone")); !errors.Is(err, ErrDuplicateConnection) {
		t.Fatalf("expected duplicate connection error, got %v", err)
	}
	if _, err = st.CreateConnectionSubscriptionToken(model.ConnectionSubscriptionToken{ConnectionID: created.ID, Token: "token", TokenHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err = st.UpdateConnectionDesiredStatus(created.ID, model.ConnectionStatusDeleted); err != nil {
		t.Fatal(err)
	}
	if _, err = st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Phone")); err != nil {
		t.Fatalf("PostgreSQL partial unique index did not release deleted name: %v", err)
	}
	testUsageRecordsAtomicity(t, st)
	testMasqueradeFileSets(t, st)
}

func postgresURLWithSchema(t *testing.T, databaseURL, schema string) string {
	t.Helper()
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	return u.String()
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func postgresTableExists(t *testing.T, db *sql.DB, schema, table string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`select exists(select 1 from information_schema.tables where table_schema = $1 and table_name = $2)`, schema, table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}
