package workqueue

import (
	"context"
	"encoding/json"
	"time"

	transportstore "laz/internal/nodeproto/transport"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
)

const TypeNodeAuthRefresh = "node.auth.refresh"

type AuthRefresher interface {
	RefreshUserAuthApplied(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (appliedSnapshotVersionMS int64, err error)
}

type AuthStore interface {
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.Connection, error)
}

type AuthRefreshHandler struct {
	Store     AuthStore
	Refresher AuthRefresher
}

type AuthRefreshPayload struct {
	NodeID            string `json:"node_id"`
	AccountID         string `json:"account_id"`
	SnapshotVersionMS int64  `json:"snapshot_version_ms"`
}

func NewAuthRefreshMessage(payload AuthRefreshPayload) transportstore.Message {
	raw, _ := json.Marshal(payload)
	return transportstore.Message{
		ID:          newMessageID("auth"),
		ActorID:     "server",
		Type:        TypeNodeAuthRefresh,
		Payload:     raw,
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(15 * time.Minute),
	}
}

func (h AuthRefreshHandler) Handle(ctx context.Context, msg transportstore.Message) (Result, error) {
	var payload AuthRefreshPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return Result{Status: transportstore.StatusFailed}, err
	}
	applied, err := h.Refresher.RefreshUserAuthApplied(ctx, payload.NodeID, payload.AccountID, payload.SnapshotVersionMS)
	if err != nil {
		return Result{RetryAt: time.Now().UTC().Add(15 * time.Second)}, err
	}
	finalized, err := h.Store.FinalizeConnectionsForAuthSnapshot(payload.NodeID, payload.AccountID, applied)
	if err != nil {
		return Result{RetryAt: time.Now().UTC().Add(15 * time.Second)}, err
	}
	events := make([]model.Event, 0, len(finalized)*2)
	for _, connection := range finalized {
		adminPayload := map[string]any{
			"node_id":       payload.NodeID,
			"account_id":    payload.AccountID,
			"client_id":     connection.ClientID,
			"connection_id": connection.ID,
			"status":        connection.Status,
		}
		events = append(events, model.Event{
			Topic:       eventsvc.AdminTopic("admin"),
			Type:        "connection.applied",
			EntityType:  "connection",
			EntityID:    connection.ID,
			Message:     "Изменение подключения применено.",
			PayloadJSON: mustJSON(adminPayload),
		})
		events = append(events, model.Event{
			Topic:       eventsvc.ClientTopic(payload.AccountID),
			Type:        "client.connection.applied",
			EntityType:  "connection",
			EntityID:    connection.ID,
			Message:     "Изменение подключения применено.",
			PayloadJSON: mustJSON(adminPayload),
		})
	}
	out, _ := json.Marshal(map[string]any{"applied_snapshot_version_ms": applied, "finalized": len(finalized)})
	return Result{Status: transportstore.StatusApplied, Output: out, Events: events}, nil
}
