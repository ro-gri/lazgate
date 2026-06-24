package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/model"
)

func TestSQLiteShortLinksRequireDeviceBoundToken(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	accountToken, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, Token: "account-token", TokenHash: "account-hash", Purpose: model.TokenPurposeClient})
	if err != nil {
		t.Fatal(err)
	}
	clientToken, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: "client-token", TokenHash: "client-hash", Purpose: model.TokenPurposeClient})
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.CreateShortLink(model.ShortLink{
		ID:           "account-link",
		TokenID:      accountToken.ID,
		Profile:      "all",
		TargetURL:    "https://net.example/c/account-link",
		EncryptedURL: "happ://account",
	})
	if err == nil || !strings.Contains(err.Error(), "short_links require client-bound connection token") {
		t.Fatalf("expected client-bound store error, got %v", err)
	}

	_, err = st.CreateShortLink(model.ShortLink{
		ID:           "client-link",
		TokenID:      clientToken.ID,
		Profile:      "all",
		TargetURL:    "https://net.example/c/client-link",
		EncryptedURL: "happ://client",
	})
	if err != nil {
		t.Fatalf("expected client-bound short link to be created, got %v", err)
	}
}

func TestSQLiteRecordsAppliedMigrations(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		var applied bool
		err = st.db.QueryRow(`select is_applied from goose_db_version where version_id = ?`, version).Scan(&applied)
		if err != nil {
			t.Fatal(err)
		}
		if !applied {
			t.Fatalf("expected migration version %d to be applied", version)
		}
	}
}

func TestSQLiteAllowsDuplicateAccountUsernames(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "First"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Second"}); err != nil {
		t.Fatalf("expected duplicate username to be allowed, got %v", err)
	}
}

func TestSQLiteClientSlugIsUniqueOnlyForNotDeletedClients(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "Duplicate iPhone"}); err == nil {
		t.Fatal("expected duplicate active client slug to fail")
	}
	if _, err := st.UpdateClientStatus(first.ID, model.StatusDeleted); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "New iPhone"}); err != nil {
		t.Fatalf("expected deleted client slug to be reusable, got %v", err)
	}
}

func TestSQLiteRepairsAccountClientForeignKeyReferencesAfterUniquenessMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
create table accounts (
  id text primary key,
  username text not null,
  display_name text not null default '',
  status text not null,
  note text not null default '',
  created_at text not null,
  updated_at text not null
);
create table clients (
  id text primary key,
  account_id text not null references accounts(id),
  slug text not null,
  name text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
);
create table account_policy_tags (
  id text primary key,
  account_id text not null references "accounts_unique_old"(id),
  tag_id text not null,
  created_at text not null
);
create table connections (
  id text primary key,
  account_id text not null references "accounts_unique_old"(id),
  client_id text not null references "clients_unique_old"(id),
  node_id text not null,
  protocol text not null,
  remote_id text not null default '',
  remote_name text not null default '',
  status text not null,
  desired_status text not null,
  last_sync_at text,
  last_error text not null default '',
  created_at text not null,
  updated_at text not null
);
create unique index clients_account_slug_unique_active
on clients(account_id, slug)
where status != 'deleted';
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tableName := range []string{"account_policy_tags", "connections"} {
		var sqlText string
		if err := st.queryRow(`select sql from sqlite_master where type = 'table' and name = ?`, tableName).Scan(&sqlText); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(sqlText, "accounts_unique_old") || strings.Contains(sqlText, "clients_unique_old") {
			t.Fatalf("expected repaired schema for %s, got %s", tableName, sqlText)
		}
	}
}

