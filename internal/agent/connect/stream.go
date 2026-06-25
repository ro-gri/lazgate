package connect

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	agentconfig "laz/internal/agent/config"
	agentstore "laz/internal/agent/store"
	"laz/internal/nodeproto"
	transportstore "laz/internal/nodeproto/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

type StreamClient struct {
	cfg       agentconfig.Config
	version   string
	outgoing  chan *nodeproto.AgentMessage
	mu        sync.Mutex
	pending   map[string]chan *nodeproto.ServerMessage
	transport transportstore.Store
}

type StreamHandler struct {
	RefreshUserAuth func(context.Context, *nodeproto.AuthRefresh) error
	ExecuteCommand  func(context.Context, *nodeproto.RuntimeCommand) *nodeproto.RuntimeCommandResult
}

func NewStream(cfg agentconfig.Config, version string, transport transportstore.Store) *StreamClient {
	if transport == nil {
		transport = transportstore.NopStore{}
	}
	return &StreamClient{
		cfg:       cfg,
		version:   version,
		outgoing:  make(chan *nodeproto.AgentMessage, 128),
		pending:   map[string]chan *nodeproto.ServerMessage{},
		transport: transport,
	}
}

func (c *StreamClient) Run(ctx context.Context, handler StreamHandler) error {
	tlsConfig, err := mtlsConfig(c.cfg.MTLS)
	if err != nil {
		return err
	}
	target := c.cfg.AgentGRPCTarget
	if target == "" {
		target = grpcTargetFromURL(c.cfg.ServerURL)
	}
	conn, err := grpc.DialContext(ctx, target, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := nodeproto.NewAgentControlClient(conn).Connect(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&nodeproto.AgentMessage{
		Type: "hello",
		Hello: &nodeproto.AgentHello{
			NodeId:          c.cfg.NodeID,
			AgentVersion:    c.version,
			ProtocolVersion: "agent-grpc-v1",
		},
	}); err != nil {
		return err
	}
	errc := make(chan error, 2)
	incoming := make(chan *nodeproto.ServerMessage, 128)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-c.outgoing:
				if msg == nil {
					continue
				}
				if err := stream.Send(msg); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-incoming:
				if msg != nil {
					c.handleServerMessage(ctx, handler, msg)
				}
			}
		}
	}()
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errc <- err
				return
			}
			if c.routePending(msg) {
				continue
			}
			select {
			case incoming <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errc:
		return err
	}
}

func (c *StreamClient) AuthManifest(ctx context.Context, since int64, full bool) (*nodeproto.AuthManifestResponse, error) {
	msg := &nodeproto.AgentMessage{
		Type:      "auth_manifest_request",
		RequestId: newRequestID(),
		AuthManifestRequest: &nodeproto.AuthManifestRequest{
			NodeId:                 c.cfg.NodeID,
			SinceManifestStartedAt: since,
			Full:                   full,
		},
	}
	res, err := c.request(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res.GetAuthManifestResponse() == nil {
		return nil, errors.New("missing auth manifest response")
	}
	return res.GetAuthManifestResponse(), nil
}

func (c *StreamClient) AuthSnapshots(ctx context.Context, users []string) (*nodeproto.UserAuthSnapshotResponse, error) {
	msg := &nodeproto.AgentMessage{
		Type:      "user_auth_snapshot_request",
		RequestId: newRequestID(),
		UserAuthSnapshotRequest: &nodeproto.UserAuthSnapshotRequest{
			NodeId: c.cfg.NodeID,
			Users:  users,
		},
	}
	res, err := c.request(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res.GetUserAuthSnapshotResponse() == nil {
		return nil, errors.New("missing auth snapshot response")
	}
	return res.GetUserAuthSnapshotResponse(), nil
}

func (c *StreamClient) SendUsageBatch(ctx context.Context, batch agentstore.UsageBatch) (*nodeproto.UsageAck, error) {
	msg := &nodeproto.AgentMessage{
		Type:      "usage_batch",
		RequestId: newRequestID(),
		UsageBatch: &nodeproto.UsageBatch{
			BatchId: batch.BatchID,
			NodeId:  batch.NodeID,
			FromMs:  batch.FromMS,
			ToMs:    batch.ToMS,
		},
	}
	for _, rec := range batch.Records {
		msg.UsageBatch.Records = append(msg.UsageBatch.Records, &nodeproto.UsageRecord{CredentialId: rec.CredentialID, TxBytes: rec.TXBytes, RxBytes: rec.RXBytes})
	}
	res, err := c.request(ctx, msg)
	if err != nil {
		return nil, err
	}
	if res.GetUsageAck() == nil {
		return nil, errors.New("missing usage ack")
	}
	return res.GetUsageAck(), nil
}

func (c *StreamClient) SendHeartbeat(ctx context.Context, heartbeat *nodeproto.Heartbeat) error {
	return c.send(ctx, &nodeproto.AgentMessage{Type: "heartbeat", Heartbeat: heartbeat})
}

func (c *StreamClient) SendOnline(ctx context.Context, report *nodeproto.OnlineReport) error {
	return c.send(ctx, &nodeproto.AgentMessage{Type: "online", OnlineReport: report})
}

func (c *StreamClient) request(ctx context.Context, msg *nodeproto.AgentMessage) (*nodeproto.ServerMessage, error) {
	if msg.GetRequestId() == "" {
		msg.RequestId = newRequestID()
	}
	ch := make(chan *nodeproto.ServerMessage, 1)
	c.mu.Lock()
	c.pending[msg.GetRequestId()] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, msg.GetRequestId())
		c.mu.Unlock()
	}()
	if err := c.send(ctx, msg); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case res := <-ch:
		return res, nil
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	}
}

