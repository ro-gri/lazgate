package amneziaagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	transportstore "laz/internal/queue"
	"laz/internal/server/agentcommands"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/integrations/nativeawg"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

type agentRequester interface {
	Request(context.Context, string, string, *amneziaproto.ServerMessage) (*commonproto.AgentMessage, error)
}

type deliveryService struct {
	store     Store
	transport transportstore.Store
	requester agentRequester
	commands  *agentcommands.Service[amneziaproto.ServerMessage]
}

func newDeliveryService(st Store, transport transportstore.Store) *deliveryService {
	commands := agentcommands.New(st, func(requestID string, command *commonproto.RuntimeCommand) *amneziaproto.ServerMessage {
		return amneziaServerMessages{}.BuildServerMessage(agentprotocol.ServerMessageParts[*amneziaproto.AuthManifestResponse]{
			RequestID: requestID, Type: "runtime_command", RuntimeCommand: command,
		})
	})
	return &deliveryService{store: st, transport: transport, commands: commands}
}

func (d *deliveryService) RunNodeTasks(ctx context.Context, node *streamNode) error {
	return d.dispatchOutbox(ctx, node)
}

func (d *deliveryService) enqueueServerConfig(ctx context.Context, nodeID string) error {
	config, err := d.store.GetAmneziaWGNodeConfig(nodeID)
	if err != nil {
		return err
	}
	command, err := nativeawg.EditServerConfig(config)
	if err != nil {
		return err
	}
	raw, err := proto.Marshal(command)
	if err != nil {
		return err
	}
	return d.transport.Enqueue(ctx, transportstore.Message{
		ID:        uuid.NewString(),
		ActorID:   nodeID,
		Type:      "edit_server_config",
		Payload:   raw,
		Status:    transportstore.StatusPending,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})
}

func (d *deliveryService) RefreshAmneziaWGAuth(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (*commonproto.AuthRefreshResult, error) {
	requestID := uuid.NewString()
	refresh := &commonproto.AuthRefresh{
		NodeId:            nodeID,
		AccountId:         accountID,
		ManifestStartedAt: time.Now().UnixMilli(),
		SnapshotVersionMs: snapshotVersionMS,
	}
	raw, err := proto.Marshal(refresh)
	if err != nil {
		return nil, err
	}
	if err := d.transport.Enqueue(ctx, transportstore.Message{
		ID:        requestID,
		ActorID:   nodeID,
		Type:      "auth_refresh",
		Payload:   raw,
		Status:    transportstore.StatusPending,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		return nil, err
	}
	return &commonproto.AuthRefreshResult{NodeId: nodeID, AccountId: accountID, Status: "queued", AppliedSnapshotVersionMs: snapshotVersionMS}, nil
}

func (d *deliveryService) dispatchOutbox(ctx context.Context, node *streamNode) error {
	const (
		batchSize = 10
		leaseFor  = 30 * time.Second
	)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var queuedVersion int64
	for {
		config, err := d.store.GetAmneziaWGNodeConfig(node.NodeID())
		if err != nil {
			return err
		}
		if config.ConfigVersionMS != queuedVersion {
			if err := d.enqueueServerConfig(ctx, node.NodeID()); err != nil {
				return err
			}
			queuedVersion = config.ConfigVersionMS
		}
		if err := d.dispatchOutboxOnce(ctx, node, batchSize, leaseFor); err != nil {
			return err
		}
		d.commands.Dispatch(node)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}
	}
}

func (d *deliveryService) dispatchOutboxOnce(ctx context.Context, node *streamNode, limit int, leaseFor time.Duration) error {
	if err := d.transport.RequeueExpiredLeases(ctx, node.NodeID()); err != nil {
		return err
	}
	messages, err := d.transport.LeasePending(ctx, node.NodeID(), limit, leaseFor)
	if err != nil {
		return err
	}
	for _, outbox := range messages {
		switch outbox.Type {
		case "auth_refresh":
			d.dispatchAuthRefresh(ctx, node, outbox)
		case "edit_server_config":
			d.dispatchServerConfig(ctx, node, outbox)
		default:
			_ = d.transport.MarkFailed(ctx, outbox.ID, fmt.Sprintf("unsupported outbox message type %s", outbox.Type), time.Time{})
		}
	}
	return nil
}

func (d *deliveryService) dispatchAuthRefresh(ctx context.Context, node *streamNode, outbox transportstore.Message) {
	refresh := &commonproto.AuthRefresh{}
	if err := proto.Unmarshal(outbox.Payload, refresh); err != nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Time{})
		return
	}
	msg := amneziaServerMessages{}.BuildServerMessage(agentprotocol.ServerMessageParts[*amneziaproto.AuthManifestResponse]{
		RequestID:   outbox.ID,
		Type:        "auth_refresh",
		AuthRefresh: refresh,
	})
	res, err := d.requester.Request(ctx, node.NodeID(), msg.GetRequestId(), msg)
	if err != nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Now().UTC().Add(time.Second))
		return
	}
	applyResult := res.GetAuthRefreshResult()
	if applyResult == nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, "missing amnezia auth refresh result", time.Time{})
		return
	}
	if applyResult.GetStatus() == "ok" || applyResult.GetStatus() == "applied" {
		raw, err := proto.Marshal(applyResult)
		if err != nil {
			_ = d.transport.MarkFailed(ctx, outbox.ID, fmt.Sprintf("marshal auth refresh result: %v", err), time.Time{})
			return
		}
		_ = d.transport.MarkAcked(ctx, outbox.ID, raw)
		return
	}
	errMsg := applyResult.GetError()
	if errMsg == "" {
		errMsg = fmt.Sprintf("amnezia auth refresh status %s", applyResult.GetStatus())
	}
	_ = d.transport.MarkFailed(ctx, outbox.ID, errMsg, time.Time{})
}

