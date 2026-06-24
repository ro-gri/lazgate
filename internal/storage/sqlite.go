package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"laz/internal/model"
	"laz/internal/storage/record"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db      *sql.DB
	dialect SQLDialect
}

type SQLDialect string

const (
	dialectSQLite   SQLDialect = "sqlite"
	dialectPostgres SQLDialect = "postgres"
)

func OpenSQLite(path string) (*SQLStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`pragma foreign_keys = on; pragma busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	st := &SQLStore{db: db, dialect: dialectSQLite}
	if err := st.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *SQLStore) migrate() error {
	switch s.dialect {
	case dialectSQLite:
		if err := applyMigrations(s.db, s.dialect); err != nil {
			return err
		}
		if err := s.renameLegacyAccountClientConnectionSchema(); err != nil {
			return err
		}
		if err := s.ensureColumn("nodes", "use_ipv6", "integer not null default 0"); err != nil {
			return err
		}
		if err := s.ensureColumn("access_tokens", "token", "text not null default ''"); err != nil {
			return err
		}
		return s.ensurePartialUniqueAccountClientSlugs()
	case dialectPostgres:
		if err := applyMigrations(s.db, s.dialect); err != nil {
			return err
		}
		if err := s.renameLegacyAccountClientConnectionSchema(); err != nil {
			return err
		}
		return s.ensurePartialUniqueAccountClientSlugs()
	default:
		return fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) ensurePartialUniqueAccountClientSlugs() error {
	switch s.dialect {
	case dialectSQLite:
		return s.ensureSQLitePartialUniqueAccountClientSlugs()
	case dialectPostgres:
		return s.ensurePostgresPartialUniqueAccountClientSlugs()
	default:
		return fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) ensureSQLitePartialUniqueAccountClientSlugs() error {
	if err := s.repairSQLiteAccountClientFKReferences(); err != nil {
		return err
	}
	clientIndexExists, err := s.indexExists("clients_account_slug_unique_active")
	if err != nil {
		return err
	}
	accountUsernameUnique, err := s.sqliteTableHasUniqueIndex("accounts", []string{"username"})
	if err != nil {
		return err
	}
	if !accountUsernameUnique && clientIndexExists {
		return nil
	}
	if _, err := s.exec(`pragma foreign_keys = off`); err != nil {
		return err
	}
	defer func() { _, _ = s.exec(`pragma foreign_keys = on`) }()
	if _, err := s.exec(`pragma legacy_alter_table = on`); err != nil {
		return err
	}
	defer func() { _, _ = s.exec(`pragma legacy_alter_table = off`) }()
	steps := []string{
		`alter table accounts rename to accounts_unique_old`,
		`create table accounts (
  id text primary key,
  username text not null,
  display_name text not null default '',
  status text not null,
  note text not null default '',
  created_at text not null,
  updated_at text not null
)`,
		`insert into accounts(id, username, display_name, status, note, created_at, updated_at)
select id, username, display_name, status, note, created_at, updated_at from accounts_unique_old`,
		`drop table accounts_unique_old`,
		`alter table clients rename to clients_unique_old`,
		`create table clients (
  id text primary key,
  account_id text not null references accounts(id),
  slug text not null,
  name text not null,
  status text not null,
  created_at text not null,
  updated_at text not null
)`,
		`insert into clients(id, account_id, slug, name, status, created_at, updated_at)
select id, account_id, slug, name, status, created_at, updated_at from clients_unique_old`,
		`drop table clients_unique_old`,
		`create unique index if not exists clients_account_slug_unique_active on clients(account_id, slug) where status != 'deleted'`,
	}
	for _, step := range steps {
		if _, err := s.exec(step); err != nil {
			return err
		}
	}
	if err := s.repairSQLiteAccountClientFKReferences(); err != nil {
		return err
	}
	return s.sqliteForeignKeyCheck()
}

func (s *SQLStore) repairSQLiteAccountClientFKReferences() error {
	rows, err := s.query(`select name, sql from sqlite_master where type = 'table' and sql is not null and (lower(sql) like '%references%accounts_unique_old%' or lower(sql) like '%references%clients_unique_old%') order by name`)
	if err != nil {
		return err
	}
	type tableSchema struct {
		name string
		sql  string
	}
	var tables []tableSchema
	for rows.Next() {
		var item tableSchema
		if err := rows.Scan(&item.name, &item.sql); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	if _, err := s.exec(`pragma foreign_keys = off`); err != nil {
		return err
	}
	defer func() { _, _ = s.exec(`pragma foreign_keys = on`) }()
	if _, err := s.exec(`pragma legacy_alter_table = on`); err != nil {
		return err
	}
	defer func() { _, _ = s.exec(`pragma legacy_alter_table = off`) }()
	for _, table := range tables {
		if err := s.rebuildSQLiteBrokenFKTable(table.name, table.sql); err != nil {
			return err
		}
	}
	return s.sqliteForeignKeyCheck()
}

func (s *SQLStore) rebuildSQLiteBrokenFKTable(tableName, createSQL string) error {
	columns, err := s.sqliteTableColumns(tableName)
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return fmt.Errorf("table %q has no columns", tableName)
	}
	repairedSQL := strings.ReplaceAll(createSQL, `"accounts_unique_old"`, `accounts`)
	repairedSQL = strings.ReplaceAll(repairedSQL, `"clients_unique_old"`, `clients`)
	repairedSQL = strings.ReplaceAll(repairedSQL, `accounts_unique_old`, `accounts`)
	repairedSQL = strings.ReplaceAll(repairedSQL, `clients_unique_old`, `clients`)
	tmpName := tableName + "_fk_repair_old"
	quotedTable := quoteSQLiteIdent(tableName)
	quotedTmp := quoteSQLiteIdent(tmpName)
	if _, err := s.exec(`drop table if exists ` + quotedTmp); err != nil {
		return err
	}
	if _, err := s.exec(`alter table ` + quotedTable + ` rename to ` + quotedTmp); err != nil {
		return err
	}
	if _, err := s.exec(repairedSQL); err != nil {
		return err
	}
	columnSQL := quoteSQLiteIdentList(columns)
	if _, err := s.exec(`insert into ` + quotedTable + `(` + columnSQL + `) select ` + columnSQL + ` from ` + quotedTmp); err != nil {
		return err
	}
	_, err = s.exec(`drop table ` + quotedTmp)
	return err
}

func (s *SQLStore) sqliteTableColumns(tableName string) ([]string, error) {
	rows, err := s.query(`pragma table_info(` + quoteSQLiteIdent(tableName) + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func (s *SQLStore) sqliteForeignKeyCheck() error {
	rows, err := s.query(`pragma foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign key check failed after account/client uniqueness migration")
	}
	return rows.Err()
}

