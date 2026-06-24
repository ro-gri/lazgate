package workqueue

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	queue "laz/internal/queue"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	"laz/internal/server/provisioning"
)

type AWGInstaller interface {
	ExecuteAWGStep(context.Context, string, provisioning.AWGState) (provisioning.AWGStepResult, error)
}
type AWGInstallHandler struct{ Installer AWGInstaller }
type AWGInstallPayload struct {
	OperationID string                `json:"operation_id"`
	State       provisioning.AWGState `json:"state"`
}

func NewAWGInstallMessage(id string, s provisioning.AWGState) queue.Message {
	return awgMessage(id, s, provisioning.AWGSteps[0], 0)
}
func awgMessage(id string, s provisioning.AWGState, step string, wait time.Duration) queue.Message {
	raw, _ := json.Marshal(AWGInstallPayload{OperationID: id, State: s})
	return queue.Message{ID: newMessageID("awginst"), ActorID: "server", Type: step, Payload: raw, Status: queue.StatusPending, AvailableAt: time.Now().UTC().Add(wait), ExpiresAt: s.Deadline.Add(5 * time.Minute)}
}
func (h AWGInstallHandler) Handle(ctx context.Context, msg queue.Message) (Result, error) {
	var p AWGInstallPayload
	if err := json.Unmarshal(msg.Payload, &p); err != nil {
		id := newMessageID("err")
		slog.Error("invalid AWG provisioning payload", "error_id", id, "message_id", msg.ID)
		return Result{Status: queue.StatusFailed, Output: []byte(mustJSON(map[string]any{"error_code": "INVALID_PROVISIONING_STATE", "error_id": id}))}, nil
	}
	r, err := h.Installer.ExecuteAWGStep(ctx, msg.Type, p.State)
	if ctx.Err() != nil {
		return Result{RetryAt: time.Now().UTC().Add(10 * time.Second)}, ctx.Err()
	}
	status := "applied"
	if err != nil {
		status = "failed"
	}
	if r.Done {
		status = "done"
	}
	if err == nil && !r.Done && r.Next == msg.Type {
		return Result{Status: queue.StatusApplied, Next: []queue.Message{awgMessage(p.OperationID, r.State, r.Next, r.Wait)}}, nil
	}
	payload := map[string]any{"operation_id": p.OperationID, "step_type": msg.Type, "status": status}
	if err != nil {
		code := "AWG_PROVISIONING_FAILED"
		for _, allowed := range []string{"UNSUPPORTED_OS", "UNSUPPORTED_KERNEL", "MISSING_KERNEL_HEADERS", "DKMS_BUILD_FAILED", "REBOOT_TIMEOUT", "INSTALLER_FAILED", "INSTALLER_DOWNLOAD_FAILED", "HOST_CHECK_FAILED", "SSH_CONNECTION_FAILED", "AWG_INTERFACE_FAILED", "FIREWALL_CONFIGURATION_FAILED", "AGENT_INSTALL_FAILED", "AGENT_CONNECTION_FAILED"} {
			if strings.HasPrefix(err.Error(), allowed+":") {
				code = allowed
				break
			}
		}
		id := newMessageID("err")
		payload["message"] = code
		payload["error_code"] = code
		payload["error_id"] = id
		slog.Error("AWG provisioning step failed; inspect root-only host journal", "error_id", id, "operation_id", p.OperationID, "step", msg.Type, "error_code", code)
	}
	event := model.Event{Topic: eventsvc.AdminTopic(p.State.EventPrincipal), Type: "awg.install." + status, EntityType: "awg_install", EntityID: p.OperationID, Message: msg.Type + ": " + status, PayloadJSON: mustJSON(payload)}
	if err != nil {
		return Result{Status: queue.StatusFailed, Events: []model.Event{event}, Output: []byte(mustJSON(payload))}, nil
	}
	if r.Done {
		payload["node"] = publicNode(r.State.Identity.Node)
		out, _ := json.Marshal(payload)
		event.PayloadJSON = string(out)
		return Result{Status: queue.StatusApplied, Output: out, Events: []model.Event{event}}, nil
	}
	return Result{Status: queue.StatusApplied, Next: []queue.Message{awgMessage(p.OperationID, r.State, r.Next, r.Wait)}, Events: []model.Event{event}}, nil
}
