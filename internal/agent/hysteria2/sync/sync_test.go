package sync

import (
	"context"
	"errors"
	"strconv"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	agentcommon "laz/internal/agent/common"
	agentstore "laz/internal/agent/common/store"
	"laz/internal/protocol"
)

type memoryStore struct {
	state     map[string]string
	users     map[string]agentstore.AuthUser
	failApply bool
}

func (m *memoryStore) GetState(_ context.Context, key string) (string, error) {
	v, ok := m.state[key]
	if !ok {
		return "", agentstore.ErrNotFound
	}
	return v, nil
}
func (m *memoryStore) SetState(_ context.Context, key, value string) error {
	m.state[key] = value
	return nil
}
func (m *memoryStore) UpsertAuthUser(_ context.Context, u agentstore.AuthUser) error {
	if m.failApply {
		return errors.New("apply failed")
	}
	m.users[u.ConnectionID] = u
	return nil
}
func (m *memoryStore) DeleteAuthUser(_ context.Context, connectionID string) error {
	if m.failApply {
		return errors.New("apply failed")
	}
	delete(m.users, connectionID)
	return nil
}
func (m *memoryStore) ListAuthUsers(context.Context) ([]agentstore.AuthUser, error) {
	out := make([]agentstore.AuthUser, 0, len(m.users))
	for _, user := range m.users {
		out = append(out, user)
	}
	return out, nil
}
func (m *memoryStore) ApplyFullAuthSnapshot(_ context.Context, keep map[string]agentstore.AuthUser, key string, cursorMS int64) error {
	if m.failApply {
		return errors.New("apply failed")
	}
	m.users = keep
	m.state[key] = strconv.FormatInt(cursorMS, 10)
	return nil
}

type fakeClient struct {
	manifest *hysteriaproto.AuthManifestResponse
}

type fakeKicker struct {
	credentials []string
}

func (f *fakeKicker) Kick(_ context.Context, connectionID string) error {
	f.credentials = append(f.credentials, connectionID)
	return nil
}

func (f fakeClient) AuthManifest(context.Context, protocol.Request) (*hysteriaproto.AuthManifestResponse, error) {
	return f.manifest, nil
}

func TestManifestDeleteKicksRemovedCredential(t *testing.T) {
	connectionID := "22222222-2222-4222-8222-222222222222"
	st := &memoryStore{
		state: map[string]string{},
		users: map[string]agentstore.AuthUser{
			connectionID: {ConnectionID: connectionID, NativeAuthID: connectionID},
		},
	}
	client := fakeClient{
		manifest: &hysteriaproto.AuthManifestResponse{ManifestStartedAt: 200, Changes: []*hysteriaproto.CredentialChange{
			{ConnectionId: connectionID, Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE},
		}},
	}
	kicker := &fakeKicker{}
	err := New(st, client, kicker).RunOnce(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(kicker.credentials) != 1 || kicker.credentials[0] != connectionID {
		t.Fatalf("expected %s kick, got %#v", connectionID, kicker.credentials)
	}
}

func TestSyncUpsertDeleteAndCursor(t *testing.T) {
	connectionID := "22222222-2222-4222-8222-222222222222"
	deletedConnectionID := "33333333-3333-4333-8333-333333333333"
	st := &memoryStore{state: map[string]string{agentcommon.AuthSyncCursorKey: "100"}, users: map[string]agentstore.AuthUser{deletedConnectionID: {ConnectionID: deletedConnectionID, NativeAuthID: deletedConnectionID}}}
	client := fakeClient{
		manifest: &hysteriaproto.AuthManifestResponse{NodeId: "44444444-4444-4444-8444-444444444444", ManifestStartedAt: 200, Changes: []*hysteriaproto.CredentialChange{
			{ConnectionId: connectionID, Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT, Payload: &hysteriaproto.Credential{PasswordHash: "hash"}},
			{ConnectionId: deletedConnectionID, Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE},
		}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.users[connectionID]; !ok {
		t.Fatalf("expected %s upsert", connectionID)
	}
	if _, ok := st.users[deletedConnectionID]; ok {
		t.Fatalf("expected %s delete", deletedConnectionID)
	}
	if st.state[agentcommon.AuthSyncCursorKey] != "200" {
		t.Fatalf("cursor did not advance: %v", st.state)
	}
}

func TestSyncCursorDoesNotAdvanceAfterApplyFailure(t *testing.T) {
	connectionID := "11111111-1111-4111-8111-111111111111"
	st := &memoryStore{state: map[string]string{agentcommon.AuthSyncCursorKey: "100"}, users: map[string]agentstore.AuthUser{}, failApply: true}
	client := fakeClient{
		manifest: &hysteriaproto.AuthManifestResponse{ManifestStartedAt: 200, Changes: []*hysteriaproto.CredentialChange{{ConnectionId: connectionID, Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT, Payload: &hysteriaproto.Credential{PasswordHash: "hash"}}}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err == nil {
		t.Fatal("expected apply failure")
	}
	if st.state[agentcommon.AuthSyncCursorKey] != "100" {
		t.Fatalf("cursor advanced after failure: %v", st.state)
	}
}

func TestFullResyncReplacesLocalAllowList(t *testing.T) {
	oldConnectionID := "55555555-5555-4555-8555-555555555555"
	newConnectionID := "77777777-7777-4777-8777-777777777777"
	st := &memoryStore{
		state: map[string]string{agentcommon.AuthSyncCursorKey: "100"},
		users: map[string]agentstore.AuthUser{
			oldConnectionID: {ConnectionID: oldConnectionID, NativeAuthID: oldConnectionID},
		},
	}
	client := fakeClient{
		manifest: &hysteriaproto.AuthManifestResponse{ManifestStartedAt: 300, Full: true, Changes: []*hysteriaproto.CredentialChange{
			{ConnectionId: newConnectionID, Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT, Payload: &hysteriaproto.Credential{PasswordHash: "hash"}},
		}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.users[oldConnectionID]; ok {
		t.Fatal("expected old local auth credential to be removed")
	}
	if _, ok := st.users[newConnectionID]; !ok {
		t.Fatal("expected new local auth credential to be present")
	}
	if st.state[agentcommon.AuthSyncCursorKey] != "300" {
		t.Fatalf("memory store apply full should use test cursor 300, got %v", st.state[agentcommon.AuthSyncCursorKey])
	}
}
