package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"laz/internal/queue"
	"laz/internal/server/events"
	"laz/internal/server/provisioning"
)

type awgStepExecutor func(context.Context, string, provisioning.AWGState) (provisioning.AWGStepResult, error)

func (f awgStepExecutor) ExecuteAWGStep(ctx context.Context, step string, state provisioning.AWGState) (provisioning.AWGStepResult, error) {
	return f(ctx, step, state)
}

func TestAWGInstallationUsesNormalOutboxContinuation(t *testing.T) {
	s := provisioning.AWGState{EventPrincipal: "owner-id", Deadline: time.Now().Add(time.Hour)}
	message := NewAWGInstallMessage("installation", s)
	if message.Type != "awg.install.prepare" {
		t.Fatal("queue must start after synchronous SSH bootstrap")
	}
	if strings.Contains(string(message.Payload), "bootstrap_password") || strings.Contains(string(message.Payload), "bootstrap_user") {
		t.Fatal("outbox schema contains bootstrap credentials")
	}
	handler := AWGInstallHandler{Installer: awgStepExecutor(func(_ context.Context, step string, state provisioning.AWGState) (provisioning.AWGStepResult, error) {
		state.BootID = "boot-one"
		return provisioning.AWGStepResult{State: state, Next: "awg.install.launch", Wait: time.Second}, nil
	})}
	result, err := handler.Handle(context.Background(), message)
	if err != nil || result.Status != queue.StatusApplied || len(result.Next) != 1 || len(result.Events) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if result.Events[0].Topic != events.AdminTopic("owner-id") {
		t.Fatal("event escaped its initiating admin")
	}
	if strings.Contains(result.Events[0].PayloadJSON, "sensitive") {
		t.Fatal("event leaked a secret")
	}
	var next AWGInstallPayload
	if err := json.Unmarshal(result.Next[0].Payload, &next); err != nil {
		t.Fatal(err)
	}
	if next.State.BootID != "boot-one" || next.OperationID != "installation" {
		t.Fatal("continuation lost state or retained bootstrap password")
	}
	if result.Next[0].Type != "awg.install.launch" || !result.Next[0].AvailableAt.After(time.Now()) {
		t.Fatal("continuation was not scheduled")
	}
}

func TestAWGFailureEventDoesNotExposeRemoteError(t *testing.T) {
	handler := AWGInstallHandler{Installer: awgStepExecutor(func(_ context.Context, _ string, state provisioning.AWGState) (provisioning.AWGStepResult, error) {
		return provisioning.AWGStepResult{State: state}, errors.New("INSTALLER_FAILED: private-key=secret")
	})}
	result, err := handler.Handle(context.Background(), NewAWGInstallMessage("installation", provisioning.AWGState{Deadline: time.Now().Add(time.Hour)}))
	if err != nil || result.Status != queue.StatusFailed || len(result.Events) != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if strings.Contains(string(result.Output)+result.Events[0].PayloadJSON, "private-key") {
		t.Fatal("remote error leaked")
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Output, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error_code"] != "INSTALLER_FAILED" || payload["error_id"] == "" {
		t.Fatal("missing safe diagnostic reference")
	}
}

func TestAWGShutdownDoesNotBecomeTerminalFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handler := AWGInstallHandler{Installer: awgStepExecutor(func(_ context.Context, _ string, state provisioning.AWGState) (provisioning.AWGStepResult, error) {
		cancel()
		return provisioning.AWGStepResult{State: state}, context.Canceled
	})}
	result, err := handler.Handle(ctx, NewAWGInstallMessage("installation", provisioning.AWGState{Deadline: time.Now().Add(time.Hour)}))
	if !errors.Is(err, context.Canceled) || result.RetryAt.IsZero() || len(result.Events) != 0 {
		t.Fatalf("shutdown must leave a retryable step, not a failure event: %+v %v", result, err)
	}
}
