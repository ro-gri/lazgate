package admin

import (
	"context"
	"encoding/json"

	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
)

func (a *App) publish(ctx context.Context, event model.Event) model.Event {
	if a.events == nil {
		return event
	}
	if event.Topic == "" {
		event.Topic = eventsvc.AdminTopic("admin")
	}
	return a.events.Publish(ctx, event)
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeObject(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
