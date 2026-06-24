package workqueue

import (
	"context"
	"encoding/json"
	"time"

	transportstore "laz/internal/queue"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	provisioning "laz/internal/server/provisioning"
)

const (
	TypeHysteriaInstallConnect       = provisioning.StepTypeConnect
	TypeHysteriaInstallCheckSystem   = provisioning.StepTypeCheckSystem
	TypeHysteriaInstallCreateUser    = provisioning.StepTypeCreateUser
	TypeHysteriaInstallInstallDetect = provisioning.StepTypeInstallDetect
	TypeHysteriaInstallWriteConfig   = provisioning.StepTypeWriteConfig
	TypeHysteriaInstallInstallAgent  = provisioning.StepTypeInstallAgent
	TypeHysteriaInstallStartService  = provisioning.StepTypeStartService
	TypeHysteriaInstallVerify        = provisioning.StepTypeVerify
	TypeHysteriaInstallRegisterNode  = provisioning.StepTypeRegisterNode
	TypeHysteriaInstallWaitAgent     = provisioning.StepTypeWaitAgent
	TypeHysteriaInstallDone          = provisioning.StepTypeDone
)

var hysteriaNextStep = map[string]string{
	TypeHysteriaInstallConnect:       TypeHysteriaInstallCheckSystem,
	TypeHysteriaInstallCheckSystem:   TypeHysteriaInstallCreateUser,
	TypeHysteriaInstallCreateUser:    TypeHysteriaInstallInstallDetect,
	TypeHysteriaInstallInstallDetect: TypeHysteriaInstallWriteConfig,
	TypeHysteriaInstallWriteConfig:   TypeHysteriaInstallRegisterNode,
	TypeHysteriaInstallRegisterNode:  TypeHysteriaInstallInstallAgent,
	TypeHysteriaInstallInstallAgent:  TypeHysteriaInstallStartService,
	TypeHysteriaInstallStartService:  TypeHysteriaInstallVerify,
	TypeHysteriaInstallVerify:        TypeHysteriaInstallWaitAgent,
	TypeHysteriaInstallWaitAgent:     TypeHysteriaInstallDone,
}

type HysteriaInstaller interface {
	ExecuteHysteriaStep(context.Context, string, provisioning.InstallState) (provisioning.StepResult, error)
}

type HysteriaInstallHandler struct {
	Installer HysteriaInstaller
}

type HysteriaInstallPayload struct {
	OperationID string                    `json:"operation_id"`
	State       provisioning.InstallState `json:"state"`
}

func NewHysteriaInstallMessage(operationID string, state provisioning.InstallState) transportstore.Message {
	raw, _ := json.Marshal(HysteriaInstallPayload{OperationID: operationID, State: state})
	return transportstore.Message{
		ID:          newMessageID("hy2inst"),
		ActorID:     "server",
		Type:        TypeHysteriaInstallInstallDetect,
		Payload:     raw,
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(30 * time.Minute),
	}
}

func (h HysteriaInstallHandler) Handle(ctx context.Context, msg transportstore.Message) (Result, error) {
	var payload HysteriaInstallPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return Result{Status: transportstore.StatusFailed}, err
	}
	stepResult, err := h.Installer.ExecuteHysteriaStep(ctx, msg.Type, payload.State)
	if err != nil {
		return Result{
			Events:  []model.Event{hysteriaStepEvent(payload.OperationID, stepResult.Step, "hysteria.install.step.failed")},
			RetryAt: time.Now().UTC().Add(30 * time.Second),
		}, err
	}
	events := []model.Event{hysteriaStepEvent(payload.OperationID, stepResult.Step, "hysteria.install.step.applied")}
	if stepResult.Done {
		events = append(events, hysteriaDoneEvent(payload.OperationID, stepResult.State))
		out, _ := json.Marshal(publicHysteriaDonePayload(payload.OperationID, stepResult.State))
		return Result{Status: transportstore.StatusApplied, Output: out, Events: events}, nil
	}
	nextType := hysteriaNextStep[msg.Type]
	nextRaw, _ := json.Marshal(HysteriaInstallPayload{OperationID: payload.OperationID, State: stepResult.State})
	next := transportstore.Message{
		ID:          newMessageID("hy2inst"),
		ActorID:     "server",
		Type:        nextType,
		Payload:     nextRaw,
		Status:      transportstore.StatusPending,
		AvailableAt: time.Now().UTC(),
		ExpiresAt:   time.Now().UTC().Add(30 * time.Minute),
	}
	return Result{Status: transportstore.StatusApplied, Next: []transportstore.Message{next}, Events: events}, nil
}

func hysteriaStepEvent(operationID string, step provisioning.Step, typ string) model.Event {
	status := step.Status
	if typ == "hysteria.install.step.failed" {
		status = provisioning.StepFailed
	}
	return model.Event{
		Topic:      eventsvc.AdminTopic("admin"),
		Type:       typ,
		EntityType: "hysteria_install",
		EntityID:   operationID,
		Message:    step.Name + ": " + localizedStepStatus(status),
		PayloadJSON: mustJSON(map[string]any{
			"operation_id": operationID,
			"step_type":    step.Type,
			"step":         step.Name,
			"status":       status,
			"status_text":  localizedStepStatus(status),
			"message":      step.Message,
		}),
	}
}

func localizedStepStatus(status provisioning.StepStatus) string {
	switch status {
	case provisioning.StepPending:
		return "ожидает"
	case provisioning.StepRunning:
		return "выполняется"
	case provisioning.StepOK:
		return "готово"
	case provisioning.StepFailed:
		return "ошибка"
	default:
		return string(status)
	}
}

func hysteriaDoneEvent(operationID string, state provisioning.InstallState) model.Event {
	payload := publicHysteriaDonePayload(operationID, state)
	return model.Event{
		Topic:       eventsvc.AdminTopic("admin"),
		Type:        "hysteria.install.done",
		EntityType:  "node",
		EntityID:    state.Node.ID,
		Message:     "Сервер Hysteria2 VPS готов.",
		PayloadJSON: mustJSON(payload),
	}
}

func publicHysteriaDonePayload(operationID string, state provisioning.InstallState) map[string]any {
	return map[string]any{
		"operation_id":     operationID,
		"status":           "done",
		"node":             publicNode(state.Node),
		"public_domain":    state.PublicDomain,
		"hysteria_port":    state.Input.HysteriaPort,
		"service_name":     state.ServiceName,
		"generated_domain": state.GeneratedDomain,
	}
}

func publicNode(node model.Node) map[string]any {
	return map[string]any{
		"id": node.ID, "name": node.Name, "type": node.Type, "region": node.Region, "status": node.Status,
	}
}
