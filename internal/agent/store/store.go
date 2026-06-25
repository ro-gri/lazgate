package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type AuthUser struct {
	UserID                    string `json:"user_id"`
	CredentialID              string `json:"credential_id"`
	Username                  string `json:"username"`
	PasswordHash              string `json:"password_hash"`
	ExpiresAtMS               int64  `json:"expires_at_ms,omitempty"`
	QuotaLimitBytes           int64  `json:"quota_limit_bytes,omitempty"`
	LastKnownGlobalUsageBytes int64  `json:"last_known_global_usage_bytes,omitempty"`
	QuotaGuardOverageBytes    int64  `json:"quota_guard_overage_bytes,omitempty"`
	PayloadJSON               string `json:"payload_json,omitempty"`
}

type UsageRecord struct {
	CredentialID string `json:"credential_id"`
	TXBytes      int64  `json:"tx_bytes"`
	RXBytes      int64  `json:"rx_bytes"`
}

type UsageBatch struct {
	BatchID   string        `json:"batch_id"`
	NodeID    string        `json:"node_id"`
	FromMS    int64         `json:"from_ms"`
	ToMS      int64         `json:"to_ms"`
	Records   []UsageRecord `json:"records"`
	CreatedMS int64         `json:"created_at_ms,omitempty"`
}

var ErrNotFound = errors.New("not found")

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	st := &DB{db: db}
	if err := st.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *DB) Close() error {
	return s.db.Close()
}

func (s *DB) migrate() error {
	_, err := s.db.Exec(`pragma journal_mode = wal;
create table if not exists auth_users (
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
create table if not exists sync_state (
  key text primary key,
  value text not null
);
create table if not exists pending_usage_batches (
  batch_id text primary key,
  payload_json text not null,
  created_at_ms integer not null
);`)
	return err
}

func (s *DB) UpsertAuthUser(ctx context.Context, user AuthUser) error {
	_, err := s.db.ExecContext(ctx, `insert into auth_users(user_id, credential_id, username, password_hash, expires_at_ms, quota_limit_bytes, last_known_global_usage_bytes, quota_guard_overage_bytes, payload_json)
values(?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(user_id) do update set credential_id = excluded.credential_id, username = excluded.username, password_hash = excluded.password_hash, expires_at_ms = excluded.expires_at_ms, quota_limit_bytes = excluded.quota_limit_bytes, last_known_global_usage_bytes = excluded.last_known_global_usage_bytes, quota_guard_overage_bytes = excluded.quota_guard_overage_bytes, payload_json = excluded.payload_json`,
		user.UserID, user.CredentialID, user.Username, user.PasswordHash, nullableInt(user.ExpiresAtMS), nullableInt(user.QuotaLimitBytes), nullableInt(user.LastKnownGlobalUsageBytes), nullableInt(user.QuotaGuardOverageBytes), user.PayloadJSON)
	return err
}

func (s *DB) DeleteAuthUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `delete from auth_users where user_id = ?`, userID)
	return err
}

func (s *DB) GetAuthUserByUsername(ctx context.Context, username string) (AuthUser, error) {
	row := s.db.QueryRowContext(ctx, `select user_id, credential_id, username, password_hash, coalesce(expires_at_ms, 0), coalesce(quota_limit_bytes, 0), coalesce(last_known_global_usage_bytes, 0), coalesce(quota_guard_overage_bytes, 0), payload_json from auth_users where username = ?`, username)
	return scanAuthUser(row)
}

func (s *DB) ListAuthUsers(ctx context.Context) ([]AuthUser, error) {
	rows, err := s.db.QueryContext(ctx, `select user_id, credential_id, username, password_hash, coalesce(expires_at_ms, 0), coalesce(quota_limit_bytes, 0), coalesce(last_known_global_usage_bytes, 0), coalesce(quota_guard_overage_bytes, 0), payload_json from auth_users order by username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthUser
	for rows.Next() {
		user, err := scanAuthUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (s *DB) ApplyFullAuthSnapshot(ctx context.Context, keep map[string]AuthUser, cursorMS int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `delete from auth_users`); err != nil {
		return err
	}
	for _, user := range keep {
		if _, err := tx.ExecContext(ctx, `insert into auth_users(user_id, credential_id, username, password_hash, expires_at_ms, quota_limit_bytes, last_known_global_usage_bytes, quota_guard_overage_bytes, payload_json) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			user.UserID, user.CredentialID, user.Username, user.PasswordHash, nullableInt(user.ExpiresAtMS), nullableInt(user.QuotaLimitBytes), nullableInt(user.LastKnownGlobalUsageBytes), nullableInt(user.QuotaGuardOverageBytes), user.PayloadJSON); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `insert into sync_state(key, value) values('last_applied_auth_manifest_started_at', ?) on conflict(key) do update set value = excluded.value`, int64String(cursorMS)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DB) GetState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `select value from sync_state where key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *DB) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `insert into sync_state(key, value) values(?, ?) on conflict(key) do update set value = excluded.value`, key, value)
	return err
}

func (s *DB) SaveUsageBatch(ctx context.Context, batch UsageBatch) error {
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	if batch.CreatedMS == 0 {
		batch.CreatedMS = time.Now().UnixMilli()
	}
	_, err = s.db.ExecContext(ctx, `insert or ignore into pending_usage_batches(batch_id, payload_json, created_at_ms) values(?, ?, ?)`, batch.BatchID, string(raw), batch.CreatedMS)
	return err
}

func (s *DB) ListUsageBatches(ctx context.Context, limit int) ([]UsageBatch, error) {
	rows, err := s.db.QueryContext(ctx, `select payload_json from pending_usage_batches order by created_at_ms limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageBatch
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var batch UsageBatch
		if err := json.Unmarshal([]byte(raw), &batch); err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

func (s *DB) UsageQueueStats(ctx context.Context) (int, int64, error) {
	rows, err := s.db.QueryContext(ctx, `select payload_json from pending_usage_batches`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var count int
	var bytes int64
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, 0, err
		}
		count++
		bytes += int64(len(raw))
	}
	return count, bytes, rows.Err()
}

func (s *DB) DeleteUsageBatch(ctx context.Context, batchID string) error {
	_, err := s.db.ExecContext(ctx, `delete from pending_usage_batches where batch_id = ?`, batchID)
	return err
}

func (s *DB) PendingUsageForCredential(ctx context.Context, credentialID string) (int64, error) {
	batches, err := s.ListUsageBatches(ctx, 10000)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, batch := range batches {
		for _, rec := range batch.Records {
			if rec.CredentialID == credentialID {
				total += rec.TXBytes + rec.RXBytes
			}
		}
	}
	return total, nil
}

func scanAuthUser(scanner interface{ Scan(...any) error }) (AuthUser, error) {
	var user AuthUser
	err := scanner.Scan(&user.UserID, &user.CredentialID, &user.Username, &user.PasswordHash, &user.ExpiresAtMS, &user.QuotaLimitBytes, &user.LastKnownGlobalUsageBytes, &user.QuotaGuardOverageBytes, &user.PayloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return user, err
}

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func int64String(v int64) string {
	return strconvFormatInt(v)
}

func strconvFormatInt(v int64) string {
	return fmt.Sprintf("%d", v)
}
