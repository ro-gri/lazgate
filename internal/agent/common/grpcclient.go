package common

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"
	"laz/internal/protocol"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

const (
	defaultQueueSize      = 128
	defaultRequestTimeout = 20 * time.Second
)

type ServerConfigCommand interface {
	comparable
	GetNodeId() string
	GetRevision() uint64
}

// ServerMessage is the typed protocol envelope received from LazGate.
type ServerMessage[C ServerConfigCommand] interface {
	GetType() string
	GetRequestId() string
	GetAuthRefresh() *commonproto.AuthRefresh
	GetRuntimeCommand() *commonproto.RuntimeCommand
	GetEditServerConfig() C
}

type ProtocolServerMessage[C ServerConfigCommand, M comparable] interface {
	ServerMessage[C]
	GetAuthManifestResponse() M
	GetUsageAck() *commonproto.UsageAck
}

// Stream is implemented by the generated bidirectional gRPC stream clients.
type Stream[S any] interface {
	Send(*commonproto.AgentMessage) error
	Recv() (S, error)
}

// GRPCDelegate contains all behavior supplied by a concrete agent protocol.
type GRPCDelegate[C ServerConfigCommand, S ServerMessage[C]] interface {
	OpenStream(context.Context, grpc.ClientConnInterface) (Stream[S], error)
	Connected(context.Context)
	ApplyAuthRefresh(context.Context, *commonproto.AuthRefresh) error
	ApplyServerConfig(context.Context, C) *commonproto.ServerConfigApplyResult
	ExecuteRuntimeCommand(context.Context, *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult
}

type GRPCClientConfig struct {
	Target          string
	TLSConfig       func() (*tls.Config, error)
	Identity        Identity
	AgentVersion    string
	ProtocolVersion string
	RequestTimeout  time.Duration
}

type GRPCClient[C ServerConfigCommand, S ServerMessage[C]] struct {
	cfg       GRPCClientConfig
	transport transportstore.Store
	sessionMu sync.RWMutex
	session   *grpcSession[S]
}

type grpcSession[S interface{ GetRequestId() string }] struct {
	ctx       context.Context
	cancel    context.CancelCauseFunc
	outgoing  chan *commonproto.AgentMessage
	pendingMu sync.Mutex
	pending   map[string]chan S
}

func NewGRPCClient[C ServerConfigCommand, S ServerMessage[C]](cfg GRPCClientConfig, transport transportstore.Store) *GRPCClient[C, S] {
	if transport == nil {
		transport = transportstore.NopStore{}
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	return &GRPCClient[C, S]{
		cfg:       cfg,
		transport: transport,
	}
}

type ProtocolClient[C ServerConfigCommand, M comparable, S ProtocolServerMessage[C, M]] struct {
	*GRPCClient[C, S]
}

func NewProtocolClient[C ServerConfigCommand, M comparable, S ProtocolServerMessage[C, M]](
	identity Identity,
	cfg Config,
	agentVersion string,
	protocolVersion string,
	transport transportstore.Store,
) *ProtocolClient[C, M, S] {
	grpcConfig := newGRPCClientConfig(identity, cfg, agentVersion, protocolVersion)
	return &ProtocolClient[C, M, S]{
		GRPCClient: NewGRPCClient[C, S](grpcConfig, transport),
	}
}

func (c *ProtocolClient[C, M, S]) AuthManifest(ctx context.Context, request protocol.Request) (M, error) {
	var zero M
	msg := authManifestRequestMessage(c.cfg.Identity, request)
	response, err := c.Request(ctx, msg)
	if err != nil {
		return zero, err
	}
	manifest := response.GetAuthManifestResponse()
	if manifest == zero {
		return zero, ErrMissingAuthManifestResponse
	}
	return manifest, nil
}

func (c *ProtocolClient[C, M, S]) SendUsageBatch(ctx context.Context, requestID string, batch agentstore.UsageBatch) (*commonproto.UsageAck, error) {
	msg := usageBatchMessage(requestID, batch)
	response, err := c.Request(ctx, msg)
	if err != nil {
		return nil, err
	}
	return validateUsageAck(response.GetUsageAck(), response.GetRequestId(), msg.GetRequestId())
}

func authManifestRequestMessage(identity Identity, request protocol.Request) *commonproto.AgentMessage {
	return &commonproto.AgentMessage{
		Type:      "auth_manifest_request",
		RequestId: uuid.NewString(),
		AuthManifestRequest: &commonproto.AuthManifestRequest{
			NodeId:                 identity.NodeID,
			SinceManifestStartedAt: request.SinceManifestStartedAtMS,
			Full:                   request.Full,
			PageSize:               uint32(request.PageSize),
			FromConnectionId:       request.FromConnectionID,
		},
	}
}

func usageBatchMessage(requestID string, batch agentstore.UsageBatch) *commonproto.AgentMessage {
	msg := &commonproto.AgentMessage{
		Type:       "usage_batch",
		RequestId:  requestID,
		UsageBatch: &commonproto.UsageBatch{NodeId: batch.NodeID, FromMs: batch.FromMS, ToMs: batch.ToMS},
	}
	for _, record := range batch.Records {
		msg.UsageBatch.Records = append(msg.UsageBatch.Records, &commonproto.UsageRecord{ConnectionId: record.ConnectionID, TxBytes: record.TXBytes, RxBytes: record.RXBytes})
	}
	return msg
}

func validateUsageAck(ack *commonproto.UsageAck, responseRequestID, requestID string) (*commonproto.UsageAck, error) {
	if ack == nil {
		return nil, ErrMissingUsageAck
	}
	if responseRequestID != requestID {
		return nil, fmt.Errorf("%w: got %q, want %q", ErrMismatchedUsageAckRequestID, responseRequestID, requestID)
	}
	return ack, nil
}

func (c *GRPCClient[C, S]) Run(ctx context.Context, delegate GRPCDelegate[C, S]) error {
	if c.cfg.TLSConfig == nil {
		return ErrMissingTLSConfig
	}
	tlsConfig, err := c.cfg.TLSConfig()
	if err != nil {
		return err
	}
	if tlsConfig == nil {
		return ErrEmptyTLSConfig
	}
	conn, err := grpc.NewClient(c.cfg.Target, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	sessionCtx, cancelSession := context.WithCancelCause(ctx)
	defer cancelSession(context.Canceled)
	stream, err := delegate.OpenStream(sessionCtx, conn)
	if err != nil {
		return err
	}
	if err := stream.Send(c.hello()); err != nil {
		return err
	}

	session := &grpcSession[S]{
		ctx:      sessionCtx,
		cancel:   cancelSession,
		outgoing: make(chan *commonproto.AgentMessage, defaultQueueSize),
		pending:  make(map[string]chan S),
	}
	c.activateSession(session)
	defer c.deactivateSession(session, ErrDisconnected)

	incoming := make(chan S, defaultQueueSize)
	group, groupCtx := errgroup.WithContext(sessionCtx)
	runLoop := func(run func() error) func() error {
		return func() error {
			err := run()
			if err != nil {
				cancelSession(err)
			}
			return err
		}
	}
	group.Go(runLoop(func() error { return c.sendLoop(groupCtx, session, stream) }))
	group.Go(runLoop(func() error { return c.receiveLoop(groupCtx, session, stream, incoming) }))
	group.Go(runLoop(func() error { return c.handleLoop(groupCtx, delegate, incoming) }))
	delegate.Connected(groupCtx)
	return group.Wait()
}

func (c *GRPCClient[C, S]) Send(ctx context.Context, msg *commonproto.AgentMessage) error {
	session, err := c.activeSession()
	if err != nil {
		return err
	}
	return session.send(ctx, msg)
}

func (s *grpcSession[S]) send(ctx context.Context, msg *commonproto.AgentMessage) error {
	if err := s.connectionError(); err != nil {
		return err
	}
	select {
	case s.outgoing <- msg:
		return s.connectionError()
	case <-s.ctx.Done():
		return s.connectionError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *GRPCClient[C, S]) SendHeartbeat(ctx context.Context, heartbeat *commonproto.Heartbeat) error {
	return c.Send(ctx, &commonproto.AgentMessage{Type: "heartbeat", Heartbeat: heartbeat})
}

func (c *GRPCClient[C, S]) SendOnline(ctx context.Context, report *commonproto.OnlineReport) error {
	return c.Send(ctx, &commonproto.AgentMessage{Type: "online", OnlineReport: report})
}

func (c *GRPCClient[C, S]) Request(ctx context.Context, msg *commonproto.AgentMessage) (S, error) {
	var zero S
	if msg.GetRequestId() == "" {
		return zero, ErrMissingRequestID
	}
	session, err := c.activeSession()
	if err != nil {
		return zero, err
	}
	response := make(chan S, 1)
	session.registerPending(msg.GetRequestId(), response)
	defer session.unregisterPending(msg.GetRequestId())
	if err := session.send(ctx, msg); err != nil {
		return zero, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	defer cancel()
	select {
	case result := <-response:
		return result, nil
	case <-session.ctx.Done():
		return zero, session.connectionError()
	case <-waitCtx.Done():
		return zero, waitCtx.Err()
	}
}

func (s *grpcSession[S]) resolvePending(msg S) bool {
	requestID := msg.GetRequestId()
	if requestID == "" {
		return false
	}
	s.pendingMu.Lock()
	response := s.pending[requestID]
	s.pendingMu.Unlock()
	if response == nil {
		return false
	}
	select {
	case response <- msg:
	default:
	}
	return true
}

func (s *grpcSession[S]) registerPending(requestID string, response chan S) {
	s.pendingMu.Lock()
	s.pending[requestID] = response
	s.pendingMu.Unlock()
}

func (s *grpcSession[S]) unregisterPending(requestID string) {
	s.pendingMu.Lock()
	delete(s.pending, requestID)
	s.pendingMu.Unlock()
}

func (c *GRPCClient[C, S]) HandleIncoming(ctx context.Context, delegate GRPCDelegate[C, S], msg S) {
	requestID := msg.GetRequestId()
	if requestID != "" {
		processed, ok, err := c.transport.IsProcessed(ctx, c.cfg.Identity.NodeID, requestID)
		if err == nil && ok {
			if reply := c.replay(msg, processed); reply != nil {
				_ = c.Send(ctx, reply)
			}
			return
		}
	}
	switch msg.GetType() {
	case "auth_refresh":
		c.handleAuthRefresh(ctx, delegate, msg)
	case "edit_server_config":
		c.handleServerConfig(ctx, delegate, msg)
	case "runtime_command":
		c.handleRuntimeCommand(ctx, delegate, msg)
	}
}

func (c *GRPCClient[C, S]) handleAuthRefresh(ctx context.Context, delegate GRPCDelegate[C, S], msg S) {
	refresh := msg.GetAuthRefresh()
	if refresh == nil {
		return
	}
	result := &commonproto.AuthRefreshResult{
		NodeId:                   c.cfg.Identity.NodeID,
		AccountId:                refresh.GetAccountId(),
		Status:                   "ok",
		AppliedSnapshotVersionMs: refresh.GetSnapshotVersionMs(),
	}
	if err := delegate.ApplyAuthRefresh(ctx, refresh); err != nil {
		result.Status = "error"
		result.Error = err.Error()
	}
	c.complete(ctx, msg.GetRequestId(), "auth_refresh", result.GetStatus() == "ok", result.GetError(), result,
		&commonproto.AgentMessage{Type: "auth_refresh_result", RequestId: msg.GetRequestId(), AuthRefreshResult: result})
}

func (c *GRPCClient[C, S]) handleServerConfig(ctx context.Context, delegate GRPCDelegate[C, S], msg S) {
	command := msg.GetEditServerConfig()
	var zero C
	if command == zero {
		return
	}
	var result *commonproto.ServerConfigApplyResult
	if command.GetNodeId() != c.cfg.Identity.NodeID {
		result = &commonproto.ServerConfigApplyResult{
			NodeId:          c.cfg.Identity.NodeID,
			DesiredRevision: command.GetRevision(),
			CorrelationId:   msg.GetRequestId(),
			Status:          "failed",
			Error:           "edit server config node_id does not match configured node",
		}
	} else {
		result = delegate.ApplyServerConfig(ctx, command)
		if result == nil {
			result = &commonproto.ServerConfigApplyResult{Status: "error", Error: "server config apply returned nil result"}
		}
		if result.NodeId == "" {
			result.NodeId = c.cfg.Identity.NodeID
		}
		if result.DesiredRevision == 0 {
			result.DesiredRevision = command.GetRevision()
		}
		if result.CorrelationId == "" {
			result.CorrelationId = msg.GetRequestId()
		}
	}
	ok := result.GetStatus() == "ok" || result.GetStatus() == "applied"
	c.complete(ctx, msg.GetRequestId(), "edit_server_config", ok, result.GetError(), result,
		&commonproto.AgentMessage{Type: "server_config_apply_result", RequestId: msg.GetRequestId(), ServerConfigApplyResult: result})
}

func (c *GRPCClient[C, S]) handleRuntimeCommand(ctx context.Context, delegate GRPCDelegate[C, S], msg S) {
	command := msg.GetRuntimeCommand()
	if command == nil {
		return
	}
	result := delegate.ExecuteRuntimeCommand(ctx, command)
	if result == nil {
		result = &commonproto.RuntimeCommandResult{CommandId: command.GetId(), Status: "error", Error: "runtime command returned nil result"}
	}
	if result.CommandId == "" {
		result.CommandId = command.GetId()
	}
	ok := result.GetStatus() == "ok" || result.GetStatus() == "skipped"
	c.complete(ctx, msg.GetRequestId(), "runtime_command", ok, result.GetError(), result,
		&commonproto.AgentMessage{Type: "runtime_command_result", RequestId: msg.GetRequestId(), RuntimeCommandResult: result})
}

func (c *GRPCClient[C, S]) complete(ctx context.Context, requestID, typ string, ok bool, errMsg string, result proto.Message, reply *commonproto.AgentMessage) {
	status := transportstore.StatusApplied
	if !ok {
		status = transportstore.StatusFailed
	}
	if requestID != "" {
		raw, _ := proto.Marshal(result)
		_ = c.transport.RecordProcessed(ctx, c.cfg.Identity.NodeID, requestID, typ, status, raw, errMsg)
	}
	_ = c.Send(ctx, reply)
}

func (c *GRPCClient[C, S]) replay(msg S, processed transportstore.ProcessedMessage) *commonproto.AgentMessage {
	switch msg.GetType() {
	case "auth_refresh":
		result := &commonproto.AuthRefreshResult{}
		_ = proto.Unmarshal(processed.Result, result)
		if result.Status == "" {
			result = &commonproto.AuthRefreshResult{NodeId: c.cfg.Identity.NodeID, AccountId: msg.GetAuthRefresh().GetAccountId(), Status: string(processed.Status), Error: processed.Error}
		}
		return &commonproto.AgentMessage{Type: "auth_refresh_result", RequestId: msg.GetRequestId(), AuthRefreshResult: result}
	case "edit_server_config":
		result := &commonproto.ServerConfigApplyResult{}
		_ = proto.Unmarshal(processed.Result, result)
		if result.Status == "" {
			result = &commonproto.ServerConfigApplyResult{NodeId: c.cfg.Identity.NodeID, DesiredRevision: msg.GetEditServerConfig().GetRevision(), CorrelationId: msg.GetRequestId(), Status: string(processed.Status), Error: processed.Error}
		}
		return &commonproto.AgentMessage{Type: "server_config_apply_result", RequestId: msg.GetRequestId(), ServerConfigApplyResult: result}
	case "runtime_command":
		result := &commonproto.RuntimeCommandResult{}
		_ = proto.Unmarshal(processed.Result, result)
		if result.Status == "" {
			result = &commonproto.RuntimeCommandResult{CommandId: msg.GetRuntimeCommand().GetId(), Status: string(processed.Status), Error: processed.Error}
		}
		return &commonproto.AgentMessage{Type: "runtime_command_result", RequestId: msg.GetRequestId(), RuntimeCommandResult: result}
	default:
		return nil
	}
}

func (c *GRPCClient[C, S]) hello() *commonproto.AgentMessage {
	return &commonproto.AgentMessage{
		Type: "hello",
		Hello: &commonproto.AgentHello{
			NodeId:          c.cfg.Identity.NodeID,
			InstallationId:  c.cfg.Identity.InstallationID,
			AgentType:       c.cfg.Identity.AgentType,
			AgentVersion:    c.cfg.AgentVersion,
			ProtocolVersion: c.cfg.ProtocolVersion,
		},
	}
}

func (c *GRPCClient[C, S]) sendLoop(ctx context.Context, session *grpcSession[S], stream Stream[S]) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-session.outgoing:
			if msg == nil {
				continue
			}
			if err := stream.Send(msg); err != nil {
				return err
			}
		}
	}
}

func (c *GRPCClient[C, S]) activeSession() (*grpcSession[S], error) {
	c.sessionMu.RLock()
	session := c.session
	c.sessionMu.RUnlock()
	if session == nil {
		return nil, ErrDisconnected
	}
	if err := session.connectionError(); err != nil {
		return nil, err
	}
	return session, nil
}

func (c *GRPCClient[C, S]) activateSession(session *grpcSession[S]) {
	c.sessionMu.Lock()
	previous := c.session
	c.session = session
	c.sessionMu.Unlock()
	if previous != nil && previous != session {
		previous.cancel(ErrDisconnected)
	}
}

func (c *GRPCClient[C, S]) deactivateSession(session *grpcSession[S], cause error) {
	c.sessionMu.Lock()
	if c.session == session {
		c.session = nil
	}
	c.sessionMu.Unlock()
	session.cancel(cause)
}

func (s *grpcSession[S]) connectionError() error {
	select {
	case <-s.ctx.Done():
		cause := context.Cause(s.ctx)
		if cause == nil || errors.Is(cause, context.Canceled) || errors.Is(cause, ErrDisconnected) {
			return ErrDisconnected
		}
		return fmt.Errorf("%w: %v", ErrDisconnected, cause)
	default:
		return nil
	}
}

func (c *GRPCClient[C, S]) receiveLoop(ctx context.Context, session *grpcSession[S], stream Stream[S], incoming chan<- S) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		if session.resolvePending(msg) {
			continue
		}
		select {
		case incoming <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *GRPCClient[C, S]) handleLoop(ctx context.Context, delegate GRPCDelegate[C, S], incoming <-chan S) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-incoming:
			c.HandleIncoming(ctx, delegate, msg)
		}
	}
}
