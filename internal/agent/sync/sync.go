package sync

import (
	"context"
	"encoding/json"
	"strconv"

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

type Syncer struct {
	store  Store
	client Client
}

func New(st Store, client Client) *Syncer {
	return &Syncer{store: st, client: client}
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
	if manifest.Full {
		keep := map[string]agentstore.AuthUser{}
		for _, snap := range snapshots.Snapshots {
			if snap.Op != "upsert" {
				continue
			}
			keep[snap.UserId] = toAuthUser(snap)
		}
		return s.store.ApplyFullAuthSnapshot(ctx, keep, manifest.ManifestStartedAt)
	}
	for _, snap := range snapshots.Snapshots {
		switch snap.Op {
		case "upsert":
			if err := s.store.UpsertAuthUser(ctx, toAuthUser(snap)); err != nil {
				return err
			}
		case "delete_from_auth":
			if err := s.store.DeleteAuthUser(ctx, snap.UserId); err != nil {
				return err
			}
		}
	}
	return s.store.SetState(ctx, cursorKey, strconv.FormatInt(manifest.ManifestStartedAt, 10))
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