func (d *deliveryService) dispatchServerConfig(ctx context.Context, node *streamNode, outbox transportstore.Message) {
	command := &amneziaproto.EditServerConfig{}
	if err := proto.Unmarshal(outbox.Payload, command); err != nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Time{})
		return
	}
	current, err := d.store.GetAmneziaWGNodeConfig(node.NodeID())
	if err != nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Now().Add(time.Second))
		return
	}
	if uint64(current.ConfigVersionMS) != command.GetRevision() {
		_ = d.transport.MarkExpired(ctx, outbox.ID, "superseded configuration")
		return
	}
	msg := &amneziaproto.ServerMessage{Type: outbox.Type, RequestId: outbox.ID, EditServerConfig: command}
	res, err := d.requester.Request(ctx, node.NodeID(), msg.GetRequestId(), msg)
	if err != nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Now().UTC().Add(time.Second))
		return
	}
	applyResult := res.GetServerConfigApplyResult()
	if applyResult == nil {
		_ = d.transport.MarkFailed(ctx, outbox.ID, "missing server config apply result", time.Time{})
		return
	}
	if applyResult.GetStatus() == "ok" || applyResult.GetStatus() == "applied" {
		if errMsg := validateServerConfigApplyResult(node, command, outbox.ID, applyResult); errMsg != "" {
			_ = d.transport.MarkFailed(ctx, outbox.ID, errMsg, time.Time{})
			return
		}
		raw, err := proto.Marshal(applyResult)
		if err != nil {
			_ = d.transport.MarkFailed(ctx, outbox.ID, fmt.Sprintf("marshal server config apply result: %v", err), time.Time{})
			return
		}
		if err := d.store.SetAmneziaWGConfigResult(node.NodeID(), int64(command.GetRevision()), ""); err != nil {
			_ = d.transport.MarkFailed(ctx, outbox.ID, err.Error(), time.Now().Add(time.Second))
			return
		}
		_ = d.transport.MarkAcked(ctx, outbox.ID, raw)
		return
	}
	errMsg := applyResult.GetError()
	if errMsg == "" {
		errMsg = fmt.Sprintf("server config apply status %s", applyResult.GetStatus())
	}
	errorID := uuid.NewString()
	slog.Error("AmneziaWG settings apply failed", "error_id", errorID, "node_id", node.NodeID(), "revision", command.GetRevision(), "error", errMsg)
	_ = d.store.SetAmneziaWGConfigResult(node.NodeID(), int64(command.GetRevision()), errorID)
	_ = d.transport.MarkFailed(ctx, outbox.ID, errMsg, time.Time{})
}

func validateServerConfigApplyResult(node *streamNode, command *amneziaproto.EditServerConfig, requestID string, result *commonproto.ServerConfigApplyResult) string {
	if result.GetNodeId() != command.GetNodeId() || result.GetNodeId() != node.NodeID() {
		return "server config apply result node_id mismatch"
	}
	if result.GetDesiredRevision() != command.GetRevision() || result.GetAppliedRevision() != command.GetRevision() {
		return "server config apply result revision mismatch"
	}
	if result.GetCorrelationId() != requestID {
		return "server config apply result correlation_id mismatch"
	}
	return ""
}

func (d *deliveryService) HandleRuntimeCommandResult(ctx context.Context, node *streamNode, result *commonproto.RuntimeCommandResult) {
	d.commands.HandleRuntimeCommandResult(ctx, node, result)
}
