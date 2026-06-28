package transportdb

import (
	"context"
	"database/sql"
	"time"

	transportstore "laz/internal/nodeproto/transport"
	commonmigrations "laz/internal/persistence/migrations"
	"laz/internal/persistence/sqlutil"
	"laz/internal/server/model"
	"laz/internal/server/storage"
	"laz/internal/server/workqueue"
)

type CleanupPolicy struct {
	DeliveredTTL time.Duration
	ExpiredTTL   time.Duration
}

func DefaultCleanupPolicy() CleanupPolicy {
	return CleanupPolicy{
		DeliveredTTL: 15 * time.Minute,
		ExpiredTTL:   time.Hour,
	}
}

type Store struct {
	db      *sql.DB
	dialect commonmigrations.Dialect
	box     *transportstore.SecretBox
}

func New(transport *transportstore.SQLStore, secretKey string) (*Store, error) {
	st := &Store{db: transport.DB(), dialect: transport.Dialect()}
	box, err := transportstore.NewSecretBox(secretKey)
	if err != nil {
		return nil, err
	}
	st.box = box
	if err := applyMigrations(st.db, st.dialect); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) CreateEvent(event model.Event) (model.Event, error) {
	normalizeEvent(&event)
	_, err := s.exec(context.Background(), `insert into server_events(id, topic, status, type, entity_type, entity_id, actor, message, payload_json, created_at_ms, delivered_at_ms, expires_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Topic, event.Status, event.Type, event.EntityType, event.EntityID, event.Actor, event.Message, event.PayloadJSON, event.CreatedAtMS, nilMS(event.DeliveredAtMS), nilMS(event.ExpiresAtMS))
	return event, err
}

func (s *Store) ListPendingEvents(topic string, limit int) []model.Event {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(context.Background(), s.bind(`select id, topic, status, type, entity_type, entity_id, actor, message, payload_json, created_at_ms, delivered_at_ms, expires_at_ms
from server_events
where topic = ? and status = ?
order by created_at_ms, id limit ?`), topic, model.EventPending, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		item, err := scanEvent(rows)
		if err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (s *Store) MarkEventDelivered(id string, deliveredAtMS int64) error {
	if deliveredAtMS == 0 {
		deliveredAtMS = nowMS()
	}
	_, err := s.exec(context.Background(), `update server_events set status = ?, delivered_at_ms = ? where id = ?`, model.EventDelivered, deliveredAtMS, id)
	return err
}

func (s *Store) ExpireEvents(now int64) error {
	if now == 0 {
		now = nowMS()
	}
	_, err := s.exec(context.Background(), `update server_events set status = ? where status = ? and expires_at_ms is not null and expires_at_ms <= ?`, model.EventExpired, model.EventPending, now)
	return err
}

func (s *Store) Cleanup(ctx context.Context, policy CleanupPolicy) error {
	if policy.DeliveredTTL > 0 {
		if _, err := s.exec(ctx, `delete from server_events where status = ? and created_at_ms < ?`, model.EventDelivered, nowMS()-policy.DeliveredTTL.Milliseconds()); err != nil {
			return err
		}
	}
	if policy.ExpiredTTL > 0 {
		if _, err := s.exec(ctx, `delete from server_events where status = ? and created_at_ms < ?`, model.EventExpired, nowMS()-policy.ExpiredTTL.Milliseconds()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, msg transportstore.Message, result workqueue.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, next := range result.Next {
		if err := s.enqueueOutboxTx(ctx, tx, next); err != nil {
			return err
		}
	}
	for _, event := range result.Events {
		normalizeEvent(&event)
		if _, err := tx.ExecContext(ctx, s.bind(`insert into server_events(id, topic, status, type, entity_type, entity_id, actor, message, payload_json, created_at_ms, delivered_at_ms, expires_at_ms) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			event.ID, event.Topic, event.Status, event.Type, event.EntityType, event.EntityID, event.Actor, event.Message, event.PayloadJSON, event.CreatedAtMS, nilMS(event.DeliveredAtMS), nilMS(event.ExpiresAtMS)); err != nil {
			return err
		}
	}
	if err := s.completeMessageTx(ctx, tx, msg.ID, result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) enqueueOutboxTx(ctx context.Context, tx *sql.Tx, msg transportstore.Message) error {
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	if msg.AvailableAt.IsZero() {
		msg.AvailableAt = msg.CreatedAt
	}
	if msg.Status == "" {
		msg.Status = transportstore.StatusPending
	}
	msg.Payload = s.seal(msg.Payload)
	msg.ResultPayload = s.seal(msg.ResultPayload)
	input, err := transportstore.EncodeMessageJSON(msg.Payload, nil)
	if err != nil {
		return err
	}
	output, err := transportstore.EncodeMessageJSON(nil, msg.ResultPayload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, s.bind(`insert into transport_outbox_messages(actor_id, id, type, status, available_at_ms, expires_at_ms, created_at_ms, input_json, output_json, error)
values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
on conflict(actor_id, id) do update set type = excluded.type, status = excluded.status, available_at_ms = excluded.available_at_ms, expires_at_ms = excluded.expires_at_ms, input_json = excluded.input_json, output_json = excluded.output_json, error = excluded.error`),
		msg.ActorID, msg.ID, msg.Type, string(msg.Status), transportstore.TimeMS(msg.AvailableAt), transportstore.TimeMSOrNil(msg.ExpiresAt), transportstore.TimeMS(msg.CreatedAt), input, output, msg.Error)
	return err
}

