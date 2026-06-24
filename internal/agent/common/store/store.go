package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"laz/internal/persistence"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"laz/internal/persistence/sqlutil"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type DB struct {
	rebind  func(string) string
	db      *sql.DB
	dialect persistence.Dialect
}

type AuthUser struct {
	ConnectionID string          `json:"connection_id"`
	NativeAuthID string          `json:"native_auth_id"`
	NativeMeta   json.RawMessage `json:"native_meta"`
}

type UsageRecord struct {
	ConnectionID string `json:"connection_id"`
	TXBytes      int64  `json:"tx_bytes"`
	RXBytes      int64  `json:"rx_bytes"`
}

type UsageBatch struct {
	RequestID string        `json:"request_id"`
	NodeID    string        `json:"node_id"`
	FromMS    int64         `json:"from_ms"`
	ToMS      int64         `json:"to_ms"`
	Records   []UsageRecord `json:"records"`
	CreatedMS int64         `json:"created_at_ms,omitempty"`
}

type TrafficSnapshot struct {
	CollectedMS int64
	Records     []UsageRecord
}

type AcknowledgedUsage struct {
	ConnectionID string
	RXBytes      int64
	TXBytes      int64
}

type ConnectionTrafficState struct {
	ConnectionID          string
	ObservedRXBytes       int64
	ObservedTXBytes       int64
	AcknowledgedRXBytes   int64
	AcknowledgedTXBytes   int64
	UnacknowledgedRXBytes int64
	UnacknowledgedTXBytes int64
	SentRXBytes           int64
	SentTXBytes           int64
	RequestID             string
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
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`pragma journal_mode = wal; pragma busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return openSQL(db, persistence.DialectSQLite)
}

func openSQL(db *sql.DB, dialect persistence.Dialect) (*DB, error) {
	rebind, err := sqlutil.NewRebinder(dialect)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	st := &DB{db: db, dialect: dialect, rebind: rebind}
	if err := applyMigrations(db, dialect); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *DB) Close() error {
	return s.db.Close()
}

func (s *DB) UpsertAuthUser(ctx context.Context, user AuthUser) error {
	_, err := s.exec(ctx, `insert into auth_users(connection_id, native_auth_id, native_meta)
values(?, ?, ?)
on conflict(connection_id) do update set native_auth_id = excluded.native_auth_id, native_meta = excluded.native_meta`,
		user.ConnectionID, user.NativeAuthID, string(user.NativeMeta))
	return err
}

func (s *DB) DeleteAuthUser(ctx context.Context, connectionID string) error {
	_, err := s.exec(ctx, `delete from auth_users where connection_id = ?`, connectionID)
	return err
}

func (s *DB) GetAuthUserByConnectionID(ctx context.Context, connectionID string) (AuthUser, error) {
	row := s.queryRow(ctx, `select connection_id, native_auth_id, native_meta from auth_users where connection_id = ?`, connectionID)
	return scanAuthUser(row)
}

func (s *DB) GetAuthUserByNativeAuthID(ctx context.Context, nativeID string) (AuthUser, error) {
	return scanAuthUser(s.queryRow(ctx, `select connection_id, native_auth_id, native_meta from auth_users where native_auth_id = ?`, nativeID))
}

func (s *DB) GetConnectionIDByNativeAuthID(ctx context.Context, nativeID string) (string, error) {
	var id string
	err := s.queryRow(ctx, `select connection_id from auth_users where native_auth_id = ?`, nativeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func (s *DB) ListAuthUsers(ctx context.Context) ([]AuthUser, error) {
	rows, err := s.db.QueryContext(ctx, `select connection_id, native_auth_id, native_meta from auth_users order by connection_id`)
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

func (s *DB) ApplyFullAuthSnapshot(ctx context.Context, keep map[string]AuthUser, cursorKey string, cursorMS int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `delete from auth_users`); err != nil {
		return err
	}
	for _, user := range keep {
		if _, err := tx.ExecContext(ctx, s.bind(`insert into auth_users(connection_id, native_auth_id, native_meta) values(?, ?, ?)`),
			user.ConnectionID, user.NativeAuthID, string(user.NativeMeta)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.bind(`insert into sync_state(key, value) values(?, ?) on conflict(key) do update set value = excluded.value`), cursorKey, strconv.FormatInt(cursorMS, 10)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *DB) GetState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.queryRow(ctx, `select value from sync_state where key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *DB) SetState(ctx context.Context, key, value string) error {
	_, err := s.exec(ctx, `insert into sync_state(key, value) values(?, ?) on conflict(key) do update set value = excluded.value`, key, value)
	return err
}

