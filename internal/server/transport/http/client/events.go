package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	eventsvc "laz/internal/server/events"
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
	accountID, ok := a.eventAccountID(w, r)
	if !ok {
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

	topic := eventsvc.ClientTopic(accountID)
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

func (a *App) eventAccountID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if raw := strings.TrimSpace(r.URL.Query().Get("session_token")); raw != "" {
		session, _, err := a.clientAuth.AuthenticateSession(raw)
		if err != nil {
			writeClientAuthError(w, err)
			return "", false
		}
		return session.AccountID, true
	}
	token, ok := a.authenticateClientToken(w, r, r.URL.Query().Get("token"))
	if !ok {
		return "", false
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return "", false
	}
	if !challengeMatches(account.Username, r.URL.Query().Get("challenge")) {
		httpx.Error(w, http.StatusForbidden, "invalid challenge")
		return "", false
	}
	return token.AccountID, true
}

func writeSSE(w http.ResponseWriter, event model.Event) {
	raw, _ := json.Marshal(map[string]any{
		"id":            event.ID,
		"type":          event.Type,
		"entity_type":   event.EntityType,
		"entity_id":     event.EntityID,
		"message":       event.Message,
		"payload":       decodeEventPayload(event.PayloadJSON),
		"created_at_ms": event.CreatedAtMS,
	})
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
