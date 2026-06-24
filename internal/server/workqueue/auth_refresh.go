package workqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	transportstore "laz/internal/queue"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
)

const TypeNodeAuthRefresh = "node.auth.refresh"

type AuthRefresher interface {
	RefreshAuthApplied(context.Context, string, string, int64) (int64, error)
}
type AuthRefresherFunc func(context.Context, string, string, int64) (int64, error)

func (f AuthRefresherFunc) RefreshAuthApplied(ctx context.Context, nodeID, accountID string, version int64) (int64, error) {
	return f(ctx, nodeID, accountID, version)
}

type AuthStore interface {
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.Connection, error)
	GetAccount(id string) (model.Account, error)
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
	return transportstore.Message{ID: newMessageID("auth"), ActorID: "server", Type: TypeNodeAuthRefresh, Payload: raw, Status: transportstore.StatusPending, AvailableAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(15 * time.Minute)}
}

func (h AuthRefreshHandler) Handle(ctx context.Context, msg transportstore.Message) (Result, error) {
	var payload AuthRefreshPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return Result{Status: transportstore.StatusFailed}, err
	}
	applied, err := h.Refresher.RefreshAuthApplied(ctx, payload.NodeID, payload.AccountID, payload.SnapshotVersionMS)
	if err != nil {
		return Result{RetryAt: time.Now().UTC().Add(15 * time.Second)}, err
	}
	finalized, err := h.Store.FinalizeConnectionsForAuthSnapshot(payload.NodeID, payload.AccountID, applied)
	if err != nil {
		return Result{RetryAt: time.Now().UTC().Add(15 * time.Second)}, err
	}
	accountLabel, nodeLabel := h.accountEventLabel(payload.AccountID), h.nodeEventLabel(payload.NodeID)
	events := make([]model.Event, 0, len(finalized)*2)
	for _, connection := range finalized {
		action := connectionAppliedAction(connection.AppliedStatus)
		message := fmt.Sprintf("Подключение %s для %s %s на %s.", connection.Name, accountLabel, action.AdminVerb, nodeLabel)
		data := map[string]any{"node_id": payload.NodeID, "node_name": nodeLabel, "account_id": payload.AccountID, "account_name": accountLabel, "connection_id": connection.ID, "connection_name": connection.Name, "desired_status": connection.DesiredStatus, "applied_status": connection.AppliedStatus, "applied_version_ms": connection.AppliedVersionMS, "action": action.Name}
		events = append(events,
			model.Event{Topic: eventsvc.AdminTopic("admin"), Type: "connection.applied", EntityType: "connection", EntityID: connection.ID, Message: message, PayloadJSON: mustJSON(data)},
			model.Event{Topic: eventsvc.ClientTopic(payload.AccountID), Type: "connection.applied", EntityType: "connection", EntityID: connection.ID, Message: "Подключение " + connection.Name + " " + action.ClientVerb + ".", PayloadJSON: mustJSON(data)},
		)
	}
	out, _ := json.Marshal(map[string]any{"applied_snapshot_version_ms": applied, "finalized": len(finalized)})
	return Result{Status: transportstore.StatusApplied, Output: out, Events: events}, nil
}

type connectionAction struct{ Name, AdminVerb, ClientVerb string }

func connectionAppliedAction(status model.ConnectionStatus) connectionAction {
	switch status {
	case model.ConnectionStatusActive:
		return connectionAction{"active", "активировано", "активировано"}
	case model.ConnectionStatusHeld:
		return connectionAction{"held", "заблокировано", "заблокировано"}
	case model.ConnectionStatusDeleted:
		return connectionAction{"deleted", "удалено", "удалено"}
	default:
		return connectionAction{"updated", "обновлено", "обновлено"}
	}
}
func (h AuthRefreshHandler) accountEventLabel(id string) string {
	v, err := h.Store.GetAccount(id)
	if err != nil {
		return id
	}
	if s := strings.TrimSpace(v.DisplayName); s != "" {
		return s
	}
	if s := strings.TrimSpace(v.Username); s != "" {
		return s
	}
	return id
}
func (h AuthRefreshHandler) nodeEventLabel(id string) string {
	v, err := h.Store.GetNode(id)
	if err != nil {
		return id
	}
	if s := strings.TrimSpace(v.Name); s != "" {
		return s
	}
	return id
}
