package sync

import (
	"context"
	"encoding/json"
	"time"

	hysteriaproto "laz/generated/nodeproto/hysteria"
	agentcommon "laz/internal/agent/common"
	agentstore "laz/internal/agent/common/store"
	"laz/internal/protocol"
)

type Client interface {
	AuthManifest(context.Context, protocol.Request) (*hysteriaproto.AuthManifestResponse, error)
}

type Kicker interface {
	Kick(context.Context, string) error
}

func New(st agentcommon.AuthStore, client Client, kickers ...Kicker) agentcommon.CredentialSync[*hysteriaproto.CredentialChange] {
	var kicker Kicker
	if len(kickers) > 0 {
		kicker = kickers[0]
	}
	return agentcommon.CredentialSync[*hysteriaproto.CredentialChange]{Store: st, Source: agentcommon.ManifestSource[*hysteriaproto.CredentialChange, *hysteriaproto.AuthManifestResponse]{Fetch: client.AuthManifest}, Runtime: hysteriaCredentialRuntime{kicker: kicker}}
}

type hysteriaCredentialRuntime struct {
	kicker Kicker
}

func (r hysteriaCredentialRuntime) ApplyCredential(_ context.Context, change *hysteriaproto.CredentialChange) (agentstore.AuthUser, error) {
	return toAuthUser(change), nil
}

func (r hysteriaCredentialRuntime) RemoveCredential(ctx context.Context, user agentstore.AuthUser) error {
	if user.NativeAuthID == "" {
		return nil
	}
	return kickCredential(ctx, r.kicker, user.NativeAuthID)
}

func (r hysteriaCredentialRuntime) ReconcileCredentials(context.Context, map[string]agentstore.AuthUser) error {
	return nil
}

func kickCredential(ctx context.Context, kicker Kicker, connectionID string) error {
	if kicker == nil || connectionID == "" {
		return nil
	}
	kickCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := kicker.Kick(kickCtx, connectionID)
	cancel()
	return err
}

func toAuthUser(snap *hysteriaproto.CredentialChange) agentstore.AuthUser {
	credential := snap.GetPayload()
	meta, _ := json.Marshal(struct {
		PasswordHash string `json:"password_hash"`
	}{credential.GetPasswordHash()})
	return agentstore.AuthUser{
		ConnectionID: snap.GetConnectionId(),
		NativeAuthID: snap.GetConnectionId(),
		NativeMeta:   meta,
	}
}
