package events

import (
	"context"
	"sync"
	"time"

	"laz/internal/server/model"
)

type Store interface {
	CreateEvent(model.Event) (model.Event, error)
	ListEventsAfter(afterMS int64, limit int) []model.Event
}

type Bus struct {
	store Store

	mu          sync.Mutex
	subscribers map[chan model.Event]struct{}
}

func New(store Store) *Bus {
	return &Bus{
		store:       store,
		subscribers: map[chan model.Event]struct{}{},
	}
}

func (b *Bus) Publish(ctx context.Context, event model.Event) model.Event {
	if event.CreatedAtMS == 0 {
		event.CreatedAtMS = time.Now().UTC().UnixMilli()
	}
	if event.PayloadJSON == "" {
		event.PayloadJSON = "{}"
	}
	if b.store != nil {
		if saved, err := b.store.CreateEvent(event); err == nil {
			event = saved
		}
	}
	b.broadcast(event)
	return event
}

func (b *Bus) ListAfter(afterMS int64, limit int) []model.Event {
	if b.store == nil {
		return nil
	}
	return b.store.ListEventsAfter(afterMS, limit)
}

func (b *Bus) Subscribe(ctx context.Context) <-chan model.Event {
	ch := make(chan model.Event, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subscribers, ch)
		close(ch)
		b.mu.Unlock()
	}()
	return ch
}

func (b *Bus) broadcast(event model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
