package events

import (
	"context"
	"time"

	"laz/internal/server/model"
)

const (
	AdminTopicPrefix  = "admin:"
	ClientTopicPrefix = "client:"
)

type Store interface {
	CreateEvent(model.Event) (model.Event, error)
	ListPendingEvents(topic string, limit int) []model.Event
	MarkEventDelivered(id string, deliveredAtMS int64) error
	ExpireEvents(nowMS int64) error
}

type Bus struct {
	store Store
}

func New(store Store) *Bus {
	return &Bus{store: store}
}

func AdminTopic(principal string) string {
	if principal == "" {
		principal = "admin"
	}
	return AdminTopicPrefix + principal
}

func ClientTopic(accountID string) string {
	return ClientTopicPrefix + accountID
}

func (b *Bus) Publish(ctx context.Context, event model.Event) model.Event {
	if b == nil || b.store == nil {
		return event
	}
	if event.Status == "" {
		event.Status = model.EventPending
	}
	if event.PayloadJSON == "" {
		event.PayloadJSON = "{}"
	}
	if event.CreatedAtMS == 0 {
		event.CreatedAtMS = time.Now().UTC().UnixMilli()
	}
	if event.ExpiresAtMS == 0 {
		event.ExpiresAtMS = time.Now().UTC().Add(10 * time.Minute).UnixMilli()
	}
	saved, err := b.store.CreateEvent(event)
	if err == nil {
		return saved
	}
	return event
}

func (b *Bus) ListPending(topic string, limit int) []model.Event {
	if b == nil || b.store == nil {
		return nil
	}
	_ = b.store.ExpireEvents(time.Now().UTC().UnixMilli())
	return b.store.ListPendingEvents(topic, limit)
}

func (b *Bus) MarkDelivered(id string) error {
	if b == nil || b.store == nil {
		return nil
	}
	return b.store.MarkEventDelivered(id, time.Now().UTC().UnixMilli())
}
