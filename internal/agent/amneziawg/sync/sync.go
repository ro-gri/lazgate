package sync

import (
	"context"

	amneziaproto "laz/generated/nodeproto/amnezia"
	agentcommon "laz/internal/agent/common"
	agentstore "laz/internal/agent/common/store"

	"laz/internal/legacy/agent/amneziawg/reconcile"
	"laz/internal/protocol"
)

type Client interface {
	AuthManifest(context.Context, protocol.Request) (*amneziaproto.AuthManifestResponse, error)
}

type DesiredPeers interface {
	PrunePeers(context.Context, map[string]bool) error
}

func New(st agentcommon.AuthStore, reconciler *reconcile.Reconciler, client Client, desired ...DesiredPeers) agentcommon.CredentialSync[*amneziaproto.CredentialChange] {
	var snapshot DesiredPeers
	if len(desired) > 0 {
		snapshot = desired[0]
	}
	return agentcommon.CredentialSync[*amneziaproto.CredentialChange]{Store: persistentAuthStore{st, reconciler, snapshot}, Source: agentcommon.ManifestSource[*amneziaproto.CredentialChange, *amneziaproto.AuthManifestResponse]{Fetch: client.AuthManifest}, Runtime: amneziaRuntime{reconciler}}
}

// Keep the identity mapping until native desired state and runtime removal have
// succeeded. The shared sync flow otherwise deletes this mapping first.
type persistentAuthStore struct {
	agentcommon.AuthStore
	reconciler *reconcile.Reconciler
	desired    DesiredPeers
}

func (s persistentAuthStore) DeleteAuthUser(ctx context.Context, id string) error {
	users, err := s.ListAuthUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.ConnectionID == id {
			if err := s.reconciler.RemovePeer(ctx, user.NativeAuthID); err != nil {
				return err
			}
		}
	}
	return s.AuthStore.DeleteAuthUser(ctx, id)
}

func (s persistentAuthStore) ApplyFullAuthSnapshot(ctx context.Context, keep map[string]agentstore.AuthUser, cursor string, started int64) error {
	users, err := s.ListAuthUsers(ctx)
	if err != nil {
		return err
	}
	for _, user := range users {
		if _, ok := keep[user.ConnectionID]; !ok {
			if err := s.reconciler.RemovePeer(ctx, user.NativeAuthID); err != nil {
				return err
			}
		}
	}
	keys := make(map[string]bool, len(keep))
	for _, user := range keep {
		keys[user.NativeAuthID] = true
	}
	if s.desired != nil {
		if err := s.desired.PrunePeers(ctx, keys); err != nil {
			return err
		}
	}
	if err := s.reconciler.PruneRuntimePeers(ctx, keys); err != nil {
		return err
	}
	return s.AuthStore.ApplyFullAuthSnapshot(ctx, keep, cursor, started)
}

type amneziaRuntime struct{ reconciler *reconcile.Reconciler }

func (r amneziaRuntime) ApplyCredential(ctx context.Context, c *amneziaproto.CredentialChange) (agentstore.AuthUser, error) {
	if err := r.reconciler.UpsertPeer(ctx, c.GetPayload()); err != nil {
		return agentstore.AuthUser{}, err
	}
	return agentstore.AuthUser{ConnectionID: c.GetConnectionId(), NativeAuthID: c.GetPayload().GetPublicKey(), NativeMeta: []byte(`{}`)}, nil
}
func (r amneziaRuntime) RemoveCredential(ctx context.Context, u agentstore.AuthUser) error {
	return r.reconciler.RemovePeer(ctx, u.NativeAuthID)
}
func (r amneziaRuntime) ReconcileCredentials(ctx context.Context, keep map[string]agentstore.AuthUser) error {
	keys := make(map[string]bool, len(keep))
	for _, u := range keep {
		keys[u.NativeAuthID] = true
	}
	return r.reconciler.PruneRuntimePeers(ctx, keys)
}
