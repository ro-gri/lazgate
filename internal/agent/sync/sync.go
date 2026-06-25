package sync

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	agentstore "laz/internal/agent/store"
	"laz/internal/nodeproto"
)

const cursorKey = "last_applied_auth_manifest_started_at"

type Store interface {
	GetState(context.Context, string) (string, error)
	SetState(context.Context, string, string) error
	UpsertAuthUser(context.Context, agentstore.AuthUser) error
	DeleteAuthUser(context.Context, string) error
	ListAuthUsers(context.Context) ([]agentstore.AuthUser, error)
	ApplyFullAuthSnapshot(context.Context, map[string]agentstore.AuthUser, int64) error
}

type Client interface {
	AuthManifest(context.Context, int64, bool) (*nodeproto.AuthManifestResponse, error)
	AuthSnapshots(context.Context, []string) (*nodeproto.UserAuthSnapshotResponse, error)
}

type Kicker interface {
	Kick(context.Context, string) error
}

type Syncer struct {
	store  Store
	client Client
	kicker Kicker
}

func New(st Store, client Client, kickers ...Kicker) *Syncer {
	var kicker Kicker
	if len(kickers) > 0 {
		kicker = kickers[0]
	}
	return &Syncer{store: st, client: client, kicker: kicker}
}

func (s *Syncer) RunOnce(ctx context.Context, full bool) error {
	since := int64(0)
	if !full {
		if raw, err := s.store.GetState(ctx, cursorKey); err == nil {
			since, _ = strconv.ParseInt(raw, 10, 64)
		} else {
			full = true
		}
	}
	manifest, err := s.client.AuthManifest(ctx, since, full)
	if err != nil {
		return err
	}
	if len(manifest.Users) == 0 {
		return s.store.SetState(ctx, cursorKey, strconv.FormatInt(manifest.ManifestStartedAt, 10))
	}
	snapshots, err := s.client.AuthSnapshots(ctx, manifest.Users)
	if err != nil {
		return err
	}
	return s.ApplySnapshots(ctx, snapshots.Snapshots, manifest.ManifestStartedAt, manifest.Full)
}

func (s *Syncer) ApplySnapshots(ctx context.Context, snapshots []*nodeproto.UserAuthSnapshot, manifestStartedAt int64, full bool) error {
	current, err := s.store.ListAuthUsers(ctx)
	if err != nil {
		return err
	}
	currentByUser := map[string]agentstore.AuthUser{}
	for _, user := range current {
		currentByUser[user.UserID] = user
	}
	toKick := map[string]bool{}
	if full {
		keep := map[string]agentstore.AuthUser{}
		for _, snap := range snapshots {
			if snap.GetOp() != "upsert" {
				continue
			}
			next := toAuthUser(snap)
			keep[snap.GetUserId()] = next
			if prev, ok := currentByUser[snap.GetUserId()]; ok && prev.CredentialID != "" && prev.CredentialID != next.CredentialID {
				toKick[prev.CredentialID] = true
			}
		}
		for id, prev := range currentByUser {
			if _, ok := keep[id]; !ok && prev.CredentialID != "" {
				toKick[prev.CredentialID] = true
			}
		}
		if err := s.store.ApplyFullAuthSnapshot(ctx, keep, manifestStartedAt); err != nil {
			return err
		}
		return s.kickRemoved(ctx, toKick)
	}
	for _, snap := range snapshots {
		switch snap.GetOp() {
		case "upsert":
			next := toAuthUser(snap)
			if prev, ok := currentByUser[snap.GetUserId()]; ok && prev.CredentialID != "" && prev.CredentialID != next.CredentialID {
				toKick[prev.CredentialID] = true
			}
			if err := s.store.UpsertAuthUser(ctx, next); err != nil {
				return err
			}
		case "delete_from_auth":
			if prev, ok := currentByUser[snap.GetUserId()]; ok && prev.CredentialID != "" {
				toKick[prev.CredentialID] = true
			}
			if err := s.store.DeleteAuthUser(ctx, snap.GetUserId()); err != nil {
				return err
			}
		}
	}
	if manifestStartedAt > 0 {
		if err := s.store.SetState(ctx, cursorKey, strconv.FormatInt(manifestStartedAt, 10)); err != nil {
			return err
		}
	}
	return s.kickRemoved(ctx, toKick)
}

func (s *Syncer) kickRemoved(ctx context.Context, credentials map[string]bool) error {
	if s.kicker == nil || len(credentials) == 0 {
		return nil
	}
	for credentialID := range credentials {
		kickCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.kicker.Kick(kickCtx, credentialID)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func toAuthUser(snap *nodeproto.UserAuthSnapshot) agentstore.AuthUser {
	payload, _ := json.Marshal(snap)
	return agentstore.AuthUser{
		UserID:                    snap.UserId,
		CredentialID:              snap.CredentialId,
		Username:                  snap.Username,
		PasswordHash:              snap.PasswordHash,
		ExpiresAtMS:               snap.ExpiresAtMs,
		QuotaLimitBytes:           snap.QuotaLimitBytes,
		LastKnownGlobalUsageBytes: snap.LastKnownGlobalUsageBytes,
		QuotaGuardOverageBytes:    snap.QuotaGuardOverageBytes,
		PayloadJSON:               string(payload),
	}
}
