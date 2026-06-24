package agentcommands

import (
	"context"
	"encoding/json"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"

	"github.com/google/uuid"
)

const defaultDispatchInterval = time.Second

type Store interface {
	ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand
	CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error
}

type MessageBuilder[Resp any] func(requestID string, command *commonproto.RuntimeCommand) *Resp

type Service[Resp any] struct {
	store    Store
	build    MessageBuilder[Resp]
	interval time.Duration
}

func New[Resp any](store Store, build MessageBuilder[Resp]) *Service[Resp] {
	return &Service[Resp]{store: store, build: build, interval: defaultDispatchInterval}
}

func (s *Service[Resp]) RunNodeTasks(ctx context.Context, node *agentprotocol.StreamNode[*commonproto.AgentMessage, *Resp]) error {
	s.Dispatch(node)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Dispatch(node)
		case <-ctx.Done():
			return nil
		case <-node.Done():
			return nil
		}
	}
}

func (s *Service[Resp]) Dispatch(node *agentprotocol.StreamNode[*commonproto.AgentMessage, *Resp]) {
	if s == nil || s.store == nil || s.build == nil || node == nil {
		return
	}
	for _, command := range s.store.ListPendingRuntimeCommands(node.InstallationID()) {
		node.Send(s.build(uuid.NewString(), runtimeCommandToProto(command)))
	}
}

func (s *Service[Resp]) HandleRuntimeCommandResult(_ context.Context, node *agentprotocol.StreamNode[*commonproto.AgentMessage, *Resp], result *commonproto.RuntimeCommandResult) {
	if s == nil || s.store == nil || node == nil || result == nil || result.GetCommandId() == "" {
		return
	}
	status := model.RuntimeCommandStatusError
	if result.GetStatus() == "ok" || result.GetStatus() == "skipped" {
		status = model.RuntimeCommandStatusCompleted
	}
	message := result.GetMessage()
	if message == "" {
		message = result.GetStatus()
	}
	_ = s.store.CompleteRuntimeCommandForInstallation(node.InstallationID(), result.GetCommandId(), status, message, result.GetError())
}

func runtimeCommandToProto(command model.RuntimeCommand) *commonproto.RuntimeCommand {
	payload := map[string]string{}
	_ = json.Unmarshal([]byte(command.Payload), &payload)
	return &commonproto.RuntimeCommand{
		Id:        command.ID,
		Type:      command.Type,
		IssuedMs:  command.IssuedAt.UnixMilli(),
		ExpiresMs: command.ExpiresAt.UnixMilli(),
		Payload:   payload,
	}
}
