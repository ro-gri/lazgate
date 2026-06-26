package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"laz/internal/server/model"
	"laz/internal/server/transport/http/httpx"
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
	token, ok := a.authenticateClientToken(w, r, r.URL.Query().Get("token"))
	if !ok {
		return
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if !challengeMatches(account.Username, r.URL.Query().Get("challenge")) {
		httpx.Error(w, http.StatusForbidden, "invalid challenge")
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
		if clientEventBelongsToAccount(event, token.AccountID) {
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
			if event.CreatedAtMS <= afterMS {
				continue
			}
			afterMS = event.CreatedAtMS
			if !clientEventBelongsToAccount(event, token.AccountID) {
				continue
			}
			writeSSE(w, event)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
	}
}

func clientEventBelongsToAccount(event model.Event, accountID string) bool {
	if event.Actor == "client:"+accountID {
		return true
	}
	payload := decodeEventPayload(event.PayloadJSON)
	if value, ok := payload["account_id"].(string); ok && value == accountID {
		return true
	}
	if value, ok := payload["account"]; ok {
		if m, ok := value.(map[string]any); ok {
			if id, ok := m["id"].(string); ok && id == accountID {
				return true
			}
		}
	}
	return false
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
	raw, _ := json.Marshal(map[string]any{
		"id":            event.ID,
		"type":          event.Type,
		"entity_type":   event.EntityType,
		"entity_id":     event.EntityID,
		"actor":         event.Actor,
		"message":       event.Message,
		"payload":       decodeEventPayload(event.PayloadJSON),
		"created_at_ms": event.CreatedAtMS,
	})
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

func decodeEventPayload(raw string) map[string]any {
	var out map[string]any
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
