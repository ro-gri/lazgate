package client

import (
	"context"
	"encoding/json"

	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
)

func (a *App) publishClientEvent(ctx context.Context, accountID string, event model.Event) model.Event {
	if a.events == nil {
		return event
	}
	event.Topic = eventsvc.ClientTopic(accountID)
	return a.events.Publish(ctx, event)
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
