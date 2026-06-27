package workqueue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	transportstore "laz/internal/nodeproto/transport"
	"laz/internal/server/model"
)

type Handler interface {
	Handle(context.Context, transportstore.Message) (Result, error)
}

type HandlerFunc func(context.Context, transportstore.Message) (Result, error)

func (f HandlerFunc) Handle(ctx context.Context, msg transportstore.Message) (Result, error) {
	return f(ctx, msg)
}

type Result struct {
	Status  transportstore.Status
	Output  []byte
	Next    []transportstore.Message
	Events  []model.Event
	RetryAt time.Time
}

type EventSink interface {
	Publish(context.Context, model.Event) model.Event
}

type Worker struct {
	store    transportstore.Store
	events   EventSink
	actorID  string
	handlers map[string]Handler
	logger   *slog.Logger
}

func New(store transportstore.Store, events EventSink, actorID string, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{store: store, events: events, actorID: actorID, handlers: map[string]Handler{}, logger: logger}
}

func (w *Worker) Register(typ string, handler Handler) {
	w.handlers[typ] = handler
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	_ = w.store.RequeueExpiredLeases(ctx, w.actorID)
	messages, err := w.store.LeasePending(ctx, w.actorID, 10, 30*time.Second)
	if err != nil {
		w.logger.Warn("lease outbox messages failed", "error", err)
		return
	}
	for _, msg := range messages {
		w.handle(ctx, msg)
	}
}

func (w *Worker) handle(ctx context.Context, msg transportstore.Message) {
	handler := w.handlers[msg.Type]
	if handler == nil {
		_ = w.store.MarkFailed(ctx, msg.ID, "unknown message type", time.Time{})
		return
	}
	result, err := handler.Handle(ctx, msg)
	if err != nil {
		retryAt := result.RetryAt
		if retryAt.IsZero() {
			retryAt = time.Now().UTC().Add(10 * time.Second)
		}
		_ = w.store.MarkFailed(ctx, msg.ID, err.Error(), retryAt)
		return
	}
	for _, next := range result.Next {
		if next.ActorID == "" {
			next.ActorID = w.actorID
		}
		if next.Status == "" {
			next.Status = transportstore.StatusPending
		}
		if next.AvailableAt.IsZero() {
			next.AvailableAt = time.Now().UTC()
		}
		if err := w.store.Enqueue(ctx, next); err != nil {
			_ = w.store.MarkFailed(ctx, msg.ID, err.Error(), time.Now().UTC().Add(10*time.Second))
			return
		}
	}
	for _, event := range result.Events {
		if w.events != nil {
			w.events.Publish(ctx, event)
		}
	}
	status := result.Status
	if status == "" {
		status = transportstore.StatusApplied
	}
	switch status {
	case transportstore.StatusApplied:
		_ = w.store.MarkApplied(ctx, msg.ID, result.Output)
	case transportstore.StatusAcked:
		_ = w.store.MarkAcked(ctx, msg.ID, result.Output)
	case transportstore.StatusExpired:
		_ = w.store.MarkExpired(ctx, msg.ID, "")
	case transportstore.StatusFailed:
		_ = w.store.MarkFailed(ctx, msg.ID, string(result.Output), time.Time{})
	default:
		_ = w.store.MarkFailed(ctx, msg.ID, errors.New("unsupported handler status").Error(), time.Time{})
	}
}