func (s *DB) ObserveTraffic(ctx context.Context, snapshot TrafficSnapshot) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range snapshot.Records {
		if record.ConnectionID == "" || record.RXBytes < 0 || record.TXBytes < 0 {
			return errors.New("traffic snapshot contains invalid values")
		}
		if _, err := tx.ExecContext(ctx, s.bind(`insert into connection_traffic_state(connection_id) values(?) on conflict(connection_id) do nothing`), record.ConnectionID); err != nil {
			return err
		}
		var observedRX, observedTX, unacknowledgedRX, unacknowledgedTX int64
		if err := tx.QueryRowContext(ctx, s.bind(`select observed_rx_bytes, observed_tx_bytes, unacknowledged_rx_bytes, unacknowledged_tx_bytes from connection_traffic_state where connection_id = ?`), record.ConnectionID).
			Scan(&observedRX, &observedTX, &unacknowledgedRX, &unacknowledgedTX); err != nil {
			return err
		}
		rxDelta := cumulativeDelta(record.RXBytes, observedRX)
		txDelta := cumulativeDelta(record.TXBytes, observedTX)
		if _, err := addNonnegative(unacknowledgedRX, rxDelta); err != nil {
			return err
		}
		if _, err := addNonnegative(unacknowledgedTX, txDelta); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.bind(`update connection_traffic_state set observed_rx_bytes = ?, observed_tx_bytes = ?, unacknowledged_rx_bytes = unacknowledged_rx_bytes + ?, unacknowledged_tx_bytes = unacknowledged_tx_bytes + ? where connection_id = ?`), record.RXBytes, record.TXBytes, rxDelta, txDelta, record.ConnectionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *DB) PrepareUsageRequest(ctx context.Context, requestID string, nodeID string, limit int) (UsageBatch, bool, error) {
	if requestID == "" {
		return UsageBatch{}, false, errors.New("traffic request_id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageBatch{}, false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, s.bind(`select connection_id, unacknowledged_tx_bytes, unacknowledged_rx_bytes from connection_traffic_state where request_id is null and (unacknowledged_rx_bytes > 0 or unacknowledged_tx_bytes > 0) order by connection_id limit ?`), limit)
	if err != nil {
		return UsageBatch{}, false, err
	}
	batch := UsageBatch{RequestID: requestID, NodeID: nodeID}
	for rows.Next() {
		var record UsageRecord
		if err := rows.Scan(&record.ConnectionID, &record.TXBytes, &record.RXBytes); err != nil {
			rows.Close()
			return UsageBatch{}, false, err
		}
		batch.Records = append(batch.Records, record)
	}
	if err := rows.Close(); err != nil {
		return UsageBatch{}, false, err
	}
	if len(batch.Records) == 0 {
		return UsageBatch{}, false, nil
	}
	for _, record := range batch.Records {
		result, err := tx.ExecContext(ctx, s.bind(`update connection_traffic_state set sent_rx_bytes = unacknowledged_rx_bytes, sent_tx_bytes = unacknowledged_tx_bytes, request_id = ? where connection_id = ? and request_id is null`), requestID, record.ConnectionID)
		if err != nil {
			return UsageBatch{}, false, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return UsageBatch{}, false, errors.New("traffic state changed while preparing request")
		}
	}
	return batch, true, tx.Commit()
}

func (s *DB) ListPreparedUsageRequests(ctx context.Context, nodeID string) ([]UsageBatch, error) {
	rows, err := s.query(ctx, `select request_id, connection_id, sent_tx_bytes, sent_rx_bytes from connection_traffic_state where request_id is not null order by request_id, connection_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]int{}
	var out []UsageBatch
	for rows.Next() {
		var requestID string
		var record UsageRecord
		if err := rows.Scan(&requestID, &record.ConnectionID, &record.TXBytes, &record.RXBytes); err != nil {
			return nil, err
		}
		index, ok := byID[requestID]
		if !ok {
			index = len(out)
			byID[requestID] = index
			out = append(out, UsageBatch{RequestID: requestID, NodeID: nodeID})
		}
		out[index].Records = append(out[index].Records, record)
	}
	return out, rows.Err()
}

func (s *DB) AcknowledgeUsage(ctx context.Context, requestID string, records []AcknowledgedUsage) error {
	ack := make(map[string]AcknowledgedUsage, len(records))
	for _, record := range records {
		if record.ConnectionID == "" || record.RXBytes < 0 || record.TXBytes < 0 {
			return errors.New("usage acknowledgement contains invalid values")
		}
		if _, duplicate := ack[record.ConnectionID]; duplicate {
			return fmt.Errorf("usage acknowledgement contains duplicate connection %s", record.ConnectionID)
		}
		ack[record.ConnectionID] = record
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, s.bind(`select connection_id, unacknowledged_rx_bytes, unacknowledged_tx_bytes, sent_rx_bytes, sent_tx_bytes from connection_traffic_state where request_id = ? order by connection_id`), requestID)
	if err != nil {
		return err
	}
	var states []ConnectionTrafficState
	for rows.Next() {
		var state ConnectionTrafficState
		if err := rows.Scan(&state.ConnectionID, &state.UnacknowledgedRXBytes, &state.UnacknowledgedTXBytes, &state.SentRXBytes, &state.SentTXBytes); err != nil {
			rows.Close()
			return err
		}
		states = append(states, state)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(states) == 0 {
		return tx.Commit()
	}
	if len(ack) != len(states) {
		return errors.New("usage acknowledgement does not match prepared connections")
	}
	for _, state := range states {
		accepted, ok := ack[state.ConnectionID]
		if !ok {
			return fmt.Errorf("usage acknowledgement misses connection %s", state.ConnectionID)
		}
		if state.SentRXBytes > state.UnacknowledgedRXBytes || state.SentTXBytes > state.UnacknowledgedTXBytes {
			return errors.New("invalid persisted traffic state")
		}
		if accepted.RXBytes < state.SentRXBytes || accepted.TXBytes < state.SentTXBytes {
			return fmt.Errorf("authoritative usage is lower than sent usage for connection %s", state.ConnectionID)
		}
		if _, err := tx.ExecContext(ctx, s.bind(`update connection_traffic_state set acknowledged_rx_bytes = ?, acknowledged_tx_bytes = ?, unacknowledged_rx_bytes = unacknowledged_rx_bytes - sent_rx_bytes, unacknowledged_tx_bytes = unacknowledged_tx_bytes - sent_tx_bytes, sent_rx_bytes = 0, sent_tx_bytes = 0, request_id = null where connection_id = ? and request_id = ?`), accepted.RXBytes, accepted.TXBytes, state.ConnectionID, requestID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *DB) GetConnectionTrafficState(ctx context.Context, connectionID string) (ConnectionTrafficState, error) {
	var state ConnectionTrafficState
	var requestID sql.NullString
	err := s.queryRow(ctx, `select connection_id, observed_rx_bytes, observed_tx_bytes, acknowledged_rx_bytes, acknowledged_tx_bytes, unacknowledged_rx_bytes, unacknowledged_tx_bytes, sent_rx_bytes, sent_tx_bytes, request_id from connection_traffic_state where connection_id = ?`, connectionID).
		Scan(&state.ConnectionID, &state.ObservedRXBytes, &state.ObservedTXBytes, &state.AcknowledgedRXBytes, &state.AcknowledgedTXBytes, &state.UnacknowledgedRXBytes, &state.UnacknowledgedTXBytes, &state.SentRXBytes, &state.SentTXBytes, &requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return ConnectionTrafficState{ConnectionID: connectionID}, nil
	}
	state.RequestID = requestID.String
	return state, err
}

func cumulativeDelta(current, observed int64) int64 {
	if current >= observed {
		return current - observed
	}
	// A lower cumulative value means the VPN runtime reset its counters. The
	// first post-reset observation is the start of a new runtime interval.
	return current
}

func addNonnegative(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errors.New("traffic counter overflow")
	}
	return a + b, nil
}

func scanAuthUser(scanner interface{ Scan(...any) error }) (AuthUser, error) {
	var user AuthUser
	var meta string
	err := scanner.Scan(&user.ConnectionID, &user.NativeAuthID, &meta)
	user.NativeMeta = json.RawMessage(meta)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthUser{}, ErrNotFound
	}
	return user, err
}

func (s *DB) bind(query string) string {
	return s.rebind(query)
}

func (s *DB) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.bind(query), args...)
}

func (s *DB) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.bind(query), args...)
}

func (s *DB) queryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.bind(query), args...)
}
