package workqueue

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	transportstore "laz/internal/queue"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	provisioning "laz/internal/server/provisioning"
)

const (
	TypeHysteriaUninstallCleanup = provisioning.StepTypeUninstallCleanup
	TypeHysteriaDetachCleanup    = provisioning.StepTypeDetachCleanup
)

type HysteriaUninstaller interface {
	SoftDeletePreparedNode(provisioning.UninstallState) (model.Node, error)
	ExecuteHysteriaUninstallStep(context.Context, string, provisioning.UninstallState) error
}

type HysteriaUninstallHandler struct {
	Uninstaller HysteriaUninstaller
}

type HysteriaUninstallPayload struct {
	OperationID string                      `json:"operation_id"`
	State       provisioning.UninstallState `json:"state"`
}

func NewHysteriaUninstallMessage(operationID string, state provisioning.UninstallState) transportstore.Message {
	return newHysteriaRemovalMessage(operationID, state, TypeHysteriaUninstallCleanup, "hy2uninst")
}

func NewHysteriaDetachMessage(operationID string, state provisioning.UninstallState) transportstore.Message {
	return newHysteriaRemovalMessage(operationID, state, TypeHysteriaDetachCleanup, "hy2detach")
}

func newHysteriaRemovalMessage(operationID string, state provisioning.UninstallState, typ string, prefix string) transportstore.Message {
	raw, _ := json.Marshal(HysteriaUninstallPayload{OperationID: operationID, State: state})
	return transportstore.Message{
		ID:          newMessageID(prefix),
		ActorID:     "server",
		Type:        typ,
		Payload:     raw,
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(30 * time.Minute),
	}
}

func (h HysteriaUninstallHandler) Handle(ctx context.Context, msg transportstore.Message) (Result, error) {
	var payload HysteriaUninstallPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return Result{Status: transportstore.StatusFailed}, err
	}
	eventPrefix := "hysteria.uninstall"
	if msg.Type == TypeHysteriaDetachCleanup {
		eventPrefix = "hysteria.detach"
	}
	if msg.Type == TypeHysteriaDetachCleanup {
		if err := h.Uninstaller.ExecuteHysteriaUninstallStep(ctx, msg.Type, payload.State); err != nil {
			return Result{
				Events:  []model.Event{hysteriaUninstallEvent(payload.OperationID, payload.State, eventPrefix+".failed", hysteriaRemovalMessage(eventPrefix, false))},
				RetryAt: time.Now().UTC().Add(30 * time.Second),
			}, err
		}
	}
	deletedNode, err := h.Uninstaller.SoftDeletePreparedNode(payload.State)
	if err != nil {
		return Result{
			Events:  []model.Event{hysteriaUninstallEvent(payload.OperationID, payload.State, eventPrefix+".failed", hysteriaRemovalMessage(eventPrefix, false))},
			RetryAt: time.Now().UTC().Add(30 * time.Second),
		}, err
	}
	payload.State.Node = deletedNode
	if msg.Type != TypeHysteriaDetachCleanup {
		if err := h.Uninstaller.ExecuteHysteriaUninstallStep(ctx, msg.Type, payload.State); err != nil {
			return Result{
				Events:  []model.Event{hysteriaUninstallEvent(payload.OperationID, payload.State, eventPrefix+".failed", hysteriaRemovalMessage(eventPrefix, false))},
				RetryAt: time.Now().UTC().Add(30 * time.Second),
			}, err
		}
	}
	return Result{
		Status: transportstore.StatusApplied,
		Events: []model.Event{hysteriaUninstallEvent(payload.OperationID, payload.State, eventPrefix+".done", hysteriaRemovalMessage(eventPrefix, true))},
	}, nil
}

func hysteriaUninstallEvent(operationID string, state provisioning.UninstallState, typ, message string) model.Event {
	return model.Event{
		Topic:      eventsvc.AdminTopic("admin"),
		Type:       typ,
		EntityType: "node",
		EntityID:   state.NodeID,
		Message:    message,
		PayloadJSON: mustJSON(map[string]any{
			"operation_id": operationID,
			"status":       uninstallEventStatus(typ),
			"node_id":      state.NodeID,
			"node_name":    state.Node.Name,
		}),
	}
}

func uninstallEventStatus(typ string) string {
	if strings.HasSuffix(typ, ".done") {
		return "done"
	}
	return "failed"
}

func hysteriaRemovalMessage(eventPrefix string, done bool) string {
	if eventPrefix == "hysteria.detach" {
		if done {
			return "Подключение Hysteria2 VPS удалено."
		}
		return "Не удалось удалить подключение Hysteria2 VPS."
	}
	if done {
		return "Удаление Hysteria2 VPS завершено."
	}
	return "Не удалось удалить Hysteria2 VPS."
}
