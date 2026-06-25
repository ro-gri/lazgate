package transport

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	st := &SQLiteStore{db: db}
	if err := st.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) migrate() error {
	_, err := s.db.Exec(`pragma journal_mode = wal;
pragma busy_timeout = 5000;
create table if not exists transport_messages (
  id text primary key,
  actor_id text not null,
  direction text not null,
  type text not null,
  payload blob not null,
  status text not null,
  attempts integer not null default 0,
  available_at text,
  expires_at text,
  created_at text not null,
  updated_at text not null,
  sent_at text,
  processed_at text,
  result_payload blob,
  error text not null default ''
);
create index if not exists idx_transport_messages_lease on transport_messages(actor_id, direction, status, available_at, created_at);
create index if not exists idx_transport_messages_cleanup on transport_messages(status, updated_at);
create table if not exists processed_messages (
  actor_id text not null,
  message_id text not null,
  type text not null,
  status text not null,
  result_payload blob,
  error text not null default '',
  processed_at text not null,
  primary key(actor_id, message_id)
);
create index if not exists idx_processed_messages_cleanup on processed_messages(processed_at);`)
	return err
}

func (s *SQLiteStore) Enqueue(ctx context.Context, msg Message) error {
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	if msg.UpdatedAt.IsZero() {
		msg.UpdatedAt = msg.CreatedAt
	}
	if msg.AvailableAt.IsZero() {
		msg.AvailableAt = msg.CreatedAt
	}
	if msg.Status == "" {
		msg.Status = StatusPending
	}
	if msg.Direction == "" {
		msg.Direction = DirectionOutbound
	}
	_, err := s.db.ExecContext(ctx, `insert into transport_messages(id, actor_id, direction, type, payload, status, attempts, available_at, expires_at, created_at, updated_at, sent_at, processed_at, result_payload, error)
values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(id) do update set actor_id = excluded.actor_id, direction = excluded.direction, type = excluded.type, payload = excluded.payload, status = excluded.status, available_at = excluded.available_at, expires_at = excluded.expires_at, updated_at = excluded.updated_at, error = ''`,
		msg.ID, msg.ActorID, string(msg.Direction), msg.Type, msg.Payload, string(msg.Status), msg.Attempts, tsOrNil(msg.AvailableAt), tsOrNil(msg.ExpiresAt), ts(msg.CreatedAt), ts(msg.UpdatedAt), tsOrNil(msg.SentAt), tsOrNil(msg.ProcessedAt), msg.ResultPayload, msg.Error)
	return err
}

func (s *SQLiteStore) LeasePending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseFor)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `select id, actor_id, direction, type, payload, status, attempts, available_at, expires_at, created_at, updated_at, sent_at, processed_at, result_payload, error
from transport_messages
where actor_id = ? and direction = ? and status = ? and (available_at is null or available_at <= ?) and (expires_at is null or expires_at > ?)
order by created_at limit ?`, actorID, string(DirectionOutbound), string(StatusPending), ts(now), ts(now), limit)
	if err != nil {
		return nil, err
	}
	var out []Message
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, msg)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, msg := range out {
		if _, err := tx.ExecContext(ctx, `update transport_messages set status = ?, attempts = attempts + 1, available_at = ?, sent_at = ?, updated_at = ? where id = ?`,
			string(StatusInFlight), ts(leaseUntil), ts(now), ts(now), msg.ID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

func (s *SQLiteStore) MarkSent(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `update transport_messages set sent_at = ?, updated_at = ? where id = ?`, ts(now), ts(now), id)
	return err
}

func (s *SQLiteStore) MarkApplied(ctx context.Context, id string, result []byte) error {
	return s.markDone(ctx, id, StatusApplied, result, "")
}

func (s *SQLiteStore) MarkAcked(ctx context.Context, id string, result []byte) error {
	return s.markDone(ctx, id, StatusAcked, result, "")
}

func (s *SQLiteStore) MarkFailed(ctx context.Context, id string, errMsg string, retryAt time.Time) error {
	now := time.Now().UTC()
	status := StatusFailed
	available := retryAt
	if !retryAt.IsZero() && retryAt.After(now) {
		status = StatusPending
	}
	_, err := s.db.ExecContext(ctx, `update transport_messages set status = ?, available_at = ?, error = ?, updated_at = ? where id = ?`,
		string(status), tsOrNil(available), errMsg, ts(now), id)
	return err
}

func (s *SQLiteStore) MarkExpired(ctx context.Context, id string, errMsg string) error {
	return s.markDone(ctx, id, StatusExpired, nil, errMsg)
}

func (s *SQLiteStore) markDone(ctx context.Context, id string, status Status, result []byte, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `update transport_messages set status = ?, result_payload = ?, error = ?, processed_at = ?, updated_at = ? where id = ?`,
		string(status), result, errMsg, ts(now), ts(now), id)
	return err
}

func (s *SQLiteStore) IsProcessed(ctx context.Context, actorID string, messageID string) (ProcessedMessage, bool, error) {
	row := s.db.QueryRowContext(ctx, `select actor_id, message_id, type, status, result_payload, error, processed_at from processed_messages where actor_id = ? and message_id = ?`, actorID, messageID)
	msg, err := scanProcessed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ProcessedMessage{}, false, nil
	}
	if err != nil {
		return ProcessedMessage{}, false, err
	}
	return msg, true, nil
}

