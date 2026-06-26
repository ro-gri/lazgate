package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	afterMS := parseEventCursor(r)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	for _, event := range a.events.ListAfter(afterMS, 100) {
		if isAdminEvent(event) {
			writeSSE(w, event)
		}
		afterMS = event.CreatedAtMS
	}
	flusher.Flush()

	events := a.events.Subscribe(r.Context())
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if !isAdminEvent(event) {
				continue
			}
			if event.CreatedAtMS <= afterMS {
				continue
			}
			writeSSE(w, event)
			afterMS = event.CreatedAtMS
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func isAdminEvent(event model.Event) bool {
	return event.Actor == "admin" || strings.HasPrefix(event.Actor, "admin:")
}

func parseEventCursor(r *http.Request) int64 {
	for _, value := range []string{r.URL.Query().Get("after_ms"), r.Header.Get("Last-Event-ID")} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func writeSSE(w http.ResponseWriter, event model.Event) {
	raw, _ := json.Marshal(eventPayload(event))
	_, _ = fmt.Fprintf(w, "id: %d\n", event.CreatedAtMS)
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
		"actor":         event.Actor,
		"message":       event.Message,
		"payload":       payload,
		"created_at_ms": event.CreatedAtMS,
	}
}
