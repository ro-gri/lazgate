package agentcommands

import (
	"context"
	"testing"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"
)

type testStore struct {
	pending      []model.RuntimeCommand
	installation string
	completedID  string
	status       model.RuntimeCommandStatus
}

func (s *testStore) ListPendingRuntimeCommands(installationID string) []model.RuntimeCommand {
	s.installation = installationID
	return s.pending
}

func (s *testStore) CompleteRuntimeCommandForInstallation(installationID, id string, status model.RuntimeCommandStatus, _, _ string) error {
	s.installation = installationID
	s.completedID = id
	s.status = status
	return nil
}

type testMessage struct {
	requestID string
	command   *commonproto.RuntimeCommand
}

func TestServiceBindsDeliveryAndCompletionToInstallation(t *testing.T) {
	store := &testStore{pending: []model.RuntimeCommand{{
		ID: "cmd-1", Type: "inspect", Payload: `{"detail":"full"}`,
		IssuedAt: time.Unix(10, 0), ExpiresAt: time.Unix(20, 0),
	}}}
	service := New(store, func(requestID string, command *commonproto.RuntimeCommand) *testMessage {
		return &testMessage{requestID: requestID, command: command}
	})
	node := agentprotocol.NewStreamNode[*commonproto.AgentMessage, *testMessage]("node-1", "installation-1", nil, 1, 1)

	service.Dispatch(node)
	select {
	case message := <-node.Outgoing():
		if message.requestID == "" || message.command.GetId() != "cmd-1" || message.command.GetPayload()["detail"] != "full" {
			t.Fatalf("unexpected runtime message: %+v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime command was not dispatched")
	}
	if store.installation != "installation-1" {
		t.Fatalf("pending command lookup installation = %q", store.installation)
	}

	service.HandleRuntimeCommandResult(context.Background(), node, &commonproto.RuntimeCommandResult{CommandId: "cmd-1", Status: "ok"})
	if store.installation != "installation-1" || store.completedID != "cmd-1" || store.status != model.RuntimeCommandStatusCompleted {
		t.Fatalf("unexpected completion: %+v", store)
	}
}

func TestCommandCompletionStatuses(t *testing.T) {
	for _, tc := range []struct {
		result string
		want   model.RuntimeCommandStatus
	}{
		{"ok", model.RuntimeCommandStatusCompleted},
		{"skipped", model.RuntimeCommandStatusCompleted},
		{"error", model.RuntimeCommandStatusError},
		{"expired", model.RuntimeCommandStatusError},
	} {
		st := &testStore{}
		service := New(st, func(string, *commonproto.RuntimeCommand) *testMessage { return nil })
		node := agentprotocol.NewStreamNode[*commonproto.AgentMessage, *testMessage]("node", "installation", nil, 1, 1)
		service.HandleRuntimeCommandResult(context.Background(), node, &commonproto.RuntimeCommandResult{CommandId: "command", Status: tc.result})
		if st.status != tc.want {
			t.Errorf("%s: got %s, want %s", tc.result, st.status, tc.want)
		}
	}
}