func (s *SQLiteStore) RecordProcessed(ctx context.Context, actorID string, messageID string, typ string, status Status, result []byte, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `insert into processed_messages(actor_id, message_id, type, status, result_payload, error, processed_at)
values(?, ?, ?, ?, ?, ?, ?)
on conflict(actor_id, message_id) do update set status = excluded.status, result_payload = excluded.result_payload, error = excluded.error, processed_at = excluded.processed_at`,
		actorID, messageID, typ, string(status), result, errMsg, ts(now))
	return err
}

func (s *SQLiteStore) RequeueExpiredLeases(ctx context.Context, actorID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `update transport_messages set status = ?, updated_at = ? where actor_id = ? and direction = ? and status = ? and available_at <= ? and (expires_at is null or expires_at > ?)`,
		string(StatusPending), ts(now), actorID, string(DirectionOutbound), string(StatusInFlight), ts(now), ts(now))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `update transport_messages set status = ?, updated_at = ? where actor_id = ? and direction = ? and status in (?, ?) and expires_at is not null and expires_at <= ?`,
		string(StatusExpired), ts(now), actorID, string(DirectionOutbound), string(StatusPending), string(StatusInFlight), ts(now))
	return err
}

func (s *SQLiteStore) Cleanup(ctx context.Context, policy CleanupPolicy) error {
	now := time.Now().UTC()
	rules := []struct {
		status Status
		typ    string
		ttl    time.Duration
	}{
		{StatusAcked, "auth_refresh", policy.AuthAckedTTL},
		{StatusApplied, "auth_refresh", policy.AuthAckedTTL},
		{StatusSuperseded, "auth_refresh", policy.AuthSupersededTTL},
		{StatusFailed, "auth_refresh", policy.AuthFailedTTL},
		{StatusExpired, "auth_refresh", policy.AuthFailedTTL},
		{StatusAcked, "runtime_command", policy.RuntimeAckedTTL},
		{StatusApplied, "runtime_command", policy.RuntimeAckedTTL},
		{StatusFailed, "runtime_command", policy.RuntimeFailedTTL},
		{StatusExpired, "runtime_command", policy.RuntimeFailedTTL},
		{StatusAcked, "online_snapshot", policy.OnlineAckedTTL},
		{StatusFailed, "online_snapshot", policy.OnlineFailedTTL},
		{StatusExpired, "online_snapshot", policy.OnlineFailedTTL},
		{StatusAcked, "traffic_batch", policy.TrafficAckedTTL},
		{StatusApplied, "traffic_batch", policy.TrafficAckedTTL},
		{StatusFailed, "traffic_batch", policy.TrafficFailedTTL},
		{StatusExpired, "traffic_batch", policy.TrafficFailedTTL},
		{StatusAcked, "", policy.DefaultAckedTTL},
		{StatusApplied, "", policy.DefaultAckedTTL},
		{StatusFailed, "", policy.DefaultFailedTTL},
		{StatusExpired, "", policy.DefaultExpiredTTL},
	}
	for _, rule := range rules {
		if rule.ttl <= 0 {
			continue
		}
		cutoff := now.Add(-rule.ttl)
		var err error
		if rule.typ == "" {
			_, err = s.db.ExecContext(ctx, `delete from transport_messages where status = ? and updated_at < ?`, string(rule.status), ts(cutoff))
		} else {
			_, err = s.db.ExecContext(ctx, `delete from transport_messages where status = ? and type = ? and updated_at < ?`, string(rule.status), rule.typ, ts(cutoff))
		}
		if err != nil {
			return err
		}
	}
	if policy.ProcessedTTL > 0 {
		cutoff := now.Add(-policy.ProcessedTTL)
		if _, err := s.db.ExecContext(ctx, `delete from processed_messages where processed_at < ?`, ts(cutoff)); err != nil {
			return err
		}
	}
	if policy.PayloadRedactAfter > 0 {
		cutoff := now.Add(-policy.PayloadRedactAfter)
		if _, err := s.db.ExecContext(ctx, `update transport_messages set payload = x'', result_payload = null where status in (?, ?, ?) and updated_at < ?`,
			string(StatusFailed), string(StatusExpired), string(StatusAcked), ts(cutoff)); err != nil {
			return err
		}
	}
	return nil
}

func scanMessage(scanner interface{ Scan(...any) error }) (Message, error) {
	var msg Message
	var direction, status string
	var availableAt, expiresAt, createdAt, updatedAt, sentAt, processedAt sql.NullString
	err := scanner.Scan(&msg.ID, &msg.ActorID, &direction, &msg.Type, &msg.Payload, &status, &msg.Attempts, &availableAt, &expiresAt, &createdAt, &updatedAt, &sentAt, &processedAt, &msg.ResultPayload, &msg.Error)
	msg.Direction = Direction(direction)
	msg.Status = Status(status)
	msg.AvailableAt = parseNullTime(availableAt)
	msg.ExpiresAt = parseNullTime(expiresAt)
	msg.CreatedAt = parseNullTime(createdAt)
	msg.UpdatedAt = parseNullTime(updatedAt)
	msg.SentAt = parseNullTime(sentAt)
	msg.ProcessedAt = parseNullTime(processedAt)
	return msg, err
}

func scanProcessed(scanner interface{ Scan(...any) error }) (ProcessedMessage, error) {
	var msg ProcessedMessage
	var status string
	var processedAt string
	err := scanner.Scan(&msg.ActorID, &msg.MessageID, &msg.Type, &status, &msg.Result, &msg.Error, &processedAt)
	msg.Status = Status(status)
	msg.ProcessedAt = parseTime(processedAt)
	return msg, err
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

func parseNullTime(value sql.NullString) time.Time {
	if !value.Valid || value.String == "" {
		return time.Time{}
	}
	return parseTime(value.String)
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