func (s *Store) completeMessageTx(ctx context.Context, tx *sql.Tx, id string, result workqueue.Result) error {
	status := result.Status
	if status == "" {
		status = transportstore.StatusApplied
	}
	switch status {
	case transportstore.StatusApplied, transportstore.StatusAcked:
		output, err := transportstore.EncodeMessageJSON(nil, s.seal(result.Output))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, s.bind(`update transport_outbox_messages set status = ?, output_json = ?, error = ? where id = ?`), string(status), output, "", id)
		return err
	case transportstore.StatusExpired:
		_, err := tx.ExecContext(ctx, s.bind(`update transport_outbox_messages set status = ?, output_json = ?, error = ? where id = ?`), string(status), "{}", "", id)
		return err
	case transportstore.StatusFailed:
		now := time.Now().UTC()
		available := result.RetryAt
		if available.IsZero() {
			available = now
		}
		storedStatus := transportstore.StatusFailed
		if available.After(now) {
			storedStatus = transportstore.StatusPending
		}
		_, err := tx.ExecContext(ctx, s.bind(`update transport_outbox_messages set status = ?, available_at_ms = ?, error = ? where id = ?`), string(storedStatus), transportstore.TimeMS(available), string(result.Output), id)
		return err
	default:
		_, err := tx.ExecContext(ctx, s.bind(`update transport_outbox_messages set status = ?, available_at_ms = ?, error = ? where id = ?`), string(transportstore.StatusFailed), transportstore.TimeMS(time.Now().UTC()), "unsupported handler status", id)
		return err
	}
}

func (s *Store) seal(raw []byte) []byte {
	if s.box == nil {
		return raw
	}
	return s.box.Seal(raw)
}

func (s *Store) bind(query string) string {
	return sqlutil.Rebind(query, s.dialect)
}

func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.bind(query), args...)
}

func normalizeEvent(event *model.Event) {
	if event.ID == "" {
		event.ID = store.NewID("evt")
	}
	if event.Topic == "" {
		event.Topic = "admin"
	}
	if event.Status == "" {
		event.Status = model.EventPending
	}
	if event.PayloadJSON == "" {
		event.PayloadJSON = "{}"
	}
	if event.CreatedAtMS == 0 {
		event.CreatedAtMS = nowMS()
	}
	if event.ExpiresAtMS == 0 {
		event.ExpiresAtMS = time.Now().UTC().Add(10 * time.Minute).UnixMilli()
	}
}

func scanEvent(scanner interface{ Scan(...any) error }) (model.Event, error) {
	var event model.Event
	var status string
	var delivered, expires sql.NullInt64
	err := scanner.Scan(&event.ID, &event.Topic, &status, &event.Type, &event.EntityType, &event.EntityID, &event.Actor, &event.Message, &event.PayloadJSON, &event.CreatedAtMS, &delivered, &expires)
	event.Status = model.EventStatus(status)
	if delivered.Valid {
		event.DeliveredAtMS = delivered.Int64
	}
	if expires.Valid {
		event.ExpiresAtMS = expires.Int64
	}
	return event, err
}

func nilMS(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nowMS() int64 {
	return time.Now().UTC().UnixMilli()
}