func (c *StreamClient) send(ctx context.Context, msg *nodeproto.AgentMessage) error {
	select {
	case c.outgoing <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *StreamClient) routePending(msg *nodeproto.ServerMessage) bool {
	if msg.GetRequestId() == "" {
		return false
	}
	c.mu.Lock()
	ch := c.pending[msg.GetRequestId()]
	c.mu.Unlock()
	if ch == nil {
		return false
	}
	ch <- msg
	return true
}

func (c *StreamClient) handleServerMessage(ctx context.Context, handler StreamHandler, msg *nodeproto.ServerMessage) {
	if msg.GetRequestId() != "" {
		processed, ok, err := c.transport.IsProcessed(ctx, c.cfg.NodeID, msg.GetRequestId())
		if err == nil && ok {
			c.sendProcessedResult(ctx, msg, processed)
			return
		}
	}
	switch msg.GetType() {
	case "auth_refresh":
		refresh := msg.GetAuthRefresh()
		if refresh == nil {
			return
		}
		result := &nodeproto.AuthRefreshResult{NodeId: c.cfg.NodeID, AccountId: refresh.GetAccountId(), Status: "ok"}
		if handler.RefreshUserAuth != nil {
			if err := handler.RefreshUserAuth(ctx, refresh); err != nil {
				result.Status = "error"
				result.Error = err.Error()
			}
		}
		status := transportstore.StatusApplied
		if result.Status != "ok" {
			status = transportstore.StatusFailed
		}
		if msg.GetRequestId() != "" {
			_ = c.transport.RecordProcessed(ctx, c.cfg.NodeID, msg.GetRequestId(), "auth_refresh", status, mustMarshalProto(result), result.GetError())
		}
		_ = c.send(ctx, &nodeproto.AgentMessage{Type: "auth_refresh_result", RequestId: msg.GetRequestId(), AuthRefreshResult: result})
	case "runtime_command":
		if handler.ExecuteCommand == nil {
			return
		}
		if msg.GetRuntimeCommand() == nil {
			return
		}
		result := handler.ExecuteCommand(ctx, msg.GetRuntimeCommand())
		status := transportstore.StatusApplied
		if result.GetStatus() != "ok" && result.GetStatus() != "skipped" {
			status = transportstore.StatusFailed
		}
		if msg.GetRequestId() != "" {
			_ = c.transport.RecordProcessed(ctx, c.cfg.NodeID, msg.GetRequestId(), "runtime_command", status, mustMarshalProto(result), result.GetError())
		}
		_ = c.send(ctx, &nodeproto.AgentMessage{Type: "runtime_command_result", RequestId: msg.GetRequestId(), RuntimeCommandResult: result})
	default:
		return
	}
}

func (c *StreamClient) sendProcessedResult(ctx context.Context, msg *nodeproto.ServerMessage, processed transportstore.ProcessedMessage) {
	switch msg.GetType() {
	case "auth_refresh":
		result := &nodeproto.AuthRefreshResult{}
		_ = unmarshalProto(processed.Result, result)
		if result.Status == "" {
			result = &nodeproto.AuthRefreshResult{NodeId: c.cfg.NodeID, AccountId: msg.GetAuthRefresh().GetAccountId(), Status: string(processed.Status), Error: processed.Error}
		}
		_ = c.send(ctx, &nodeproto.AgentMessage{Type: "auth_refresh_result", RequestId: msg.GetRequestId(), AuthRefreshResult: result})
	case "runtime_command":
		result := &nodeproto.RuntimeCommandResult{}
		_ = unmarshalProto(processed.Result, result)
		if result.Status == "" {
			result = &nodeproto.RuntimeCommandResult{CommandId: msg.GetRuntimeCommand().GetId(), Status: string(processed.Status), Error: processed.Error}
		}
		_ = c.send(ctx, &nodeproto.AgentMessage{Type: "runtime_command_result", RequestId: msg.GetRequestId(), RuntimeCommandResult: result})
	}
}

func grpcTargetFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":9443"
	}
	return host
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}

func mustMarshalProto(msg proto.Message) []byte {
	raw, _ := proto.Marshal(msg)
	return raw
}

func unmarshalProto(raw []byte, msg proto.Message) error {
	if len(raw) == 0 {
		return nil
	}
	return proto.Unmarshal(raw, msg)
}
