package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
)

func (a *App) eventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.events == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	topic := eventsvc.AdminTopic("admin")
	ticker := time.NewTicker(800 * time.Millisecond)
	heartbeat := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			for _, event := range a.events.ListPending(topic, 50) {
				writeSSE(w, event)
				flusher.Flush()
				_ = a.events.MarkDelivered(event.ID)
			}
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event model.Event) {
	raw, _ := json.Marshal(eventPayload(event))
	_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
	_, _ = fmt.Fprintf(w, "event: %s\n", safeSSEName(event.Type))
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}

func safeSSEName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "message"
	}
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, value)
}

func eventPayload(event model.Event) map[string]any {
	var payload any = map[string]any{}
	if strings.TrimSpace(event.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(event.PayloadJSON), &payload)
	}
	return map[string]any{
		"id":            event.ID,
		"type":          event.Type,
		"entity_type":   event.EntityType,
		"entity_id":     event.EntityID,
		"message":       event.Message,
		"payload":       payload,
		"created_at_ms": event.CreatedAtMS,
	}
}