func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteSQLiteIdentList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, quoteSQLiteIdent(name))
	}
	return strings.Join(quoted, ", ")
}

func (s *SQLStore) ensurePostgresPartialUniqueAccountClientSlugs() error {
	steps := []string{
		`alter table accounts drop constraint if exists accounts_username_key`,
		`drop index if exists accounts_username_unique_active`,
		`alter table clients drop constraint if exists clients_account_id_slug_key`,
		`create unique index if not exists clients_account_slug_unique_active on clients(account_id, slug) where status != 'deleted'`,
	}
	for _, step := range steps {
		if _, err := s.exec(step); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) renameLegacyAccountClientConnectionSchema() error {
	renames := []struct {
		oldName string
		newName string
	}{
		{"users", "accounts"},
		{"devices", "clients"},
		{"accesses", "connections"},
		{"user_policy_tags", "account_policy_tags"},
	}
	for _, item := range renames {
		oldExists, err := s.tableExists(item.oldName)
		if err != nil {
			return err
		}
		newExists, err := s.tableExists(item.newName)
		if err != nil {
			return err
		}
		if oldExists && !newExists {
			if _, err := s.exec(`alter table ` + item.oldName + ` rename to ` + item.newName); err != nil {
				return err
			}
		}
	}

	columnRenames := []struct {
		table   string
		oldName string
		newName string
	}{
		{"clients", "user_id", "account_id"},
		{"connections", "user_id", "account_id"},
		{"connections", "device_id", "client_id"},
		{"issued_configs", "access_id", "connection_id"},
		{"access_tokens", "user_id", "account_id"},
		{"access_tokens", "device_id", "client_id"},
		{"client_credentials", "user_id", "account_id"},
		{"client_sessions", "user_id", "account_id"},
		{"account_policy_tags", "user_id", "account_id"},
	}
	for _, item := range columnRenames {
		if err := s.renameColumnIfNeeded(item.table, item.oldName, item.newName); err != nil {
			return err
		}
	}

	_, _ = s.exec(`drop index if exists accesses_user_device_node_unique_active`)
	_, _ = s.exec(`drop index if exists client_sessions_user_id_idx`)
	_, _ = s.exec(`drop index if exists user_policy_tags_user_id_idx`)
	_, err := s.exec(`create unique index if not exists connections_account_client_node_unique_active on connections(account_id, client_id, node_id) where status != 'deleted'`)
	if err != nil {
		return err
	}
	if _, err := s.exec(`create index if not exists client_sessions_account_id_idx on client_sessions(account_id)`); err != nil {
		return err
	}
	_, err = s.exec(`create index if not exists account_policy_tags_account_id_idx on account_policy_tags(account_id)`)
	return err
}

func (s *SQLStore) renameColumnIfNeeded(table, oldName, newName string) error {
	tableExists, err := s.tableExists(table)
	if err != nil || !tableExists {
		return err
	}
	oldExists, err := s.columnExists(table, oldName)
	if err != nil {
		return err
	}
	newExists, err := s.columnExists(table, newName)
	if err != nil {
		return err
	}
	if oldExists && !newExists {
		_, err = s.exec(`alter table ` + table + ` rename column ` + oldName + ` to ` + newName)
	}
	return err
}

func (s *SQLStore) tableExists(name string) (bool, error) {
	var count int
	switch s.dialect {
	case dialectSQLite:
		err := s.queryRow(`select count(*) from sqlite_master where type = 'table' and name = ?`, name).Scan(&count)
		return count > 0, err
	case dialectPostgres:
		err := s.queryRow(`select count(*) from information_schema.tables where table_schema = current_schema() and table_name = ?`, name).Scan(&count)
		return count > 0, err
	default:
		return false, fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) indexExists(name string) (bool, error) {
	var count int
	switch s.dialect {
	case dialectSQLite:
		err := s.queryRow(`select count(*) from sqlite_master where type = 'index' and name = ?`, name).Scan(&count)
		return count > 0, err
	case dialectPostgres:
		err := s.queryRow(`select count(*) from pg_indexes where schemaname = current_schema() and indexname = ?`, name).Scan(&count)
		return count > 0, err
	default:
		return false, fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) sqliteTableHasUniqueIndex(table string, columns []string) (bool, error) {
	if s.dialect != dialectSQLite {
		return false, nil
	}
	rows, err := s.query(`pragma index_list(` + table + `)`)
	if err != nil {
		return false, err
	}
	uniqueIndexes := []string{}
	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			_ = rows.Close()
			return false, err
		}
		if unique != 1 {
			continue
		}
		uniqueIndexes = append(uniqueIndexes, name)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	for _, name := range uniqueIndexes {
		matches, err := s.sqliteIndexColumnsMatch(name, columns)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}
	return false, nil
}

func (s *SQLStore) sqliteIndexColumnsMatch(indexName string, columns []string) (bool, error) {
	rows, err := s.query(`pragma index_info(` + indexName + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return false, err
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(got) != len(columns) {
		return false, nil
	}
	for i := range got {
		if got[i] != columns[i] {
			return false, nil
		}
	}
	return true, nil
}

func (s *SQLStore) columnExists(table, column string) (bool, error) {
	var count int
	switch s.dialect {
	case dialectSQLite:
		rows, err := s.query(`pragma table_info(` + table + `)`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull int
			var defaultValue any
			var pk int
			if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
				return false, err
			}
			if name == column {
				return true, nil
			}
		}
		return false, rows.Err()
	case dialectPostgres:
		err := s.queryRow(`select count(*) from information_schema.columns where table_schema = current_schema() and table_name = ? and column_name = ?`, table, column).Scan(&count)
		return count > 0, err
	default:
		return false, fmt.Errorf("unsupported sql dialect %q", s.dialect)
	}
}

func (s *SQLStore) ensureColumn(table, column, definition string) error {
	rows, err := s.query(`pragma table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = s.exec(`alter table ` + table + ` add column ` + column + ` ` + definition)
	return err
}

func (s *SQLStore) exec(query string, args ...any) (sql.Result, error) {
	return s.db.Exec(s.rebind(query), args...)
}

func (s *SQLStore) query(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(s.rebind(query), args...)
}

func (s *SQLStore) queryRow(query string, args ...any) *sql.Row {
	return s.db.QueryRow(s.rebind(query), args...)
}

func (s *SQLStore) rebind(query string) string {
	if s.dialect != dialectPostgres {
		return query
	}
	var b strings.Builder
	arg := 1
	for _, r := range query {
		if r == '?' {
			b.WriteString(fmt.Sprintf("$%d", arg))
			arg++
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *SQLStore) CreateAccount(u model.Account) (model.Account, error) {
	t := now()
	u.ID = NewID("usr")
	u.Status = model.StatusActive
	u.CreatedAt = t
	u.UpdatedAt = t
	_, err := s.exec(`insert into accounts(id, username, display_name, status, note, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, u.Status, u.Note, ts(u.CreatedAt), ts(u.UpdatedAt))
	return u, err
}

func (s *SQLStore) GetAccount(id string) (model.Account, error) {
	row := s.queryRow(`select id, username, display_name, status, note, created_at, updated_at from accounts where id = ?`, id)
	return scanAccount(row)
}

func (s *SQLStore) ListAccounts() []model.Account {
	rows, err := s.query(`select id, username, display_name, status, note, created_at, updated_at from accounts order by username`)
	if err != nil {
		return []model.Account{}
	}
	defer rows.Close()
	out := []model.Account{}
	for rows.Next() {
		u, err := scanAccount(rows)
		if err == nil {
			out = append(out, u)
		}
	}
	return out
}

func (s *SQLStore) UpdateAccountStatus(id string, status model.Status) (model.Account, error) {
	_, err := s.exec(`update accounts set status = ?, updated_at = ? where id = ?`, status, ts(now()), id)
	if err != nil {
		return model.Account{}, err
	}
	return s.GetAccount(id)
}

func (s *SQLStore) CreateNode(n model.Node) (model.Node, error) {
	t := now()
	n.ID = NewID("nod")
	n.Status = model.StatusActive
	n.CreatedAt = t
	n.UpdatedAt = t
	_, err := s.exec(`insert into nodes(id, name, type, base_url, api_key, region, ssh_host, ssh_port, ssh_user, ssh_key_path, use_ipv6, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.Name, n.Type, n.BaseURL, n.APIKey, n.Region, n.SSHHost, n.SSHPort, n.SSHUser, n.SSHKeyPath, boolInt(n.UseIPv6), n.Status, ts(n.CreatedAt), ts(n.UpdatedAt))
	return n, err
}

func (s *SQLStore) ListNodes() []model.Node {
	rows, err := s.query(`select id, name, type, base_url, api_key, region, ssh_host, ssh_port, ssh_user, ssh_key_path, use_ipv6, status, created_at, updated_at from nodes order by name`)
	if err != nil {
		return []model.Node{}
	}
	defer rows.Close()
	out := []model.Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

func (s *SQLStore) GetNode(id string) (model.Node, error) {
	row := s.queryRow(`select id, name, type, base_url, api_key, region, ssh_host, ssh_port, ssh_user, ssh_key_path, use_ipv6, status, created_at, updated_at from nodes where id = ?`, id)
	return scanNode(row)
}

func (s *SQLStore) CreateClient(d model.Client) (model.Client, error) {
	if _, err := s.GetAccount(d.AccountID); err != nil {
		return model.Client{}, err
	}
	t := now()
	d.ID = NewID("dev")
	d.Status = model.StatusActive
	d.CreatedAt = t
	d.UpdatedAt = t
	_, err := s.exec(`insert into clients(id, account_id, slug, name, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.AccountID, d.Slug, d.Name, d.Status, ts(d.CreatedAt), ts(d.UpdatedAt))
	return d, err
}

func (s *SQLStore) GetClientForAccount(accountID, clientID string) (model.Client, error) {
	row := s.queryRow(`select id, account_id, slug, name, status, created_at, updated_at from clients where id = ? and account_id = ? limit 1`, clientID, accountID)
	return scanClient(row)
}

func (s *SQLStore) UpdateClientStatus(id string, status model.Status) (model.Client, error) {
	t := now()
	_, err := s.exec(`update clients set status = ?, updated_at = ? where id = ?`, status, ts(t), id)
	if err != nil {
		return model.Client{}, err
	}
	row := s.queryRow(`select id, account_id, slug, name, status, created_at, updated_at from clients where id = ?`, id)
	return scanClient(row)
}

func (s *SQLStore) CountActiveClientsForAccount(accountID string) (int, error) {
	var count int
	err := s.queryRow(`select count(*) from clients where account_id = ? and status = ?`, accountID, model.StatusActive).Scan(&count)
	return count, err
}

func (s *SQLStore) CreateConnection(a model.Connection) (model.Connection, error) {
	var existingID string
	err := s.queryRow(`select id from connections where account_id = ? and client_id = ? and node_id = ? and status != ? limit 1`,
		a.AccountID, a.ClientID, a.NodeID, model.StatusDeleted).Scan(&existingID)
	if err == nil {
		return model.Connection{}, ErrDuplicateConnection
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.Connection{}, err
	}

	t := now()
	a.ID = NewID("acc")
	a.Status = model.StatusActive
	a.DesiredStatus = model.StatusActive
	a.CreatedAt = t
	a.UpdatedAt = t
	_, err = s.exec(`insert into connections(id, account_id, client_id, node_id, protocol, remote_id, remote_name, status, desired_status, last_sync_at, last_error, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AccountID, a.ClientID, a.NodeID, a.Protocol, a.RemoteID, a.RemoteName, a.Status, a.DesiredStatus, tsOrNil(a.LastSyncAt), a.LastError, ts(a.CreatedAt), ts(a.UpdatedAt))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return model.Connection{}, ErrDuplicateConnection
	}
	return a, err
}

func (s *SQLStore) GetConnection(id string) (model.Connection, error) {
	row := s.queryRow(`select id, account_id, client_id, node_id, protocol, remote_id, remote_name, status, desired_status, last_sync_at, last_error, created_at, updated_at from connections where id = ?`, id)
	return scanConnection(row)
}

func (s *SQLStore) UpdateConnectionStatus(id string, status model.Status, lastErr string) (model.Connection, error) {
	t := now()
	_, err := s.exec(`update connections set status = ?, desired_status = ?, last_error = ?, last_sync_at = ?, updated_at = ? where id = ?`,
		status, status, lastErr, ts(t), ts(t), id)
	if err != nil {
		return model.Connection{}, err
	}
	return s.GetConnection(id)
}

func (s *SQLStore) ListConnections() []model.Connection {
	rows, err := s.query(`select id, account_id, client_id, node_id, protocol, remote_id, remote_name, status, desired_status, last_sync_at, last_error, created_at, updated_at from connections order by created_at`)
	if err != nil {
		return []model.Connection{}
	}
	defer rows.Close()
	out := []model.Connection{}
	for rows.Next() {
		a, err := scanConnection(rows)
		if err == nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *SQLStore) CreateIssuedConfig(c model.IssuedConfig) (model.IssuedConfig, error) {
	t := now()
	c.ID = NewID("cfg")
	c.Status = model.StatusActive
	c.CreatedAt = t
	c.UpdatedAt = t
	_, err := s.exec(`insert into issued_configs(id, connection_id, kind, slug, name, client, content_type, config, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ConnectionID, c.Kind, c.Slug, c.Name, c.Client, c.ContentType, c.Config, c.Status, ts(c.CreatedAt), ts(c.UpdatedAt))
	return c, err
}

func (s *SQLStore) ListIssuedConfigs() []model.IssuedConfig {
	rows, err := s.query(`select id, connection_id, kind, slug, name, client, content_type, config, status, created_at, updated_at from issued_configs order by created_at`)
	if err != nil {
		return []model.IssuedConfig{}
	}
	defer rows.Close()
	out := []model.IssuedConfig{}
	for rows.Next() {
		c, err := scanConfig(rows)
		if err == nil {
			out = append(out, c)
		}
	}
	return out
}

func (s *SQLStore) RevokeConfigsForConnection(connectionID string) error {
	_, err := s.exec(`update issued_configs set status = ?, updated_at = ? where connection_id = ? and status != ?`,
		model.StatusDeleted, ts(now()), connectionID, model.StatusDeleted)
	return err
}

func (s *SQLStore) CreateConfigProfile(p model.ConfigProfile) (model.ConfigProfile, error) {
	t := now()
	p.ID = NewID("prf")
	p.Status = model.StatusActive
	p.CreatedAt = t
	p.UpdatedAt = t
	_, err := s.exec(`insert into config_profiles(id, protocol, kind, slug, name, client, content_type, config_template, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Protocol, p.Kind, p.Slug, p.Name, p.Client, p.ContentType, p.ConfigTemplate, p.Status, ts(p.CreatedAt), ts(p.UpdatedAt))
	return p, err
}

func (s *SQLStore) ListConfigProfiles() []model.ConfigProfile {
	rows, err := s.query(`select id, protocol, kind, slug, name, client, content_type, config_template, status, created_at, updated_at from config_profiles order by protocol, slug`)
	if err != nil {
		return []model.ConfigProfile{}
	}
	defer rows.Close()
	out := []model.ConfigProfile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (s *SQLStore) CreateAccessToken(t model.AccessToken) (model.AccessToken, error) {
	if _, err := s.GetAccount(t.AccountID); err != nil {
		return model.AccessToken{}, err
	}
	now := now()
	t.ID = NewID("tok")
	t.Status = model.StatusActive
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.exec(`insert into access_tokens(id, account_id, client_id, token, token_hash, purpose, status, expires_at, last_used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.AccountID, t.ClientID, t.Token, t.TokenHash, t.Purpose, t.Status, tsOrNil(t.ExpiresAt), tsOrNil(t.LastUsedAt), ts(t.CreatedAt), ts(t.UpdatedAt))
	return t, err
}

func (s *SQLStore) ListAccessTokens() []model.AccessToken {
	rows, err := s.query(`select id, account_id, client_id, token, token_hash, purpose, status, expires_at, last_used_at, created_at, updated_at from access_tokens order by created_at`)
	if err != nil {
		return []model.AccessToken{}
	}
	defer rows.Close()
	out := []model.AccessToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err == nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *SQLStore) GetAccessTokenByHash(hash string) (model.AccessToken, error) {
	row := s.queryRow(`select id, account_id, client_id, token, token_hash, purpose, status, expires_at, last_used_at, created_at, updated_at from access_tokens where token_hash = ?`, hash)
	return scanToken(row)
}

func (s *SQLStore) TouchAccessToken(id string) error {
	_, err := s.exec(`update access_tokens set last_used_at = ?, updated_at = ? where id = ?`, ts(now()), ts(now()), id)
	return err
}

func (s *SQLStore) CreateAdminSession(session model.AdminSession) (model.AdminSession, error) {
	now := now()
	session.ID = NewID("adm")
	session.Status = model.StatusActive
	session.CreatedAt = now
	session.UpdatedAt = now
	_, err := s.exec(`insert into admin_sessions(id, token, token_hash, csrf_token, csrf_token_hash, principal_name, role, status, expires_at, last_used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Token, session.TokenHash, session.CSRFToken, session.CSRFTokenHash, session.PrincipalName, session.Role, session.Status, tsOrNil(session.ExpiresAt), tsOrNil(session.LastUsedAt), ts(session.CreatedAt), ts(session.UpdatedAt))
	return session, err
}

func (s *SQLStore) GetAdminSessionByHash(hash string) (model.AdminSession, error) {
	row := s.queryRow(`select id, token, token_hash, csrf_token, csrf_token_hash, principal_name, role, status, expires_at, last_used_at, created_at, updated_at from admin_sessions where token_hash = ?`, hash)
	return scanAdminSession(row)
}

func (s *SQLStore) TouchAdminSession(id string) error {
	_, err := s.exec(`update admin_sessions set last_used_at = ?, updated_at = ? where id = ?`, ts(now()), ts(now()), id)
	return err
}

func (s *SQLStore) RevokeAdminSession(id string) error {
	_, err := s.exec(`update admin_sessions set status = ?, updated_at = ? where id = ?`, model.StatusDeleted, ts(now()), id)
	return err
}

func (s *SQLStore) UpsertClientCredential(credential model.ClientCredential) (model.ClientCredential, error) {
	if _, err := s.GetAccount(credential.AccountID); err != nil {
		return model.ClientCredential{}, err
	}
	now := now()
	existing, err := s.GetClientCredential(credential.AccountID)
	if err == nil {
		credential.CreatedAt = existing.CreatedAt
	} else {
		credential.CreatedAt = now
	}
	credential.UpdatedAt = now
	_, err = s.exec(`insert into client_credentials(account_id, pin_hash, recovery_code_hash, failed_attempts, locked_until, created_at, updated_at)
values(?, ?, ?, ?, ?, ?, ?)
on conflict(account_id) do update set pin_hash = excluded.pin_hash, recovery_code_hash = excluded.recovery_code_hash, failed_attempts = excluded.failed_attempts, locked_until = excluded.locked_until, updated_at = excluded.updated_at`,
		credential.AccountID, credential.PINHash, credential.RecoveryCodeHash, credential.FailedAttempts, tsOrNil(credential.LockedUntil), ts(credential.CreatedAt), ts(credential.UpdatedAt))
	return credential, err
}

func (s *SQLStore) GetClientCredential(accountID string) (model.ClientCredential, error) {
	row := s.queryRow(`select account_id, pin_hash, recovery_code_hash, failed_attempts, locked_until, created_at, updated_at from client_credentials where account_id = ?`, accountID)
	return scanClientCredential(row)
}

func (s *SQLStore) UpdateClientCredentialAuthState(accountID string, failedAttempts int, lockedUntil time.Time) (model.ClientCredential, error) {
	_, err := s.exec(`update client_credentials set failed_attempts = ?, locked_until = ?, updated_at = ? where account_id = ?`,
		failedAttempts, tsOrNil(lockedUntil), ts(now()), accountID)
	if err != nil {
		return model.ClientCredential{}, err
	}
	return s.GetClientCredential(accountID)
}

func (s *SQLStore) CreateClientSession(session model.ClientSession) (model.ClientSession, error) {
	if _, err := s.GetAccount(session.AccountID); err != nil {
		return model.ClientSession{}, err
	}
	now := now()
	session.ID = NewID("ses")
	session.Status = model.StatusActive
	session.CreatedAt = now
	session.UpdatedAt = now
	_, err := s.exec(`insert into client_sessions(id, account_id, token, token_hash, status, expires_at, last_used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.AccountID, session.Token, session.TokenHash, session.Status, tsOrNil(session.ExpiresAt), tsOrNil(session.LastUsedAt), ts(session.CreatedAt), ts(session.UpdatedAt))
	return session, err
}

func (s *SQLStore) GetClientSessionByHash(hash string) (model.ClientSession, error) {
	row := s.queryRow(`select id, account_id, token, token_hash, status, expires_at, last_used_at, created_at, updated_at from client_sessions where token_hash = ?`, hash)
	return scanClientSession(row)
}

func (s *SQLStore) TouchClientSession(id string) error {
	_, err := s.exec(`update client_sessions set last_used_at = ?, updated_at = ? where id = ?`, ts(now()), ts(now()), id)
	return err
}

func (s *SQLStore) RevokeClientSession(id string) error {
	_, err := s.exec(`update client_sessions set status = ?, updated_at = ? where id = ?`, model.StatusDeleted, ts(now()), id)
	return err
}

func (s *SQLStore) RevokeClientSessionsForAccount(accountID string) error {
	_, err := s.exec(`update client_sessions set status = ?, updated_at = ? where account_id = ? and status != ?`,
		model.StatusDeleted, ts(now()), accountID, model.StatusDeleted)
	return err
}

func (s *SQLStore) CreatePolicyTag(tag model.PolicyTag) (model.PolicyTag, error) {
	now := now()
	tag.ID = NewID("tag")
	tag.Status = model.StatusActive
	tag.CreatedAt = now
	tag.UpdatedAt = now
	if tag.ClientLimit == 0 {
		tag.ClientLimit = model.ClientLimitUnlimited
	}
	allowed, err := json.Marshal(tag.AllowedNodeIDs)
	if err != nil {
		return model.PolicyTag{}, err
	}
	_, err = s.exec(`insert into policy_tags(id, slug, name, allowed_node_ids, client_limit, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?)`,
		tag.ID, tag.Slug, tag.Name, string(allowed), tag.ClientLimit, tag.Status, ts(tag.CreatedAt), ts(tag.UpdatedAt))
	return tag, err
}

func (s *SQLStore) ListPolicyTags() []model.PolicyTag {
	rows, err := s.query(`select id, slug, name, allowed_node_ids, client_limit, status, created_at, updated_at from policy_tags order by slug`)
	if err != nil {
		return []model.PolicyTag{}
	}
	defer rows.Close()
	out := []model.PolicyTag{}
	for rows.Next() {
		tag, err := scanPolicyTag(rows)
		if err == nil {
			out = append(out, tag)
		}
	}
	return out
}

func (s *SQLStore) AssignPolicyTag(userTag model.AccountPolicyTag) (model.AccountPolicyTag, error) {
	if _, err := s.GetAccount(userTag.AccountID); err != nil {
		return model.AccountPolicyTag{}, err
	}
	now := now()
	userTag.ID = NewID("utag")
	userTag.CreatedAt = now
	_, err := s.exec(`insert into account_policy_tags(id, account_id, tag_id, created_at) values(?, ?, ?, ?) on conflict(account_id, tag_id) do nothing`,
		userTag.ID, userTag.AccountID, userTag.TagID, ts(userTag.CreatedAt))
	if err != nil {
		return model.AccountPolicyTag{}, err
	}
	return userTag, nil
}

func (s *SQLStore) ListAccountPolicyTags(accountID string) []model.AccountPolicyTag {
	rows, err := s.query(`select id, account_id, tag_id, created_at from account_policy_tags where account_id = ? order by created_at`, accountID)
	if err != nil {
		return []model.AccountPolicyTag{}
	}
	defer rows.Close()
	out := []model.AccountPolicyTag{}
	for rows.Next() {
		userTag, err := scanAccountPolicyTag(rows)
		if err == nil {
			out = append(out, userTag)
		}
	}
	return out
}

func (s *SQLStore) CreateShortLink(link model.ShortLink) (model.ShortLink, error) {
	token, err := s.getAccessTokenByID(link.TokenID)
	if err != nil {
		return model.ShortLink{}, err
	}
	if token.ClientID == "" {
		return model.ShortLink{}, errors.New("short_links require client-bound connection token")
	}
	t := now()
	link.Status = model.StatusActive
	link.CreatedAt = t
	link.UpdatedAt = t
	_, err = s.exec(`insert into short_links(id, token_id, profile, target_url, encrypted_url, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID, link.TokenID, link.Profile, link.TargetURL, link.EncryptedURL, link.Status, ts(link.CreatedAt), ts(link.UpdatedAt))
	return link, err
}

func (s *SQLStore) getAccessTokenByID(id string) (model.AccessToken, error) {
	row := s.queryRow(`select id, account_id, client_id, token, token_hash, purpose, status, expires_at, last_used_at, created_at, updated_at from access_tokens where id = ?`, id)
	return scanToken(row)
}

func (s *SQLStore) GetShortLink(id string) (model.ShortLink, error) {
	row := s.queryRow(`select id, token_id, profile, target_url, encrypted_url, status, created_at, updated_at from short_links where id = ?`, id)
	return scanShortLink(row)
}

func (s *SQLStore) GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error) {
	row := s.queryRow(`select id, token_id, profile, target_url, encrypted_url, status, created_at, updated_at from short_links where token_id = ? and profile = ? and status != ? limit 1`,
		tokenID, profile, model.StatusDeleted)
	return scanShortLink(row)
}

func (s *SQLStore) CreateAuditLog(log model.AuditLog) (model.AuditLog, error) {
	log.ID = NewID("aud")
	log.CreatedAt = now()
	_, err := s.exec(`insert into audit_logs(id, actor, action, entity_type, entity_id, details, created_at) values(?, ?, ?, ?, ?, ?, ?)`,
		log.ID, log.Actor, log.Action, log.EntityType, log.EntityID, log.Details, ts(log.CreatedAt))
	return log, err
}

func (s *SQLStore) ListAuditLogs() []model.AuditLog {
	rows, err := s.query(`select id, actor, action, entity_type, entity_id, details, created_at from audit_logs order by created_at desc`)
	if err != nil {
		return []model.AuditLog{}
	}
	defer rows.Close()
	out := []model.AuditLog{}
	for rows.Next() {
		log, err := scanAuditLog(rows)
		if err == nil {
			out = append(out, log)
		}
	}
	return out
}

func (s *SQLStore) Summary(accountID string) (model.AccountSummary, error) {
	account, err := s.GetAccount(accountID)
	if err != nil {
		return model.AccountSummary{}, err
	}
	summary := model.AccountSummary{
		Account:     account,
		Clients:     []model.Client{},
		Connections: []model.ConnectionSummary{},
		Configs:     []model.IssuedConfig{},
		Profiles:    []model.ConfigProfile{},
		Generated:   now(),
	}
	includeDeleted := account.Status == model.StatusDeleted
	clients := s.listClientsForAccount(accountID, includeDeleted)
	summary.Clients = clients
	clientByID := map[string]model.Client{}
	for _, d := range clients {
		clientByID[d.ID] = d
	}
	activeProtocols := map[model.Protocol]bool{}
	summary.Connections = s.listConnectionSummariesForAccount(accountID, includeDeleted, clientByID)
	for _, item := range summary.Connections {
		if item.Connection.Status == model.StatusActive {
			activeProtocols[item.Connection.Protocol] = true
		}
	}
	summary.Configs = s.listConfigsForAccount(accountID, includeDeleted)
	summary.Profiles = s.listActiveProfilesForProtocols(activeProtocols)
	return summary, nil
}

func (s *SQLStore) ClientSummary(accountID, clientID string) (model.AccountSummary, error) {
	account, err := s.GetAccount(accountID)
	if err != nil {
		return model.AccountSummary{}, err
	}
	summary := model.AccountSummary{
		Account:     account,
		Clients:     []model.Client{},
		Connections: []model.ConnectionSummary{},
		Configs:     []model.IssuedConfig{},
		Profiles:    []model.ConfigProfile{},
		Generated:   now(),
	}
	if account.Status != model.StatusActive {
		return summary, nil
	}
	summary.Clients = s.listActiveClientsForAccount(accountID, clientID)
	clientByID := map[string]model.Client{}
	for _, d := range summary.Clients {
		clientByID[d.ID] = d
	}
	summary.Connections = s.listClientConnectionSummaries(accountID, clientID, clientByID)

	activeProtocols := map[model.Protocol]bool{}
	activeConnectionIDs := []string{}
	for _, item := range summary.Connections {
		activeProtocols[item.Connection.Protocol] = true
		activeConnectionIDs = append(activeConnectionIDs, item.Connection.ID)
	}
	summary.Configs = s.listActiveConfigsForConnections(activeConnectionIDs)
	summary.Profiles = s.listActiveProfilesForProtocols(activeProtocols)
	return summary, nil
}

func (s *SQLStore) listClientsForAccount(accountID string, includeDeleted bool) []model.Client {
	query := `select id, account_id, slug, name, status, created_at, updated_at from clients where account_id = ?`
	args := []any{accountID}
	if !includeDeleted {
		query += ` and status != ?`
		args = append(args, model.StatusDeleted)
	}
	query += ` order by slug`
	rows, err := s.query(query, args...)
	if err != nil {
		return []model.Client{}
	}
	defer rows.Close()
	out := []model.Client{}
	for rows.Next() {
		d, err := scanClient(rows)
		if err == nil {
			out = append(out, d)
		}
	}
	return out
}

func (s *SQLStore) listConnectionSummariesForAccount(accountID string, includeDeleted bool, clientByID map[string]model.Client) []model.ConnectionSummary {
	query := `
select
	a.id, a.account_id, a.client_id, a.node_id, a.protocol, a.remote_id, a.remote_name, a.status, a.desired_status, a.last_sync_at, a.last_error, a.created_at, a.updated_at,
	n.id, n.name, n.type, n.base_url, '', n.region, n.ssh_host, n.ssh_port, n.ssh_user, '', n.use_ipv6, n.status, n.created_at, n.updated_at
from connections a
join nodes n on n.id = a.node_id
where a.account_id = ?`
	args := []any{accountID}
	if !includeDeleted {
		query += ` and a.status != ?`
		args = append(args, model.StatusDeleted)
	}
	query += ` order by a.created_at, n.name`
	rows, err := s.query(query, args...)
	if err != nil {
		return []model.ConnectionSummary{}
	}
	defer rows.Close()
	out := []model.ConnectionSummary{}
	for rows.Next() {
		connection, node, err := scanConnectionNode(rows)
		if err != nil {
			continue
		}
		out = append(out, model.ConnectionSummary{
			Connection: connection,
			Node:       node,
			Client:     clientByID[connection.ClientID],
		})
	}
	return out
}

func (s *SQLStore) listConfigsForAccount(accountID string, includeDeleted bool) []model.IssuedConfig {
	query := `
select c.id, c.connection_id, c.kind, c.slug, c.name, c.client, c.content_type, c.config, c.status, c.created_at, c.updated_at
from issued_configs c
join connections a on a.id = c.connection_id
where a.account_id = ?`
	args := []any{accountID}
	if !includeDeleted {
		query += ` and c.status = ?`
		args = append(args, model.StatusActive)
	}
	query += ` order by c.name`
	rows, err := s.query(query, args...)
	if err != nil {
		return []model.IssuedConfig{}
	}
	defer rows.Close()
	out := []model.IssuedConfig{}
	for rows.Next() {
		cfg, err := scanConfig(rows)
		if err == nil {
			out = append(out, cfg)
		}
	}
	return out
}

func (s *SQLStore) listActiveClientsForAccount(accountID, clientID string) []model.Client {
	query := `select id, account_id, slug, name, status, created_at, updated_at from clients where account_id = ? and status = ?`
	args := []any{accountID, model.StatusActive}
	if clientID != "" {
		query += ` and id = ?`
		args = append(args, clientID)
	}
	query += ` order by slug`
	rows, err := s.query(query, args...)
	if err != nil {
		return []model.Client{}
	}
	defer rows.Close()
	out := []model.Client{}
	for rows.Next() {
		d, err := scanClient(rows)
		if err == nil {
			out = append(out, d)
		}
	}
	return out
}

func (s *SQLStore) listClientConnectionSummaries(accountID, clientID string, clientByID map[string]model.Client) []model.ConnectionSummary {
	query := `
select
	a.id, a.account_id, a.client_id, a.node_id, a.protocol, a.remote_id, a.remote_name, a.status, a.desired_status, a.last_sync_at, a.last_error, a.created_at, a.updated_at,
	n.id, n.name, n.type, n.base_url, '', n.region, n.ssh_host, n.ssh_port, n.ssh_user, '', n.use_ipv6, n.status, n.created_at, n.updated_at
from connections a
join clients d on d.id = a.client_id
join nodes n on n.id = a.node_id
where a.account_id = ? and a.status = ? and d.status = ? and n.status = ?`
	args := []any{accountID, model.StatusActive, model.StatusActive, model.StatusActive}
	if clientID != "" {
		query += ` and a.client_id = ?`
		args = append(args, clientID)
	}
	query += ` order by d.slug, n.name`
	rows, err := s.query(query, args...)
	if err != nil {
		return []model.ConnectionSummary{}
	}
	defer rows.Close()
	out := []model.ConnectionSummary{}
	for rows.Next() {
		connection, node, err := scanConnectionNode(rows)
		if err != nil {
			continue
		}
		out = append(out, model.ConnectionSummary{
			Connection: connection,
			Node:       node,
			Client:     clientByID[connection.ClientID],
		})
	}
	return out
}

func (s *SQLStore) listActiveConfigsForConnections(accessIDs []string) []model.IssuedConfig {
	if len(accessIDs) == 0 {
		return []model.IssuedConfig{}
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(accessIDs)), ",")
	args := make([]any, 0, len(accessIDs)+1)
	for _, id := range accessIDs {
		args = append(args, id)
	}
	args = append(args, model.StatusActive)
	rows, err := s.query(`select id, connection_id, kind, slug, name, client, content_type, config, status, created_at, updated_at from issued_configs where connection_id in (`+placeholders+`) and status = ? order by name`, args...)
	if err != nil {
		return []model.IssuedConfig{}
	}
	defer rows.Close()
	out := []model.IssuedConfig{}
	for rows.Next() {
		cfg, err := scanConfig(rows)
		if err == nil {
			out = append(out, cfg)
		}
	}
	return out
}

func (s *SQLStore) listActiveProfilesForProtocols(protocols map[model.Protocol]bool) []model.ConfigProfile {
	if len(protocols) == 0 {
		return []model.ConfigProfile{}
	}
	placeholders := []string{}
	args := []any{}
	for protocol := range protocols {
		placeholders = append(placeholders, "?")
		args = append(args, protocol)
	}
	args = append(args, model.StatusActive)
	rows, err := s.query(`select id, protocol, kind, slug, name, client, content_type, config_template, status, created_at, updated_at from config_profiles where protocol in (`+strings.Join(placeholders, ",")+`) and status = ? order by slug`, args...)
	if err != nil {
		return []model.ConfigProfile{}
	}
	defer rows.Close()
	out := []model.ConfigProfile{}
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err == nil {
			out = append(out, profile)
		}
	}
	return out
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (model.Account, error) {
	var u record.Account
	var created, updated string
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Status, &u.Note, &created, &updated)
	u.CreatedAt = parseTS(created)
	u.UpdatedAt = parseTS(updated)
	return accountModel(u), convertErr(err)
}

func scanNode(row scanner) (model.Node, error) {
	var n record.Node
	var created, updated string
	var useIPv6 int
	err := row.Scan(&n.ID, &n.Name, &n.Type, &n.BaseURL, &n.APIKey, &n.Region, &n.SSHHost, &n.SSHPort, &n.SSHUser, &n.SSHKeyPath, &useIPv6, &n.Status, &created, &updated)
	n.UseIPv6 = useIPv6 != 0
	n.CreatedAt = parseTS(created)
	n.UpdatedAt = parseTS(updated)
	return nodeModel(n), convertErr(err)
}

func scanClient(row scanner) (model.Client, error) {
	var d record.Client
	var created, updated string
	err := row.Scan(&d.ID, &d.AccountID, &d.Slug, &d.Name, &d.Status, &created, &updated)
	d.CreatedAt = parseTS(created)
	d.UpdatedAt = parseTS(updated)
	return clientModel(d), convertErr(err)
}

func scanConnection(row scanner) (model.Connection, error) {
	var a record.Connection
	var lastSync sql.NullString
	var created, updated string
	err := row.Scan(&a.ID, &a.AccountID, &a.ClientID, &a.NodeID, &a.Protocol, &a.RemoteID, &a.RemoteName, &a.Status, &a.DesiredStatus, &lastSync, &a.LastError, &created, &updated)
	a.LastSyncAt = parseNullTS(lastSync)
	a.CreatedAt = parseTS(created)
	a.UpdatedAt = parseTS(updated)
	return connectionModel(a), convertErr(err)
}

func scanConnectionNode(row scanner) (model.Connection, model.Node, error) {
	var a record.Connection
	var n record.Node
	var lastSync sql.NullString
	var accessCreated, accessUpdated string
	var nodeCreated, nodeUpdated string
	var useIPv6 int
	err := row.Scan(
		&a.ID, &a.AccountID, &a.ClientID, &a.NodeID, &a.Protocol, &a.RemoteID, &a.RemoteName, &a.Status, &a.DesiredStatus, &lastSync, &a.LastError, &accessCreated, &accessUpdated,
		&n.ID, &n.Name, &n.Type, &n.BaseURL, &n.APIKey, &n.Region, &n.SSHHost, &n.SSHPort, &n.SSHUser, &n.SSHKeyPath, &useIPv6, &n.Status, &nodeCreated, &nodeUpdated,
	)
	a.LastSyncAt = parseNullTS(lastSync)
	a.CreatedAt = parseTS(accessCreated)
	a.UpdatedAt = parseTS(accessUpdated)
	n.UseIPv6 = useIPv6 != 0
	n.CreatedAt = parseTS(nodeCreated)
	n.UpdatedAt = parseTS(nodeUpdated)
	if err != nil {
		return model.Connection{}, model.Node{}, convertErr(err)
	}
	return connectionModel(a), nodeModel(n), nil
}

func scanConfig(row scanner) (model.IssuedConfig, error) {
	var c record.IssuedConfig
	var created, updated string
	err := row.Scan(&c.ID, &c.ConnectionID, &c.Kind, &c.Slug, &c.Name, &c.Client, &c.ContentType, &c.Config, &c.Status, &created, &updated)
	c.CreatedAt = parseTS(created)
	c.UpdatedAt = parseTS(updated)
	return issuedConfigModel(c), convertErr(err)
}

func scanProfile(row scanner) (model.ConfigProfile, error) {
	var p record.ConfigProfile
	var created, updated string
	err := row.Scan(&p.ID, &p.Protocol, &p.Kind, &p.Slug, &p.Name, &p.Client, &p.ContentType, &p.ConfigTemplate, &p.Status, &created, &updated)
	p.CreatedAt = parseTS(created)
	p.UpdatedAt = parseTS(updated)
	return configProfileModel(p), convertErr(err)
}

func scanToken(row scanner) (model.AccessToken, error) {
	var t record.AccessToken
	var expires, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&t.ID, &t.AccountID, &t.ClientID, &t.Token, &t.TokenHash, &t.Purpose, &t.Status, &expires, &lastUsed, &created, &updated)
	t.ExpiresAt = parseNullTS(expires)
	t.LastUsedAt = parseNullTS(lastUsed)
	t.CreatedAt = parseTS(created)
	t.UpdatedAt = parseTS(updated)
	return accessTokenModel(t), convertErr(err)
}

func scanAdminSession(row scanner) (model.AdminSession, error) {
	var session record.AdminSession
	var expires, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&session.ID, &session.Token, &session.TokenHash, &session.CSRFToken, &session.CSRFTokenHash, &session.PrincipalName, &session.Role, &session.Status, &expires, &lastUsed, &created, &updated)
	session.ExpiresAt = parseNullTS(expires)
	session.LastUsedAt = parseNullTS(lastUsed)
	session.CreatedAt = parseTS(created)
	session.UpdatedAt = parseTS(updated)
	return adminSessionModel(session), convertErr(err)
}

func scanClientCredential(row scanner) (model.ClientCredential, error) {
	var credential record.ClientCredential
	var lockedUntil sql.NullString
	var created, updated string
	err := row.Scan(&credential.AccountID, &credential.PINHash, &credential.RecoveryCodeHash, &credential.FailedAttempts, &lockedUntil, &created, &updated)
	credential.LockedUntil = parseNullTS(lockedUntil)
	credential.CreatedAt = parseTS(created)
	credential.UpdatedAt = parseTS(updated)
	return clientCredentialModel(credential), convertErr(err)
}

func scanClientSession(row scanner) (model.ClientSession, error) {
	var session record.ClientSession
	var expires, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&session.ID, &session.AccountID, &session.Token, &session.TokenHash, &session.Status, &expires, &lastUsed, &created, &updated)
	session.ExpiresAt = parseNullTS(expires)
	session.LastUsedAt = parseNullTS(lastUsed)
	session.CreatedAt = parseTS(created)
	session.UpdatedAt = parseTS(updated)
	return clientSessionModel(session), convertErr(err)
}

func scanPolicyTag(row scanner) (model.PolicyTag, error) {
	var tag record.PolicyTag
	var allowed string
	var created, updated string
	err := row.Scan(&tag.ID, &tag.Slug, &tag.Name, &allowed, &tag.ClientLimit, &tag.Status, &created, &updated)
	_ = json.Unmarshal([]byte(allowed), &tag.AllowedNodeIDs)
	tag.CreatedAt = parseTS(created)
	tag.UpdatedAt = parseTS(updated)
	return policyTagModel(tag), convertErr(err)
}

func scanAccountPolicyTag(row scanner) (model.AccountPolicyTag, error) {
	var userTag record.AccountPolicyTag
	var created string
	err := row.Scan(&userTag.ID, &userTag.AccountID, &userTag.TagID, &created)
	userTag.CreatedAt = parseTS(created)
	return accountPolicyTagModel(userTag), convertErr(err)
}

func scanShortLink(row scanner) (model.ShortLink, error) {
	var link record.ShortLink
	var created, updated string
	err := row.Scan(&link.ID, &link.TokenID, &link.Profile, &link.TargetURL, &link.EncryptedURL, &link.Status, &created, &updated)
	link.CreatedAt = parseTS(created)
	link.UpdatedAt = parseTS(updated)
	return shortLinkModel(link), convertErr(err)
}

func scanAuditLog(row scanner) (model.AuditLog, error) {
	var log record.AuditLog
	var created string
	err := row.Scan(&log.ID, &log.Actor, &log.Action, &log.EntityType, &log.EntityID, &log.Details, &created)
	log.CreatedAt = parseTS(created)
	return auditLogModel(log), convertErr(err)
}

func convertErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func ts(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func tsOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return ts(t)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func parseTS(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}

func parseNullTS(value sql.NullString) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return parseTS(value.String)
}
