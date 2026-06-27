package agentcontrol

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"laz/internal/nodeproto"
	transportstore "laz/internal/nodeproto/transport"
	"laz/internal/server/integrations/nativehy2"
	"laz/internal/server/model"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
)

type Hub struct {
	nodeproto.UnimplementedAgentControlServer
	store     Store
	transport transportstore.Store
	mu        sync.RWMutex
	nodes     map[string]*streamNode
}

type streamNode struct {
	nodeID   string
	send     chan *nodeproto.ServerMessage
	incoming chan *nodeproto.AgentMessage
	pending  map[string]chan *nodeproto.AgentMessage
	mu       sync.Mutex
}

func NewHub(st Store, stores ...transportstore.Store) *Hub {
	var transport transportstore.Store = transportstore.NopStore{}
	if len(stores) > 0 && stores[0] != nil {
		transport = stores[0]
	}
	return &Hub{store: st, transport: transport, nodes: map[string]*streamNode{}}
}

func (h *Hub) Connect(stream nodeproto.AgentControl_ConnectServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetType() != "hello" || first.GetHello().GetNodeId() == "" {
		return errors.New("agent hello required")
	}
	nodeID := first.GetHello().GetNodeId()
	if peerNodeID := nodeIDFromPeer(stream.Context()); peerNodeID != "" && peerNodeID != nodeID {
		return fmt.Errorf("agent certificate node id %s does not match hello node id %s", peerNodeID, nodeID)
	}
	if err := h.verifyPinnedNodeCertificate(stream.Context(), nodeID); err != nil {
		return err
	}
	node := &streamNode{nodeID: nodeID, send: make(chan *nodeproto.ServerMessage, 64), incoming: make(chan *nodeproto.AgentMessage, 128), pending: map[string]chan *nodeproto.AgentMessage{}}
	h.register(nodeID, node)
	defer h.unregister(nodeID, node)

	errc := make(chan error, 2)
	go func() {
		for msg := range node.send {
			if err := stream.Send(msg); err != nil {
				errc <- err
				return
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
			select {
			case node.incoming <- msg:
			case <-stream.Context().Done():
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case msg := <-node.incoming:
				if msg != nil {
					h.handleAgentMessage(node, msg)
				}
			case <-stream.Context().Done():
				return
			}
		}
	}()
	err = <-errc
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (h *Hub) RefreshUserAuth(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (*nodeproto.AuthRefreshResult, error) {
	manifestStartedAt := time.Now().UnixMilli()
	msg := &nodeproto.ServerMessage{
		Type:      "auth_refresh",
		RequestId: newRequestID(),
		AuthRefresh: &nodeproto.AuthRefresh{
			NodeId:            nodeID,
			AccountId:         accountID,
			Snapshots:         h.authSnapshotsForAccount(nodeID, accountID),
			ManifestStartedAt: manifestStartedAt,
			SnapshotVersionMs: snapshotVersionMS,
		},
	}
	res, err := h.request(ctx, nodeID, msg)
	if err != nil {
		return nil, err
	}
	result := res.GetAuthRefreshResult()
	if result.GetStatus() != "ok" {
		if result.GetError() != "" {
			return result, errors.New(result.GetError())
		}
		return result, fmt.Errorf("auth refresh status %s", result.GetStatus())
	}
	return result, nil
}

func (h *Hub) authSnapshotsForAccount(nodeID, accountID string) []*nodeproto.UserAuthSnapshot {
	var users []string
	for _, c := range h.store.ListConnections() {
		if c.NodeID == nodeID && c.AccountID == accountID {
			users = append(users, c.ID)
		}
	}
	sort.Strings(users)
	return h.authSnapshots(&nodeproto.UserAuthSnapshotRequest{NodeId: nodeID, Users: users}).GetSnapshots()
}

func (h *Hub) request(ctx context.Context, nodeID string, msg *nodeproto.ServerMessage) (*nodeproto.AgentMessage, error) {
	node := h.get(nodeID)
	if node == nil {
		return nil, fmt.Errorf("agent node %s is not connected", nodeID)
	}
	ch := make(chan *nodeproto.AgentMessage, 1)
	node.mu.Lock()
	node.pending[msg.RequestId] = ch
	node.mu.Unlock()
	defer func() {
		node.mu.Lock()
		delete(node.pending, msg.RequestId)
		node.mu.Unlock()
	}()
	select {
	case node.send <- msg:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case res := <-ch:
		return res, nil
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	}
}

func mustMarshalProto(msg proto.Message) []byte {
	raw, _ := proto.Marshal(msg)
	return raw
}

func (h *Hub) handleAgentMessage(node *streamNode, msg *nodeproto.AgentMessage) {
	if msg.GetRequestId() != "" {
		node.mu.Lock()
		ch := node.pending[msg.GetRequestId()]
		node.mu.Unlock()
		if ch != nil {
			ch <- msg
			return
		}
	}
	switch msg.GetType() {
	case "auth_manifest_request":
		node.send <- &nodeproto.ServerMessage{Type: "auth_manifest_response", RequestId: msg.GetRequestId(), AuthManifestResponse: h.authManifest(msg.GetAuthManifestRequest())}
	case "user_auth_snapshot_request":
		node.send <- &nodeproto.ServerMessage{Type: "user_auth_snapshot_response", RequestId: msg.GetRequestId(), UserAuthSnapshotResponse: h.authSnapshots(msg.GetUserAuthSnapshotRequest())}
	case "heartbeat":
		_ = h.store.UpsertNodeRuntime(runtimeFromHeartbeat(msg.GetHeartbeat()))
	case "online":
		_ = h.store.UpsertNodeOnlineClients(msg.GetOnlineReport().GetNodeId(), onlineFromReport(msg.GetOnlineReport()))
	case "usage_batch":
		batch, records, err := usageFromProto(msg.GetUsageBatch(), node.nodeID)
		ok := false
		if err == nil {
			ok, err = h.store.CreateUsageBatch(batch, records)
		}
		ack := &nodeproto.UsageAck{BatchId: batch.BatchID, Ok: err == nil || ok}
		status := transportstore.StatusApplied
		errMsg := ""
		if err != nil && !ok {
			status = transportstore.StatusFailed
			errMsg = err.Error()
		}
		if msg.GetRequestId() != "" {
			_ = h.transport.RecordProcessed(context.Background(), node.nodeID, msg.GetRequestId(), "traffic_batch", status, mustMarshalProto(ack), errMsg)
		}
		node.send <- &nodeproto.ServerMessage{
			Type:      "usage_ack",
			RequestId: msg.GetRequestId(),
			UsageAck:  ack,
		}
	}
}

func (h *Hub) authManifest(input *nodeproto.AuthManifestRequest) *nodeproto.AuthManifestResponse {
	if input == nil {
		return &nodeproto.AuthManifestResponse{}
	}
	full := input.GetFull() || input.GetSinceManifestStartedAt() == 0
	since := msTime(input.GetSinceManifestStartedAt()).Add(-time.Millisecond)
	users := map[string]bool{}
	for _, c := range h.store.ListConnections() {
		if c.NodeID != input.GetNodeId() {
			continue
		}
		if full {
			if h.connectionAllowed(c) {
				users[c.ID] = true
			}
			continue
		}
		if !c.UpdatedAt.IsZero() && !c.UpdatedAt.Before(since) {
			users[c.ID] = true
			continue
		}
		account, err := h.store.GetAccount(c.AccountID)
		if err == nil && !account.UpdatedAt.IsZero() && !account.UpdatedAt.Before(since) {
			users[c.ID] = true
			continue
		}
		client, err := h.store.GetClientForAccount(c.AccountID, c.ClientID)
		if err == nil && !client.UpdatedAt.IsZero() && !client.UpdatedAt.Before(since) {
			users[c.ID] = true
		}
	}
	out := make([]string, 0, len(users))
	for user := range users {
		out = append(out, user)
	}
	sort.Strings(out)
	return &nodeproto.AuthManifestResponse{NodeId: input.GetNodeId(), ManifestStartedAt: time.Now().UnixMilli(), Full: full, Users: out}
}

func (h *Hub) authSnapshots(input *nodeproto.UserAuthSnapshotRequest) *nodeproto.UserAuthSnapshotResponse {
	if input == nil {
		return &nodeproto.UserAuthSnapshotResponse{}
	}
	want := map[string]bool{}
	snapshots := make([]*nodeproto.UserAuthSnapshot, 0, len(input.GetUsers()))
	for _, id := range input.GetUsers() {
		want[id] = true
		snapshots = append(snapshots, &nodeproto.UserAuthSnapshot{UserId: id, Op: "delete_from_auth"})
	}
	configByConn := map[string]string{}
	for _, cfg := range h.store.ListIssuedConfigs() {
		if cfg.Status == model.StatusActive && cfg.Kind == model.ConfigHy2URI {
			configByConn[cfg.ConnectionID] = cfg.Config
		}
	}
	for _, c := range h.store.ListConnections() {
		if c.NodeID != input.GetNodeId() || !want[c.ID] || !h.connectionAllowed(c) {
			continue
		}
		password := passwordFromHy2URI(configByConn[c.ID])
		if password == "" {
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			continue
		}
		for i := range snapshots {
			if snapshots[i].UserId == c.ID {
				snapshots[i] = &nodeproto.UserAuthSnapshot{
					UserId:       c.ID,
					Op:           "upsert",
					CredentialId: c.ID,
					Username:     c.RemoteName,
					PasswordHash: string(hash),
				}
				break
			}
		}
	}
	return &nodeproto.UserAuthSnapshotResponse{NodeId: input.GetNodeId(), Snapshots: snapshots}
}

func (h *Hub) connectionAllowed(c model.Connection) bool {
	switch c.Status {
	case model.StatusActive, model.StatusPendingCreate, model.StatusPendingResume:
	default:
		return false
	}
	account, err := h.store.GetAccount(c.AccountID)
	if err != nil || account.Status != model.StatusActive {
		return false
	}
	client, err := h.store.GetClientForAccount(c.AccountID, c.ClientID)
	return err == nil && client.Status == model.StatusActive
}

func (h *Hub) register(nodeID string, node *streamNode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes[nodeID] = node
}

func (h *Hub) unregister(nodeID string, node *streamNode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.nodes[nodeID] == node {
		delete(h.nodes, nodeID)
		close(node.send)
	}
}

func (h *Hub) get(nodeID string) *streamNode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.nodes[nodeID]
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}

func nodeIDFromPeer(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return ""
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return ""
	}
	return tlsInfo.State.PeerCertificates[0].Subject.CommonName
}

func (h *Hub) verifyPinnedNodeCertificate(ctx context.Context, nodeID string) error {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return errors.New("agent peer is missing")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return errors.New("agent client certificate is missing")
	}
	node, err := h.store.GetNode(nodeID)
	if err != nil {
		return err
	}
	meta := nativehy2.ParseMetadata(node.APIKey)
	if strings.TrimSpace(meta.NodeCertPEM) == "" {
		return errors.New("node does not have pinned agent certificate")
	}
	block, _ := pem.Decode([]byte(meta.NodeCertPEM))
	if block == nil {
		return errors.New("invalid pinned agent certificate")
	}
	pinned, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	if !pinned.Equal(tlsInfo.State.PeerCertificates[0]) {
		return errors.New("agent certificate does not match pinned node certificate")
	}
	return nil
}
