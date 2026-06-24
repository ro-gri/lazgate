package sync

import (
	"context"
	"errors"
	"reflect"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	agentstore "laz/internal/agent/common/store"
	"laz/internal/legacy/agent/amneziawg/reconcile"
	"laz/internal/legacy/agent/amneziawg/tools"
	"laz/internal/protocol"
	"path/filepath"
)

type syncStateStore struct {
	values map[string]string
	*agentstore.DB
}

func (s *syncStateStore) GetState(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", agentstore.ErrNotFound
	}
	return value, nil
}

func (s *syncStateStore) SetState(_ context.Context, key string, value string) error {
	s.values[key] = value
	return nil
}

type manifestClient struct {
	responses []*amneziaproto.AuthManifestResponse
	requests  []protocol.Request
}

func (c *manifestClient) AuthManifest(_ context.Context, request protocol.Request) (*amneziaproto.AuthManifestResponse, error) {
	c.requests = append(c.requests, request)
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

type syncTools struct {
	operations []string
	removeErr  error
}

func (*syncTools) ShowDump(context.Context) (tools.InterfaceDump, error) {
	return tools.InterfaceDump{}, nil
}

func (t *syncTools) SetPeer(_ context.Context, publicKey string, _ tools.PeerOptions) error {
	t.operations = append(t.operations, "set:"+publicKey)
	return nil
}

func (*syncTools) SetPeerAllowedIPs(context.Context, string, []string) error { return nil }

func (t *syncTools) RemovePeer(_ context.Context, publicKey string) error {
	t.operations = append(t.operations, "delete:"+publicKey)
	return t.removeErr
}

func TestRemovalFailureKeepsDBIdentityAndCursorForRetry(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "incremental", true: "full"}[full], func(t *testing.T) {
			ctx := context.Background()
			db, err := agentstore.Open(filepath.Join(t.TempDir(), "agent.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			user := agentstore.AuthUser{ConnectionID: "connection", NativeAuthID: "public-key", NativeMeta: []byte(`{}`)}
			if err := db.UpsertAuthUser(ctx, user); err != nil {
				t.Fatal(err)
			}
			state := &syncStateStore{values: map[string]string{}, DB: db}
			runtime := &syncTools{removeErr: errors.New("native apply failed")}
			response := &amneziaproto.AuthManifestResponse{ManifestStartedAt: 200, Full: full}
			if !full {
				response.Changes = []*amneziaproto.CredentialChange{{ConnectionId: "connection", Op: commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE}}
			}
			client := &manifestClient{responses: []*amneziaproto.AuthManifestResponse{response, response}}
			syncer := New(state, reconcile.New(runtime), client)
			if err := syncer.RunOnce(ctx, full); err == nil {
				t.Fatal("expected failure")
			}
			if _, err := db.GetAuthUserByNativeAuthID(ctx, "public-key"); err != nil {
				t.Fatalf("retry identity lost: %v", err)
			}
			if _, err := db.GetState(ctx, "last_applied_auth_manifest_started_at"); err != agentstore.ErrNotFound {
				t.Fatalf("cursor advanced despite failure: %v", err)
			}
			runtime.removeErr = nil
			if err := syncer.RunOnce(ctx, full); err != nil {
				t.Fatal(err)
			}
			if _, err := db.GetAuthUserByNativeAuthID(ctx, "public-key"); err != agentstore.ErrNotFound {
				t.Fatalf("retry did not remove identity: %v", err)
			}
		})
	}
}

func (*syncTools) SetInterfaceConfig(context.Context, tools.InterfaceConfig) error { return nil }

func TestPayloadFreeDeleteResolvesPublicKeyFromAgentState(t *testing.T) {
	db, err := agentstore.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state := &syncStateStore{values: map[string]string{}, DB: db}

	runtime := &syncTools{}
	client := &manifestClient{responses: []*amneziaproto.AuthManifestResponse{
		{
			NodeId:            "node-1",
			ManifestStartedAt: 100,
			Full:              true,
			Changes: []*amneziaproto.CredentialChange{{
				ConnectionId: "connection-1",
				Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT,
				Payload: &amneziaproto.Credential{
					PublicKey:           "public-key-1",
					PresharedKey:        "preshared-key-1",
					AssignedIpv4:        "10.8.1.2",
					DesiredStatus:       "active",
					PersistentKeepalive: 25,
				},
			}},
		},
		{
			NodeId:            "node-1",
			ManifestStartedAt: 200,
			Changes: []*amneziaproto.CredentialChange{{
				ConnectionId: "connection-1",
				Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE,
			}},
		},
	}}
	syncer := New(state, reconcile.New(runtime), client)

	if err := syncer.RunOnce(context.Background(), true); err != nil {
		t.Fatalf("full sync: %v", err)
	}
	if err := syncer.RunOnce(context.Background(), false); err != nil {
		t.Fatalf("incremental sync: %v", err)
	}

	if want := []string{"set:public-key-1", "delete:public-key-1", "delete:public-key-1"}; !reflect.DeepEqual(runtime.operations, want) {
		t.Fatalf("runtime operations = %v, want %v", runtime.operations, want)
	}
	if _, err := db.GetAuthUserByNativeAuthID(context.Background(), "public-key-1"); err != agentstore.ErrNotFound {
		t.Fatalf("deleted peer remains: %v", err)
	}
	if len(client.requests) != 2 || client.requests[1].SinceManifestStartedAtMS != 100 || client.requests[1].Full {
		t.Fatalf("requests = %+v", client.requests)
	}
}