func TestSQLiteRenamesLegacyAccountClientConnectionSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []string{
		`create table goose_db_version (id integer primary key autoincrement, version_id integer not null, is_applied integer not null, tstamp text null)`,
		`insert into goose_db_version(version_id, is_applied, tstamp) values (0, 1, '1970-01-01T00:00:00Z'), (1, 1, '1970-01-01T00:00:00Z'), (2, 1, '1970-01-01T00:00:00Z'), (3, 1, '1970-01-01T00:00:00Z'), (4, 1, '1970-01-01T00:00:00Z'), (5, 1, '1970-01-01T00:00:00Z'), (6, 1, '1970-01-01T00:00:00Z')`,
		`create table users (id text primary key, username text not null unique, display_name text not null default '', status text not null, note text not null default '', created_at text not null, updated_at text not null)`,
		`create table devices (id text primary key, user_id text not null references users(id), slug text not null, name text not null, status text not null, created_at text not null, updated_at text not null, unique(user_id, slug))`,
		`create table nodes (id text primary key, name text not null unique, type text not null, base_url text not null, api_key text not null default '', region text not null default '', ssh_host text not null default '', ssh_port integer not null default 0, ssh_user text not null default '', ssh_key_path text not null default '', use_ipv6 integer not null default 0, status text not null, created_at text not null, updated_at text not null)`,
		`create table accesses (id text primary key, user_id text not null references users(id), device_id text not null references devices(id), node_id text not null references nodes(id), protocol text not null, remote_id text not null default '', remote_name text not null default '', status text not null, desired_status text not null, last_sync_at text, last_error text not null default '', created_at text not null, updated_at text not null)`,
		`create unique index accesses_user_device_node_unique_active on accesses(user_id, device_id, node_id) where status != 'deleted'`,
		`create table issued_configs (id text primary key, access_id text not null references accesses(id), kind text not null, slug text not null default '', name text not null, client text not null default '', content_type text not null default '', config text not null, status text not null, created_at text not null, updated_at text not null)`,
		`create table config_profiles (id text primary key, protocol text not null, kind text not null, slug text not null, name text not null, client text not null default '', content_type text not null default '', config_template text not null, status text not null, created_at text not null, updated_at text not null, unique(protocol, slug, client))`,
		`create table access_tokens (id text primary key, user_id text not null references users(id), device_id text not null default '', token text not null default '', token_hash text not null unique, purpose text not null, status text not null, expires_at text, last_used_at text, created_at text not null, updated_at text not null)`,
		`create table short_links (id text primary key, token_id text not null references access_tokens(id), profile text not null, target_url text not null, encrypted_url text not null, status text not null, created_at text not null, updated_at text not null, unique(token_id, profile))`,
		`create table client_credentials (user_id text primary key references users(id), pin_hash text not null default '', recovery_code_hash text not null default '', failed_attempts integer not null default 0, locked_until text, created_at text not null, updated_at text not null)`,
		`create table client_sessions (id text primary key, user_id text not null references users(id), token text not null default '', token_hash text not null unique, status text not null, expires_at text, last_used_at text, created_at text not null, updated_at text not null)`,
		`create table policy_tags (id text primary key, slug text not null unique, name text not null, allowed_node_ids text not null default '[]', client_limit integer not null default -1, status text not null, created_at text not null, updated_at text not null)`,
		`create table user_policy_tags (id text primary key, user_id text not null references users(id), tag_id text not null references policy_tags(id), created_at text not null, unique(user_id, tag_id))`,
	}
	for _, statement := range legacy {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"accounts", "clients", "connections", "account_policy_tags"} {
		exists, err := st.tableExists(table)
		if err != nil || !exists {
			t.Fatalf("expected table %s to exist, exists=%v err=%v", table, exists, err)
		}
	}
	for _, table := range []string{"users", "devices", "accesses", "user_policy_tags"} {
		exists, err := st.tableExists(table)
		if err != nil || exists {
			t.Fatalf("expected legacy table %s to be gone, exists=%v err=%v", table, exists, err)
		}
	}
	for _, item := range []struct {
		table  string
		column string
	}{
		{"clients", "account_id"},
		{"connections", "account_id"},
		{"connections", "client_id"},
		{"issued_configs", "connection_id"},
		{"access_tokens", "account_id"},
		{"access_tokens", "client_id"},
		{"client_credentials", "account_id"},
		{"client_sessions", "account_id"},
		{"account_policy_tags", "account_id"},
	} {
		exists, err := st.columnExists(item.table, item.column)
		if err != nil || !exists {
			t.Fatalf("expected %s.%s to exist, exists=%v err=%v", item.table, item.column, exists, err)
		}
	}
}

