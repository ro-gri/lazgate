package workqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.FinalizedConnection, error)
	GetAccount(id string) (model.Account, error)
	GetClientForAccount(accountID, clientID string) (model.Client, error)
	GetNode(id string) (model.Node, error)
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
	accountLabel := h.accountEventLabel(payload.AccountID)
	nodeLabel := h.nodeEventLabel(payload.NodeID)

	events := make([]model.Event, 0, len(finalized)*2)
	for _, item := range finalized {
		connection := item.Connection
		clientLabel := h.clientEventLabel(payload.AccountID, connection.ClientID)
		action := connectionAppliedAction(item.PreviousStatus)
		adminMessage := fmt.Sprintf("Подключение %s для %s %s на %s.", clientLabel, accountLabel, action.AdminVerb, nodeLabel)
		clientMessage := fmt.Sprintf("Ваше подключение %s %s на %s.", clientLabel, action.ClientVerb, nodeLabel)
		adminPayload := map[string]any{
			"node_id":         payload.NodeID,
			"node_name":       nodeLabel,
			"account_id":      payload.AccountID,
			"account_name":    accountLabel,
			"client_id":       connection.ClientID,
			"client_name":     clientLabel,
			"connection_id":   connection.ID,
			"previous_status": item.PreviousStatus,
			"status":          connection.Status,
			"action":          action.Name,
		}
		events = append(events, model.Event{
			Topic:       eventsvc.AdminTopic("admin"),
			Type:        "connection.applied",
			EntityType:  "connection",
			EntityID:    connection.ID,
			Message:     adminMessage,
			PayloadJSON: mustJSON(adminPayload),
		})
		events = append(events, model.Event{
			Topic:       eventsvc.ClientTopic(payload.AccountID),
			Type:        "client.connection.applied",
			EntityType:  "connection",
			EntityID:    connection.ID,
			Message:     clientMessage,
			PayloadJSON: mustJSON(adminPayload),
		})
	}
	out, _ := json.Marshal(map[string]any{"applied_snapshot_version_ms": applied, "finalized": len(finalized)})
	return Result{Status: transportstore.StatusApplied, Output: out, Events: events}, nil
}

type connectionAction struct {
	Name       string
	AdminVerb  string
	ClientVerb string
}

func connectionAppliedAction(previousStatus model.Status) connectionAction {
	switch previousStatus {
	case model.StatusPendingCreate:
		return connectionAction{Name: "created", AdminVerb: "создано", ClientVerb: "создано"}
	case model.StatusPendingHold:
		return connectionAction{Name: "blocked", AdminVerb: "заблокировано", ClientVerb: "заблокировано"}
	case model.StatusPendingResume:
		return connectionAction{Name: "unblocked", AdminVerb: "разблокировано", ClientVerb: "разблокировано"}
	case model.StatusPendingDelete:
		return connectionAction{Name: "deleted", AdminVerb: "удалено", ClientVerb: "удалено"}
	default:
		return connectionAction{Name: "updated", AdminVerb: "обновлено", ClientVerb: "обновлено"}
	}
}

func (h AuthRefreshHandler) accountEventLabel(accountID string) string {
	account, err := h.Store.GetAccount(accountID)
	if err != nil {
		return accountID
	}
	if label := strings.TrimSpace(account.DisplayName); label != "" {
		return label
	}
	if label := strings.TrimSpace(account.Username); label != "" {
		return label
	}
	return accountID
}

func (h AuthRefreshHandler) clientEventLabel(accountID, clientID string) string {
	client, err := h.Store.GetClientForAccount(accountID, clientID)
	if err != nil {
		return clientID
	}
	return clientLabel(client, clientID)
}

func clientLabel(client model.Client, fallback string) string {
	if label := strings.TrimSpace(client.Name); label != "" {
		return label
	}
	if label := strings.TrimSpace(client.Slug); label != "" {
		return label
	}
	return fallback
}

func (h AuthRefreshHandler) nodeEventLabel(nodeID string) string {
	node, err := h.Store.GetNode(nodeID)
	if err != nil {
		return nodeID
	}
	if label := strings.TrimSpace(node.Name); label != "" {
		return label
	}
	return nodeID
}
