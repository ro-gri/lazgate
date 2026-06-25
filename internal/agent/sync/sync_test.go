package sync

import (
	"context"
	"errors"
	"strconv"
	"testing"

	agentstore "laz/internal/agent/store"
	"laz/internal/nodeproto"
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
	m.users[u.UserID] = u
	return nil
}
func (m *memoryStore) DeleteAuthUser(_ context.Context, id string) error {
	if m.failApply {
		return errors.New("apply failed")
	}
	delete(m.users, id)
	return nil
}
func (m *memoryStore) ListAuthUsers(context.Context) ([]agentstore.AuthUser, error) { return nil, nil }
func (m *memoryStore) ApplyFullAuthSnapshot(_ context.Context, keep map[string]agentstore.AuthUser, cursorMS int64) error {
	if m.failApply {
		return errors.New("apply failed")
	}
	m.users = keep
	m.state[cursorKey] = strconv.FormatInt(cursorMS, 10)
	return nil
}

type fakeClient struct {
	manifest  *nodeproto.AuthManifestResponse
	snapshots *nodeproto.UserAuthSnapshotResponse
}

func (f fakeClient) AuthManifest(context.Context, int64, bool) (*nodeproto.AuthManifestResponse, error) {
	return f.manifest, nil
}
func (f fakeClient) AuthSnapshots(context.Context, []string) (*nodeproto.UserAuthSnapshotResponse, error) {
	return f.snapshots, nil
}

func TestSyncUpsertDeleteAndCursor(t *testing.T) {
	st := &memoryStore{state: map[string]string{cursorKey: "100"}, users: map[string]agentstore.AuthUser{"usr_2": {UserID: "usr_2"}}}
	client := fakeClient{
		manifest: &nodeproto.AuthManifestResponse{NodeId: "nod", ManifestStartedAt: 200, Users: []string{"usr_1", "usr_2"}},
		snapshots: &nodeproto.UserAuthSnapshotResponse{Snapshots: []*nodeproto.UserAuthSnapshot{
			{UserId: "usr_1", Op: "upsert", CredentialId: "cred_1", Username: "alice", PasswordHash: "hash"},
			{UserId: "usr_2", Op: "delete_from_auth"},
		}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.users["usr_1"]; !ok {
		t.Fatal("expected usr_1 upsert")
	}
	if _, ok := st.users["usr_2"]; ok {
		t.Fatal("expected usr_2 delete")
	}
	if st.state[cursorKey] != "200" {
		t.Fatalf("cursor did not advance: %v", st.state)
	}
}

func TestSyncCursorDoesNotAdvanceAfterApplyFailure(t *testing.T) {
	st := &memoryStore{state: map[string]string{cursorKey: "100"}, users: map[string]agentstore.AuthUser{}, failApply: true}
	client := fakeClient{
		manifest:  &nodeproto.AuthManifestResponse{ManifestStartedAt: 200, Users: []string{"usr_1"}},
		snapshots: &nodeproto.UserAuthSnapshotResponse{Snapshots: []*nodeproto.UserAuthSnapshot{{UserId: "usr_1", Op: "upsert"}}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err == nil {
		t.Fatal("expected apply failure")
	}
	if st.state[cursorKey] != "100" {
		t.Fatalf("cursor advanced after failure: %v", st.state)
	}
}

func TestFullResyncReplacesLocalAllowList(t *testing.T) {
	st := &memoryStore{
		state: map[string]string{cursorKey: "100"},
		users: map[string]agentstore.AuthUser{
			"usr_old": {UserID: "usr_old", Username: "old"},
		},
	}
	client := fakeClient{
		manifest: &nodeproto.AuthManifestResponse{ManifestStartedAt: 300, Full: true, Users: []string{"usr_new"}},
		snapshots: &nodeproto.UserAuthSnapshotResponse{Snapshots: []*nodeproto.UserAuthSnapshot{
			{UserId: "usr_new", Op: "upsert", CredentialId: "cred_new", Username: "new", PasswordHash: "hash"},
		}},
	}
	if err := New(st, client).RunOnce(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.users["usr_old"]; ok {
		t.Fatal("expected old local auth user to be removed")
	}
	if _, ok := st.users["usr_new"]; !ok {
		t.Fatal("expected new local auth user to be present")
	}
	if st.state[cursorKey] != "300" {
		t.Fatalf("memory store apply full should use test cursor 200, got %v", st.state[cursorKey])
	}
}
