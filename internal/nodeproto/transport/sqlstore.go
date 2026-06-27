package transport

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	commonmigrations "laz/internal/persistence/migrations"
	"laz/internal/persistence/sqlutil"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db      *sql.DB
	dialect commonmigrations.Dialect
}

type SQLiteStore = SQLStore

type messageJSON struct {
	Payload []byte `json:"payload,omitempty"`
	Result  []byte `json:"result,omitempty"`
}

func OpenSQLite(path string) (*SQLStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`pragma journal_mode = wal; pragma busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return openSQL(db, commonmigrations.DialectSQLite)
}

func OpenPostgres(databaseURL string) (*SQLStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	return openSQL(db, commonmigrations.DialectPostgres)
}

func openSQL(db *sql.DB, dialect commonmigrations.Dialect) (*SQLStore, error) {
	st := &SQLStore{db: db, dialect: dialect}
	if err := applyMigrations(db, dialect); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) Enqueue(ctx context.Context, msg Message) error {
	msg.Direction = DirectionOutbound
	return s.enqueue(ctx, "transport_outbox_messages", msg)
}

func (s *SQLStore) EnqueueInbox(ctx context.Context, msg Message) error {
	msg.Direction = DirectionInbound
	return s.enqueue(ctx, "transport_inbox_messages", msg)
}

func (s *SQLStore) enqueue(ctx context.Context, table string, msg Message) error {
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	if msg.AvailableAt.IsZero() {
		msg.AvailableAt = msg.CreatedAt
	}
	if msg.Status == "" {
		msg.Status = StatusPending
	}
	input, err := encodeMessageJSON(messageJSON{Payload: msg.Payload})
	if err != nil {
		return err
	}
	output, err := encodeMessageJSON(messageJSON{Result: msg.ResultPayload})
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `insert into `+table+`(actor_id, id, type, status, available_at_ms, expires_at_ms, created_at_ms, input_json, output_json, error)
values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(actor_id, id) do update set type = excluded.type, status = excluded.status, available_at_ms = excluded.available_at_ms, expires_at_ms = excluded.expires_at_ms, input_json = excluded.input_json, output_json = excluded.output_json, error = excluded.error`,
		msg.ActorID, msg.ID, msg.Type, string(msg.Status), timeMS(msg.AvailableAt), timeMSOrNil(msg.ExpiresAt), timeMS(msg.CreatedAt), input, output, msg.Error)
	return err
}

func (s *SQLStore) LeasePending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	return s.leasePending(ctx, "transport_outbox_messages", DirectionOutbound, actorID, limit, leaseFor)
}

func (s *SQLStore) LeaseInboxPending(ctx context.Context, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	return s.leasePending(ctx, "transport_inbox_messages", DirectionInbound, actorID, limit, leaseFor)
}

func (s *SQLStore) leasePending(ctx context.Context, table string, direction Direction, actorID string, limit int, leaseFor time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC()
	nowMS := now.UnixMilli()
	leaseUntilMS := now.Add(leaseFor).UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, s.bind(`select actor_id, id, type, status, available_at_ms, expires_at_ms, created_at_ms, input_json, output_json, error
from `+table+`
where actor_id = ? and status = ? and available_at_ms <= ? and (expires_at_ms is null or expires_at_ms > ?)
order by available_at_ms, id limit ?`), actorID, string(StatusPending), nowMS, nowMS, limit)
	if err != nil {
		return nil, err
	}
	var out []Message
	for rows.Next() {
		msg, err := scanMessage(rows, direction)
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
		if _, err := tx.ExecContext(ctx, s.bind(`update `+table+` set status = ?, available_at_ms = ? where actor_id = ? and id = ?`),
			string(StatusInFlight), leaseUntilMS, msg.ActorID, msg.ID); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

func (s *SQLStore) MarkApplied(ctx context.Context, id string, result []byte) error {
	return s.markDone(ctx, "transport_outbox_messages", id, StatusApplied, result, "")
}

func (s *SQLStore) MarkAcked(ctx context.Context, id string, result []byte) error {
	return s.markDone(ctx, "transport_outbox_messages", id, StatusAcked, result, "")
}

func (s *SQLStore) MarkFailed(ctx context.Context, id string, errMsg string, retryAt time.Time) error {
	now := time.Now().UTC()
	status := StatusFailed
	available := retryAt
	if !retryAt.IsZero() && retryAt.After(now) {
		status = StatusPending
	}
	if available.IsZero() {
		available = now
	}
	_, err := s.exec(ctx, `update transport_outbox_messages set status = ?, available_at_ms = ?, error = ? where id = ?`,
		string(status), timeMS(available), errMsg, id)
	return err
}

func (s *SQLStore) MarkExpired(ctx context.Context, id string, errMsg string) error {
	return s.markDone(ctx, "transport_outbox_messages", id, StatusExpired, nil, errMsg)
}

func (s *SQLStore) markDone(ctx context.Context, table string, id string, status Status, result []byte, errMsg string) error {
	output, err := encodeMessageJSON(messageJSON{Result: result})
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `update `+table+` set status = ?, output_json = ?, error = ? where id = ?`,
		string(status), output, errMsg, id)
	return err
}

func (s *SQLStore) IsProcessed(ctx context.Context, actorID string, messageID string) (ProcessedMessage, bool, error) {
	row := s.queryRow(ctx, `select actor_id, id, type, status, output_json, error from transport_inbox_messages where actor_id = ? and id = ? and status in (?, ?, ?)`,
		actorID, messageID, string(StatusApplied), string(StatusAcked), string(StatusFailed))
	msg, err := scanProcessed(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ProcessedMessage{}, false, nil
	}
	if err != nil {
		return ProcessedMessage{}, false, err
	}
	return msg, true, nil
}

func (s *SQLStore) RecordProcessed(ctx context.Context, actorID string, messageID string, typ string, status Status, result []byte, errMsg string) error {
	now := time.Now().UTC()
	input, err := encodeMessageJSON(messageJSON{})
	if err != nil {
		return err
	}
	output, err := encodeMessageJSON(messageJSON{Result: result})
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `insert into transport_inbox_messages(actor_id, id, type, status, available_at_ms, expires_at_ms, created_at_ms, input_json, output_json, error)
values(?, ?, ?, ?, ?, null, ?, ?, ?, ?)
on conflict(actor_id, id) do update set status = excluded.status, output_json = excluded.output_json, error = excluded.error`,
		actorID, messageID, typ, string(status), now.UnixMilli(), now.UnixMilli(), input, output, errMsg)
	return err
}

func (s *SQLStore) RequeueExpiredLeases(ctx context.Context, actorID string) error {
	return s.requeueExpiredLeases(ctx, "transport_outbox_messages", actorID)
}

func (s *SQLStore) requeueExpiredLeases(ctx context.Context, table string, actorID string) error {
	nowMS := time.Now().UTC().UnixMilli()
	_, err := s.exec(ctx, `update `+table+` set status = ? where actor_id = ? and status = ? and available_at_ms <= ? and (expires_at_ms is null or expires_at_ms > ?)`,
		string(StatusPending), actorID, string(StatusInFlight), nowMS, nowMS)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `update `+table+` set status = ? where actor_id = ? and status in (?, ?) and expires_at_ms is not null and expires_at_ms <= ?`,
		string(StatusExpired), actorID, string(StatusPending), string(StatusInFlight), nowMS)
	return err
}

func (s *SQLStore) Cleanup(ctx context.Context, policy CleanupPolicy) error {
	for _, table := range []string{"transport_outbox_messages", "transport_inbox_messages"} {
		if err := s.cleanupTable(ctx, table, policy); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) cleanupTable(ctx context.Context, table string, policy CleanupPolicy) error {
	nowMS := time.Now().UTC().UnixMilli()
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
		cutoff := nowMS - rule.ttl.Milliseconds()
		var err error
		if rule.typ == "" {
			_, err = s.exec(ctx, `delete from `+table+` where status = ? and created_at_ms < ?`, string(rule.status), cutoff)
		} else {
			_, err = s.exec(ctx, `delete from `+table+` where status = ? and type = ? and created_at_ms < ?`, string(rule.status), rule.typ, cutoff)
		}
		if err != nil {
			return err
		}
	}
	if policy.PayloadRedactAfter > 0 {
		cutoff := nowMS - policy.PayloadRedactAfter.Milliseconds()
		if _, err := s.exec(ctx, `update `+table+` set input_json = '{}', output_json = '{}' where status in (?, ?, ?) and created_at_ms < ?`,
			string(StatusFailed), string(StatusExpired), string(StatusAcked), cutoff); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) bind(query string) string {
	return sqlutil.Rebind(query, s.dialect)
}

func (s *SQLStore) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.bind(query), args...)
}

func (s *SQLStore) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.bind(query), args...)
}

func scanMessage(scanner interface{ Scan(...any) error }, direction Direction) (Message, error) {
	var msg Message
	var status string
	var availableMS, createdMS int64
	var expiresMS sql.NullInt64
	var inputJSON, outputJSON string
	err := scanner.Scan(&msg.ActorID, &msg.ID, &msg.Type, &status, &availableMS, &expiresMS, &createdMS, &inputJSON, &outputJSON, &msg.Error)
	msg.Direction = direction
	msg.Status = Status(status)
	msg.AvailableAt = timeFromMS(availableMS)
	msg.ExpiresAt = timeFromNullMS(expiresMS)
	msg.CreatedAt = timeFromMS(createdMS)
	msg.Payload = mustDecodeMessageJSON(inputJSON).Payload
	msg.ResultPayload = mustDecodeMessageJSON(outputJSON).Result
	return msg, err
}

func scanProcessed(scanner interface{ Scan(...any) error }) (ProcessedMessage, error) {
	var msg ProcessedMessage
	var status string
	var outputJSON string
	err := scanner.Scan(&msg.ActorID, &msg.MessageID, &msg.Type, &status, &outputJSON, &msg.Error)
	msg.Status = Status(status)
	msg.Result = mustDecodeMessageJSON(outputJSON).Result
	return msg, err
}

func encodeMessageJSON(value messageJSON) (string, error) {
	m := map[string]string{}
	if len(value.Payload) > 0 {
		m["payload_b64"] = base64.StdEncoding.EncodeToString(value.Payload)
	}
	if len(value.Result) > 0 {
		m["result_b64"] = base64.StdEncoding.EncodeToString(value.Result)
	}
	if len(m) == 0 {
		return "{}", nil
	}
	raw, err := json.Marshal(m)
	return string(raw), err
}

func mustDecodeMessageJSON(raw string) messageJSON {
	var m map[string]string
	_ = json.Unmarshal([]byte(raw), &m)
	var out messageJSON
	if encoded := m["payload_b64"]; encoded != "" {
		out.Payload, _ = base64.StdEncoding.DecodeString(encoded)
	}
	if encoded := m["result_b64"]; encoded != "" {
		out.Result, _ = base64.StdEncoding.DecodeString(encoded)
	}
	return out
}

func timeMS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func timeMSOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return timeMS(t)
}

func timeFromMS(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func timeFromNullMS(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return timeFromMS(ms.Int64)
}
