package common

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"
	"laz/internal/protocol"

	"google.golang.org/grpc"
)

type testServerMessage struct {
	typ       string
	requestID string
	refresh   *commonproto.AuthRefresh
	runtime   *commonproto.RuntimeCommand
	config    *testServerConfig
	manifest  *testManifest
	usageAck  *commonproto.UsageAck
}

func (m *testServerMessage) GetType() string      { return m.typ }
func (m *testServerMessage) GetRequestId() string { return m.requestID }
func (m *testServerMessage) GetAuthRefresh() *commonproto.AuthRefresh {
	return m.refresh
}
func (m *testServerMessage) GetRuntimeCommand() *commonproto.RuntimeCommand {
	return m.runtime
}
func (m *testServerMessage) GetEditServerConfig() *testServerConfig { return m.config }
func (m *testServerMessage) GetAuthManifestResponse() *testManifest { return m.manifest }
func (m *testServerMessage) GetUsageAck() *commonproto.UsageAck     { return m.usageAck }

type testManifest struct {
	value string
}

type testServerConfig struct {
	nodeID   string
	revision uint64
}

func (c *testServerConfig) GetNodeId() string   { return c.nodeID }
func (c *testServerConfig) GetRevision() uint64 { return c.revision }

type testDelegate struct {
	runtimeExecutions int
	refreshExecutions int
	configExecutions  int
}

func activateTestSession[C ServerConfigCommand, S ServerMessage[C]](t *testing.T, client *GRPCClient[C, S]) *grpcSession[S] {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	session := &grpcSession[S]{ctx: ctx, cancel: cancel, outgoing: make(chan *commonproto.AgentMessage, defaultQueueSize), pending: make(map[string]chan S)}
	client.activateSession(session)
	t.Cleanup(func() { client.deactivateSession(session, ErrDisconnected) })
	return session
}

func (*testDelegate) OpenStream(context.Context, grpc.ClientConnInterface) (Stream[*testServerMessage], error) {
	return nil, nil
}
func (*testDelegate) Connected(context.Context) {}

func (d *testDelegate) ApplyAuthRefresh(context.Context, *commonproto.AuthRefresh) error {
	d.refreshExecutions++
	return nil
}
func (d *testDelegate) ApplyServerConfig(context.Context, *testServerConfig) *commonproto.ServerConfigApplyResult {
	d.configExecutions++
	return &commonproto.ServerConfigApplyResult{Status: "applied"}
}
func (d *testDelegate) ExecuteRuntimeCommand(_ context.Context, command *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult {
	d.runtimeExecutions++
	return &commonproto.RuntimeCommandResult{CommandId: command.GetId(), Status: "ok", Message: "done"}
}

func TestRequestResolvesTypedResponse(t *testing.T) {
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{RequestTimeout: time.Second}, nil)
	session := activateTestSession(t, client)
	done := make(chan *testServerMessage, 1)
	go func() {
		response, _ := client.Request(context.Background(), &commonproto.AgentMessage{Type: "request", RequestId: "request-1"})
		done <- response
	}()

	response := &testServerMessage{typ: "response", requestID: "request-1"}
	deadline := time.Now().Add(time.Second)
	for !session.resolvePending(response) {
		if time.Now().After(deadline) {
			t.Fatal("request was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	if got := <-done; got != response {
		t.Fatalf("response = %p, want %p", got, response)
	}
}

func TestProtocolClientRequestsAuthManifest(t *testing.T) {
	client := NewProtocolClient[*testServerConfig, *testManifest, *testServerMessage](Identity{NodeID: "node-1"}, Config{}, "test", "test-v1", nil)
	session := activateTestSession(t, client.GRPCClient)
	want := &testManifest{value: "manifest"}
	done := make(chan *testManifest, 1)
	go func() {
		manifest, _ := client.AuthManifest(context.Background(), protocol.Request{
			Full:                     true,
			SinceManifestStartedAtMS: 12,
			PageSize:                 100,
			FromConnectionID:         "connection-1",
		})
		done <- manifest
	}()

	request := <-session.outgoing
	manifestRequest := request.GetAuthManifestRequest()
	if request.GetType() != "auth_manifest_request" || manifestRequest.GetNodeId() != "node-1" || !manifestRequest.GetFull() || manifestRequest.GetSinceManifestStartedAt() != 12 || manifestRequest.GetPageSize() != 100 || manifestRequest.GetFromConnectionId() != "connection-1" {
		t.Fatalf("unexpected auth manifest request: %+v", request)
	}
	session.resolvePending(&testServerMessage{requestID: request.GetRequestId(), manifest: want})
	if got := <-done; got != want {
		t.Fatalf("manifest = %p, want %p", got, want)
	}
}

func TestProtocolClientSendsUsageBatchAndValidatesAck(t *testing.T) {
	client := NewProtocolClient[*testServerConfig, *testManifest, *testServerMessage](Identity{NodeID: "node-1"}, Config{}, "test", "test-v1", nil)
	session := activateTestSession(t, client.GRPCClient)
	done := make(chan error, 1)
	go func() {
		_, err := client.SendUsageBatch(context.Background(), "request-1", agentstore.UsageBatch{
			RequestID: "request-1",
			NodeID:    "node-1",
			FromMS:    10,
			ToMS:      20,
			Records:   []agentstore.UsageRecord{{ConnectionID: "connection-1", TXBytes: 30, RXBytes: 40}},
		})
		done <- err
	}()

	request := <-session.outgoing
	batch := request.GetUsageBatch()
	if request.GetType() != "usage_batch" || request.GetRequestId() != "request-1" || len(batch.GetRecords()) != 1 || batch.GetRecords()[0].GetConnectionId() != "connection-1" {
		t.Fatalf("unexpected usage batch: %+v", request)
	}
	session.resolvePending(&testServerMessage{
		requestID: request.GetRequestId(),
		usageAck:  &commonproto.UsageAck{Ok: true},
	})
	if err := <-done; err != nil {
		t.Fatalf("SendUsageBatch() error = %v", err)
	}
}

func TestHandleIncomingRecordsAuthRefresh(t *testing.T) {
	store := newTestStore()
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{Identity: Identity{NodeID: "node-1"}}, store)
	activateTestSession(t, client)
	delegate := &testDelegate{}
	refresh := &commonproto.AuthRefresh{NodeId: "node-1", AccountId: "account-1", SnapshotVersionMs: 42}

	client.HandleIncoming(context.Background(), delegate, &testServerMessage{typ: "auth_refresh", requestID: "request-1", refresh: refresh})

	if delegate.refreshExecutions != 1 {
		t.Fatalf("refresh executions = %d, want 1", delegate.refreshExecutions)
	}
	processed, ok, err := store.IsProcessed(context.Background(), "node-1", "request-1")
	if err != nil || !ok || processed.Status != transportstore.StatusApplied {
		t.Fatalf("processed=%+v ok=%v err=%v", processed, ok, err)
	}
}

func TestHandleIncomingRejectsServerConfigForAnotherNode(t *testing.T) {
	store := newTestStore()
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{Identity: Identity{NodeID: "node-1"}}, store)
	activateTestSession(t, client)
	delegate := &testDelegate{}

	client.HandleIncoming(context.Background(), delegate, &testServerMessage{
		typ:       "edit_server_config",
		requestID: "request-1",
		config:    &testServerConfig{nodeID: "node-2", revision: 7},
	})

	if delegate.configExecutions != 0 {
		t.Fatalf("config executions = %d, want 0", delegate.configExecutions)
	}
	processed, ok, err := store.IsProcessed(context.Background(), "node-1", "request-1")
	if err != nil || !ok || processed.Status != transportstore.StatusFailed {
		t.Fatalf("processed=%+v ok=%v err=%v", processed, ok, err)
	}
}

