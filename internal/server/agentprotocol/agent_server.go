package agentprotocol

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type AgentServerHandler[ManifestResp any] interface {
	BuildAuthManifestResponse(context.Context, *commonproto.AuthManifestRequest) ManifestResp
}

type AgentServerTelemetry[Resp any] interface {
	HandleHeartbeat(context.Context, *StreamNode[*commonproto.AgentMessage, *Resp], *commonproto.Heartbeat)
	HandleOnline(context.Context, *StreamNode[*commonproto.AgentMessage, *Resp], *commonproto.OnlineReport)
	HandleUsageBatch(context.Context, *StreamNode[*commonproto.AgentMessage, *Resp], *commonproto.AgentMessage) (*commonproto.UsageAck, bool)
}

type AgentServerRuntimeResultHandler[Resp any] interface {
	HandleRuntimeCommandResult(context.Context, *StreamNode[*commonproto.AgentMessage, *Resp], *commonproto.RuntimeCommandResult)
}

type ServerMessageParts[ManifestResp any] struct {
	RequestID            string
	Type                 string
	AuthRefresh          *commonproto.AuthRefresh
	UsageAck             *commonproto.UsageAck
	RuntimeCommand       *commonproto.RuntimeCommand
	AuthManifestResponse ManifestResp
}

type AgentServerMessages[Resp any, ManifestResp any] interface {
	BuildServerMessage(ServerMessageParts[ManifestResp]) *Resp
	BuildInvalidAuthManifestResponse(nodeID string, message string) ManifestResp
}

type AgentServerNodeTasker[Resp any] interface {
	RunNodeTasks(context.Context, *StreamNode[*commonproto.AgentMessage, *Resp]) error
}

type AgentServer[Resp any, ManifestResp any] struct {
	streams           *StreamHub[*commonproto.AgentMessage, *Resp]
	manifest          AgentServerHandler[ManifestResp]
	hooks             any
	messages          AgentServerMessages[Resp, ManifestResp]
	telemetry         AgentServerTelemetry[Resp]
	expectedAgentType string
	lookup            InstallationLookup
}

func NewAgentServer[Resp any, ManifestResp any](manifest AgentServerHandler[ManifestResp], hooks any, messages AgentServerMessages[Resp, ManifestResp], telemetry AgentServerTelemetry[Resp], expectedAgentType string, lookup InstallationLookup) *AgentServer[Resp, ManifestResp] {
	return &AgentServer[Resp, ManifestResp]{
		streams:           NewStreamHub[*commonproto.AgentMessage, *Resp](),
		manifest:          manifest,
		hooks:             hooks,
		messages:          messages,
		telemetry:         telemetry,
		expectedAgentType: expectedAgentType,
		lookup:            lookup,
	}
}

func (s *AgentServer[Resp, ManifestResp]) Connect(stream grpc.BidiStreamingServer[commonproto.AgentMessage, Resp]) error {
	return s.streams.Connect(stream, s)
}

func (s *AgentServer[Resp, ManifestResp]) Request(ctx context.Context, nodeID string, requestID string, msg *Resp) (*commonproto.AgentMessage, error) {
	return s.streams.Request(ctx, nodeID, requestID, msg)
}

