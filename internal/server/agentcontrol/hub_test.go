package agentcontrol

import (
	"testing"
	"time"

	"laz/internal/nodeproto"
	"laz/internal/server/model"
	store "laz/internal/server/storage"

	"golang.org/x/crypto/bcrypt"
)

func TestHubUsageBatchDeduplicates(t *testing.T) {
	st := newAgentTestStore(t)
	node, err := st.CreateNode(model.Node{Name: "node", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(st)
	batch := &nodeproto.UsageBatch{
		BatchId: "batch_1",
		NodeId:  node.ID,
		FromMs:  1,
		ToMs:    2,
		Records: []*nodeproto.UsageRecord{{CredentialId: "cred", TxBytes: 10, RxBytes: 20}},
	}
	nodeStream := &streamNode{nodeID: node.ID, send: make(chan *nodeproto.ServerMessage, 2), pending: map[string]chan *nodeproto.AgentMessage{}}
	hub.handleAgentMessage(nodeStream, &nodeproto.AgentMessage{Type: "usage_batch", RequestId: "req1", UsageBatch: batch})
	hub.handleAgentMessage(nodeStream, &nodeproto.AgentMessage{Type: "usage_batch", RequestId: "req2", UsageBatch: batch})
	if got := (<-nodeStream.send).GetUsageAck(); !got.GetOk() || got.GetBatchId() != "batch_1" {
		t.Fatalf("unexpected ack %#v", got)
	}
	if got := (<-nodeStream.send).GetUsageAck(); !got.GetOk() || got.GetBatchId() != "batch_1" {
		t.Fatalf("unexpected duplicate ack %#v", got)
	}
	records := st.ListUsageRecords()
	if len(records) != 1 || records[0].TotalBytes != 30 {
		t.Fatalf("unexpected usage records: %+v", records)
	}
}

func TestHubUsageBatchRejectsWrongNodeAndInvalidValues(t *testing.T) {
	st := newAgentTestStore(t)
	node, err := st.CreateNode(model.Node{Name: "node", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	nodeStream := &streamNode{nodeID: node.ID, send: make(chan *nodeproto.ServerMessage, 2), pending: map[string]chan *nodeproto.AgentMessage{}}
	hub := NewHub(st)
	hub.handleAgentMessage(nodeStream, &nodeproto.AgentMessage{
		Type:      "usage_batch",
		RequestId: "wrong-node",
		UsageBatch: &nodeproto.UsageBatch{
			BatchId: "batch_wrong",
			NodeId:  "other",
			Records: []*nodeproto.UsageRecord{{CredentialId: "cred", TxBytes: 1, RxBytes: 1}},
		},
	})
	if got := (<-nodeStream.send).GetUsageAck(); got.GetOk() {
		t.Fatalf("wrong-node batch was acked: %#v", got)
	}
	hub.handleAgentMessage(nodeStream, &nodeproto.AgentMessage{
		Type:      "usage_batch",
		RequestId: "negative",
		UsageBatch: &nodeproto.UsageBatch{
			BatchId: "batch_negative",
			NodeId:  node.ID,
			Records: []*nodeproto.UsageRecord{{CredentialId: "cred", TxBytes: -1, RxBytes: 1}},
		},
	})
	if got := (<-nodeStream.send).GetUsageAck(); got.GetOk() {
		t.Fatalf("negative batch was acked: %#v", got)
	}
	if records := st.ListUsageRecords(); len(records) != 0 {
		t.Fatalf("invalid batches were persisted: %+v", records)
	}
}

func TestHubAuthSnapshotUsesPasswordHash(t *testing.T) {
	st := newAgentTestStore(t)
	account, err := st.CreateAccount(model.Account{Username: "alice", DisplayName: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "phone", Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "node", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2, RemoteName: "alice_phone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Slug: "hy2", Status: model.StatusActive, Config: "hy2://alice_phone%3Asecret@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	res := NewHub(st).authSnapshots(&nodeproto.UserAuthSnapshotRequest{NodeId: node.ID, Users: []string{connection.ID}})
	if len(res.GetSnapshots()) != 1 {
		t.Fatalf("unexpected snapshots %#v", res.GetSnapshots())
	}
	snap := res.GetSnapshots()[0]
	if snap.GetOp() != "upsert" || snap.GetPasswordHash() == "secret" {
		t.Fatalf("expected upsert with hash, got %#v", snap)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(snap.GetPasswordHash()), []byte("secret")); err != nil {
		t.Fatalf("hash does not verify password: %v", err)
	}
}

func TestPasswordFromHy2URI(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "encoded username password pair", raw: "hy2://alice_phone%3Asecret@example.com:443", want: "secret"},
		{name: "standard userinfo password", raw: "hy2://alice_phone:secret@example.com:443", want: "secret"},
		{name: "username only", raw: "hy2://secret@example.com:443", want: "secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := passwordFromHy2URI(tt.raw); got != tt.want {
				t.Fatalf("passwordFromHy2URI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHubAuthManifestFullAndIncremental(t *testing.T) {
	st := newAgentTestStore(t)
	node, err := st.CreateNode(model.Node{Name: "node", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	active := createNativeConnection(t, st, node.ID, "active")
	held := createNativeConnection(t, st, node.ID, "held")
	if _, err := st.UpdateAccountStatus(held.account.ID, model.StatusHeld); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(st)
	full := hub.authManifest(&nodeproto.AuthManifestRequest{NodeId: node.ID, Full: true})
	if got := full.GetUsers(); len(got) != 1 || got[0] != active.connection.ID {
		t.Fatalf("full manifest should include only active account, got %#v", got)
	}
	since := time.Now()
	time.Sleep(time.Millisecond)
	if _, err := st.UpdateAccountStatus(active.account.ID, model.StatusHeld); err != nil {
		t.Fatal(err)
	}
	incremental := hub.authManifest(&nodeproto.AuthManifestRequest{NodeId: node.ID, SinceManifestStartedAt: since.UnixMilli()})
	if got := incremental.GetUsers(); !containsString(got, active.connection.ID) {
		t.Fatalf("incremental manifest should include changed account, got %#v", got)
	}
	snap := hub.authSnapshots(&nodeproto.UserAuthSnapshotRequest{NodeId: node.ID, Users: []string{active.connection.ID}})
	if len(snap.GetSnapshots()) != 1 || snap.GetSnapshots()[0].GetOp() != "delete_from_auth" {
		t.Fatalf("held account snapshot should delete, got %#v", snap.GetSnapshots())
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

type nativeConnectionFixture struct {
	account    model.Account
	client     model.Client
	connection model.Connection
}

func createNativeConnection(t *testing.T, st *store.SQLStore, nodeID, name string) nativeConnectionFixture {
	t.Helper()
	account, err := st.CreateAccount(model.Account{Username: name, DisplayName: name})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "phone", Name: "Phone"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: nodeID, Protocol: model.ProtocolHysteria2, RemoteName: name + "_phone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Slug: "hy2", Status: model.StatusActive, Config: "hy2://" + name + "_phone:secret@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	return nativeConnectionFixture{account: account, client: client, connection: connection}
}

func newAgentTestStore(t *testing.T) *store.SQLStore {
	t.Helper()
	st, err := store.OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