func TestSQLiteClientSummaryReturnsOnlyActiveDeviceScope(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	phone, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "phone", Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	mac, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "mac", Name: "Mac"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{
		Name:       "Core",
		Type:       model.NodeTypeBlitzHysteria,
		BaseURL:    "https://core.example",
		APIKey:     "secret-api-key",
		SSHKeyPath: "/secret/key",
	})
	if err != nil {
		t.Fatal(err)
	}
	phoneConnection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: phone.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	macConnection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: mac.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: phoneConnection.ID, Kind: model.ConfigHy2URI, Name: "phone", Config: "hy2://phone"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: macConnection.ID, Kind: model.ConfigHy2URI, Name: "mac", Config: "hy2://mac"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateConfigProfile(model.ConfigProfile{Protocol: model.ProtocolHysteria2, Kind: model.ConfigKind("hp_subscription"), Slug: "all", Name: "All"}); err != nil {
		t.Fatal(err)
	}

	summary, err := st.ClientSummary(account.ID, phone.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Clients) != 1 || summary.Clients[0].ID != phone.ID {
		t.Fatalf("expected only phone client, got %+v", summary.Clients)
	}
	if len(summary.Connections) != 1 || summary.Connections[0].Connection.ID != phoneConnection.ID {
		t.Fatalf("expected only phone connection, got %+v", summary.Connections)
	}
	if summary.Connections[0].Node.APIKey != "" || summary.Connections[0].Node.SSHKeyPath != "" {
		t.Fatalf("client summary should not load node secrets, got %+v", summary.Connections[0].Node)
	}
	if len(summary.Configs) != 1 || summary.Configs[0].ConnectionID != phoneConnection.ID {
		t.Fatalf("expected only phone config, got %+v", summary.Configs)
	}
	if len(summary.Profiles) == 0 {
		t.Fatalf("expected active hysteria profile, got %+v", summary.Profiles)
	}
	for _, profile := range summary.Profiles {
		if profile.Protocol != model.ProtocolHysteria2 {
			t.Fatalf("expected only hysteria profiles, got %+v", summary.Profiles)
		}
	}

	all, err := st.ClientSummary(account.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Clients) != 2 || len(all.Connections) != 2 || len(all.Configs) != 2 {
		t.Fatalf("expected active account scope, got clients=%d connections=%d configs=%d", len(all.Clients), len(all.Connections), len(all.Configs))
	}
}

func TestSQLiteAuditLogs(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateAuditLog(model.AuditLog{
		Actor:      "admin",
		Action:     "accounts.create",
		EntityType: "account",
		EntityID:   "usr_1",
		Details:    `{"username":"qwerty"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("expected audit metadata, got %+v", created)
	}

	items := st.ListAuditLogs()
	if len(items) != 1 {
		t.Fatalf("expected one audit item, got %d", len(items))
	}
	if items[0].Action != "accounts.create" || items[0].EntityID != "usr_1" {
		t.Fatalf("unexpected audit item: %+v", items[0])
	}
}

func TestSQLiteClientSecurityPolicyStorage(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := st.UpsertClientCredential(model.ClientCredential{
		AccountID:        account.ID,
		PINHash:          "pin",
		RecoveryCodeHash: "recovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.CreatedAt.IsZero() || credential.UpdatedAt.IsZero() {
		t.Fatalf("expected credential timestamps: %+v", credential)
	}
	session, err := st.CreateClientSession(model.ClientSession{
		AccountID: account.ID,
		Token:     "raw",
		TokenHash: "hash",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotSession, err := st.GetClientSessionByHash("hash")
	if err != nil {
		t.Fatal(err)
	}
	if gotSession.ID != session.ID {
		t.Fatalf("unexpected session: %+v", gotSession)
	}
	tag, err := st.CreatePolicyTag(model.PolicyTag{
		Slug:           "family",
		Name:           "Family",
		AllowedNodeIDs: []string{"nod_1"},
		ClientLimit:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := st.AssignPolicyTag(model.AccountPolicyTag{AccountID: account.ID, TagID: tag.ID})
	if err != nil {
		t.Fatal(err)
	}
	if assigned.AccountID != account.ID || assigned.TagID != tag.ID {
		t.Fatalf("unexpected assigned tag: %+v", assigned)
	}
	tags := st.ListPolicyTags()
	if len(tags) != 1 || len(tags[0].AllowedNodeIDs) != 1 || tags[0].AllowedNodeIDs[0] != "nod_1" {
		t.Fatalf("unexpected policy tags: %+v", tags)
	}
}

func TestSQLiteAdminSessionStorage(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateAdminSession(model.AdminSession{
		Token:         "raw",
		TokenHash:     "hash",
		CSRFToken:     "csrf",
		CSRFTokenHash: "csrf-hash",
		PrincipalName: "admin",
		Role:          "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetAdminSessionByHash("hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.PrincipalName != "admin" || got.Role != "owner" {
		t.Fatalf("unexpected admin session: %+v", got)
	}
	if err := st.TouchAdminSession(created.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeAdminSession(created.ID); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetAdminSessionByHash("hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusDeleted {
		t.Fatalf("expected deleted session, got %+v", got)
	}
}