func (s *AgentServer[Resp, ManifestResp]) RefreshAuth(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (*commonproto.AuthRefreshResult, error) {
	requestID := uuid.NewString()
	refresh := &commonproto.AuthRefresh{
		NodeId:            nodeID,
		AccountId:         accountID,
		ManifestStartedAt: time.Now().UnixMilli(),
		SnapshotVersionMs: snapshotVersionMS,
	}
	res, err := s.Request(ctx, nodeID, requestID, s.messages.BuildServerMessage(ServerMessageParts[ManifestResp]{
		RequestID:   requestID,
		Type:        "auth_refresh",
		AuthRefresh: refresh,
	}))
	if errors.Is(err, ErrAgentNodeNotConnected) {
		return nil, fmt.Errorf("agent node %s is not connected", nodeID)
	}
	if err != nil {
		return nil, err
	}
	result := res.GetAuthRefreshResult()
	if result == nil {
		return nil, errors.New("missing auth refresh result")
	}
	if result.GetStatus() == "ok" || result.GetStatus() == "applied" {
		return result, nil
	}
	if result.GetError() != "" {
		return result, errors.New(result.GetError())
	}
	return result, fmt.Errorf("auth refresh status %s", result.GetStatus())
}

func (s *AgentServer[Resp, ManifestResp]) RefreshAuthApplied(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (int64, error) {
	result, err := s.RefreshAuth(ctx, nodeID, accountID, snapshotVersionMS)
	if err != nil {
		return 0, err
	}
	applied := result.GetAppliedSnapshotVersionMs()
	if applied == 0 {
		applied = snapshotVersionMS
	}
	return applied, nil
}

func (s *AgentServer[Resp, ManifestResp]) HandleIncoming(ctx context.Context, node *StreamNode[*commonproto.AgentMessage, *Resp], msg *commonproto.AgentMessage) {
	s.streams.HandleIncoming(ctx, s, node, msg)
}

func (s *AgentServer[Resp, ManifestResp]) Register(nodeID string, node *StreamNode[*commonproto.AgentMessage, *Resp]) {
	s.streams.Register(nodeID, node)
}

func (s *AgentServer[Resp, ManifestResp]) Unregister(nodeID string, node *StreamNode[*commonproto.AgentMessage, *Resp]) {
	s.streams.Unregister(nodeID, node)
}

func (s *AgentServer[Resp, ManifestResp]) Get(nodeID string) *StreamNode[*commonproto.AgentMessage, *Resp] {
	return s.streams.Get(nodeID)
}

func (s *AgentServer[Resp, ManifestResp]) ParseHello(msg *commonproto.AgentMessage) (HelloIdentity, bool) {
	if msg.GetType() != "hello" {
		return HelloIdentity{}, false
	}
	hello := msg.GetHello()
	return HelloIdentity{
		NodeID:         hello.GetNodeId(),
		InstallationID: hello.GetInstallationId(),
		AgentType:      hello.GetAgentType(),
	}, true
}

func (s *AgentServer[Resp, ManifestResp]) AuthenticateInstallation(ctx context.Context, hello HelloIdentity) (InstallationIdentity, error) {
	return AuthenticateMTLSInstallation(ctx, hello, s.expectedAgentType, s.lookup)
}

func (s *AgentServer[Resp, ManifestResp]) RequestID(msg *commonproto.AgentMessage) string {
	return msg.GetRequestId()
}

func (s *AgentServer[Resp, ManifestResp]) HandleMessage(ctx context.Context, node *StreamNode[*commonproto.AgentMessage, *Resp], msg *commonproto.AgentMessage) {
	switch msg.GetType() {
	case "auth_manifest_request":
		response, ok := s.buildAuthManifestResponse(ctx, node, msg)
		if ok {
			node.Send(response)
		}
	case "heartbeat":
		if heartbeat := msg.GetHeartbeat(); heartbeat != nil && s.telemetry != nil {
			s.telemetry.HandleHeartbeat(ctx, node, heartbeat)
		}
	case "online":
		if report := msg.GetOnlineReport(); report != nil && s.telemetry != nil {
			s.telemetry.HandleOnline(ctx, node, report)
		}
	case "usage_batch":
		if s.telemetry != nil {
			ack, handled := s.telemetry.HandleUsageBatch(ctx, node, msg)
			if handled {
				node.Send(s.messages.BuildServerMessage(ServerMessageParts[ManifestResp]{
					RequestID: msg.GetRequestId(),
					Type:      "usage_ack",
					UsageAck:  ack,
				}))
			}
		}
	case "runtime_command_result":
		if handler, ok := s.hooks.(AgentServerRuntimeResultHandler[Resp]); ok {
			handler.HandleRuntimeCommandResult(ctx, node, msg.GetRuntimeCommandResult())
		}
	}
}

func (s *AgentServer[Resp, ManifestResp]) buildAuthManifestResponse(ctx context.Context, node *StreamNode[*commonproto.AgentMessage, *Resp], msg *commonproto.AgentMessage) (*Resp, bool) {
	request := msg.GetAuthManifestRequest()
	if request == nil || request.GetNodeId() != node.NodeID() {
		return s.messages.BuildServerMessage(ServerMessageParts[ManifestResp]{
			RequestID:            msg.GetRequestId(),
			Type:                 "auth_manifest_response",
			AuthManifestResponse: s.messages.BuildInvalidAuthManifestResponse(node.NodeID(), "invalid auth manifest request"),
		}), true
	}
	return s.messages.BuildServerMessage(ServerMessageParts[ManifestResp]{
		RequestID:            msg.GetRequestId(),
		Type:                 "auth_manifest_response",
		AuthManifestResponse: s.manifest.BuildAuthManifestResponse(ctx, request),
	}), true
}

func (s *AgentServer[Resp, ManifestResp]) RunNodeTasks(ctx context.Context, node *StreamNode[*commonproto.AgentMessage, *Resp]) error {
	tasker, ok := s.hooks.(AgentServerNodeTasker[Resp])
	if !ok {
		return nil
	}
	return tasker.RunNodeTasks(ctx, node)
}