func TestHandleIncomingRecordsAndReplaysProcessedMessage(t *testing.T) {
	store := newTestStore()
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{Identity: Identity{NodeID: "node-1"}}, store)
	session := activateTestSession(t, client)
	delegate := &testDelegate{}
	msg := &testServerMessage{typ: "runtime_command", requestID: "request-1", runtime: &commonproto.RuntimeCommand{Id: "command-1"}}

	client.HandleIncoming(context.Background(), delegate, msg)
	client.HandleIncoming(context.Background(), delegate, msg)

	if delegate.runtimeExecutions != 1 {
		t.Fatalf("runtime executions=%d, want 1", delegate.runtimeExecutions)
	}
	processed, ok, err := store.IsProcessed(context.Background(), "node-1", "request-1")
	if err != nil || !ok || processed.Type != "runtime_command" || processed.Status != transportstore.StatusApplied {
		t.Fatalf("processed=%+v ok=%v err=%v", processed, ok, err)
	}
	first := <-session.outgoing
	second := <-session.outgoing
	if first.GetRuntimeCommandResult().GetMessage() != "done" || second.GetRuntimeCommandResult().GetMessage() != "done" {
		t.Fatalf("initial=%+v replay=%+v", first, second)
	}
}

func TestSendRejectsDisconnectedClient(t *testing.T) {
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{}, nil)
	if err := client.Send(context.Background(), &commonproto.AgentMessage{Type: "heartbeat"}); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Send() error = %v, want %v", err, ErrDisconnected)
	}
}

func TestDisconnectFailsPendingRequestAndDropsSessionQueue(t *testing.T) {
	client := NewGRPCClient[*testServerConfig, *testServerMessage](GRPCClientConfig{RequestTimeout: time.Hour}, nil)
	first := activateTestSession(t, client)
	result := make(chan error, 1)
	go func() {
		_, err := client.Request(context.Background(), &commonproto.AgentMessage{Type: "request", RequestId: "request-1"})
		result <- err
	}()

	<-first.outgoing
	if err := first.send(context.Background(), &commonproto.AgentMessage{Type: "heartbeat"}); err != nil {
		t.Fatalf("queue stale message: %v", err)
	}
	client.deactivateSession(first, ErrDisconnected)

	select {
	case err := <-result:
		if !errors.Is(err, ErrDisconnected) {
			t.Fatalf("Request() error = %v, want %v", err, ErrDisconnected)
		}
	case <-time.After(time.Second):
		t.Fatal("pending request was not interrupted by disconnect")
	}

	second := activateTestSession(t, client)
	if len(second.outgoing) != 0 {
		t.Fatalf("new session inherited %d queued messages", len(second.outgoing))
	}
	if len(second.pending) != 0 {
		t.Fatalf("new session inherited %d pending requests", len(second.pending))
	}
}

type testStore struct {
	transportstore.NopStore
	mu        sync.Mutex
	processed map[string]transportstore.ProcessedMessage
}

func newTestStore() *testStore {
	return &testStore{processed: make(map[string]transportstore.ProcessedMessage)}
}

func (s *testStore) IsProcessed(_ context.Context, actorID, messageID string) (transportstore.ProcessedMessage, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	message, ok := s.processed[actorID+":"+messageID]
	return message, ok, nil
}

func (s *testStore) RecordProcessed(_ context.Context, actorID, messageID, typ string, status transportstore.Status, result []byte, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processed[actorID+":"+messageID] = transportstore.ProcessedMessage{ActorID: actorID, MessageID: messageID, Type: typ, Status: status, Result: result, Error: errMsg}
	return nil
}
