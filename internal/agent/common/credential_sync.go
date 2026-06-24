package common

import (
	"context"
	"errors"
	"fmt"
	commonproto "laz/generated/nodeproto/agentcommon"
	"strconv"

	agentstore "laz/internal/agent/common/store"
	"laz/internal/protocol"
)

type ManifestChange interface {
	GetConnectionId() string
	GetOp() commonproto.CredentialChangeOp
}
type ManifestResponse[T ManifestChange] interface {
	GetChanges() []T
	GetManifestStartedAt() int64
	GetFull() bool
	GetError() string
}
type ManifestSource[T ManifestChange, R ManifestResponse[T]] struct {
	Fetch func(context.Context, protocol.Request) (R, error)
}

func (s ManifestSource[T, R]) FetchSyncPage(ctx context.Context, req protocol.Request) (protocol.Response[T], error) {
	manifest, err := s.Fetch(ctx, req)
	if err != nil {
		return protocol.Response[T]{}, err
	}
	if manifest.GetError() != "" {
		return protocol.Response[T]{}, fmt.Errorf("auth manifest rejected: %s", manifest.GetError())
	}
	changes := make([]protocol.Change[T], 0, len(manifest.GetChanges()))
	for _, c := range manifest.GetChanges() {
		op := protocol.OperationDelete
		if c.GetOp() == commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT {
			op = protocol.OperationUpsert
		}
		changes = append(changes, protocol.Change[T]{ConnectionID: c.GetConnectionId(), Operation: op, Payload: c})
	}
	protocol.SortChanges(changes)
	return protocol.Response[T]{ManifestStartedAtMS: manifest.GetManifestStartedAt(), Full: manifest.GetFull(), Changes: changes}, nil
}

type AuthStore interface {
	GetState(context.Context, string) (string, error)
	SetState(context.Context, string, string) error
	ListAuthUsers(context.Context) ([]agentstore.AuthUser, error)
	UpsertAuthUser(context.Context, agentstore.AuthUser) error
	DeleteAuthUser(context.Context, string) error
	ApplyFullAuthSnapshot(context.Context, map[string]agentstore.AuthUser, string, int64) error
}

type CredentialRuntime[T any] interface {
	ApplyCredential(context.Context, T) (agentstore.AuthUser, error)
	RemoveCredential(context.Context, agentstore.AuthUser) error
	ReconcileCredentials(context.Context, map[string]agentstore.AuthUser) error
}

const AuthSyncCursorKey = "last_applied_auth_manifest_started_at"

type CredentialSync[T any] struct {
	Store   AuthStore
	Source  Source[T]
	Runtime CredentialRuntime[T]
}

func (s CredentialSync[T]) RunOnce(ctx context.Context, full bool) error {
	return (Engine[T]{CursorKey: AuthSyncCursorKey, Cursors: authCursor{s.Store}, Source: s.Source, Applier: s}).Run(ctx, full)
}

type authCursor struct{ store AuthStore }

func (s authCursor) LoadSyncCursor(ctx context.Context, key string) (int64, bool, error) {
	raw, err := s.store.GetState(ctx, key)
	if errors.Is(err, agentstore.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	return v, true, err
}
func (s authCursor) SaveSyncCursor(ctx context.Context, key string, v int64) error {
	return s.store.SetState(ctx, key, strconv.FormatInt(v, 10))
}

func (s CredentialSync[T]) BeginSyncPass(ctx context.Context, full bool) (Pass[T], error) {
	users, err := s.Store.ListAuthUsers(ctx)
	if err != nil {
		return nil, err
	}
	current := map[string]agentstore.AuthUser{}
	for _, u := range users {
		current[u.ConnectionID] = u
	}
	return &credentialSyncPass[T]{sync: s, full: full, current: current, keep: map[string]agentstore.AuthUser{}, removed: map[string]agentstore.AuthUser{}}, nil
}

type credentialSyncPass[T any] struct {
	sync                   CredentialSync[T]
	full                   bool
	current, keep, removed map[string]agentstore.AuthUser
}

func (p *credentialSyncPass[T]) ApplySyncChange(ctx context.Context, c protocol.Change[T]) error {
	if c.Operation == protocol.OperationUpsert {
		u, err := p.sync.Runtime.ApplyCredential(ctx, c.Payload)
		if err != nil {
			return err
		}
		p.keep[c.ConnectionID] = u
		if !p.full {
			return p.sync.Store.UpsertAuthUser(ctx, u)
		}
		return nil
	}
	if u, ok := p.current[c.ConnectionID]; ok {
		p.removed[c.ConnectionID] = u
	}
	if !p.full {
		return p.sync.Store.DeleteAuthUser(ctx, c.ConnectionID)
	}
	return nil
}
func (p *credentialSyncPass[T]) FinishSyncPass(ctx context.Context, r PassResult) error {
	if p.full {
		for id, u := range p.current {
			if _, ok := p.keep[id]; !ok {
				p.removed[id] = u
			}
		}
		if err := p.sync.Store.ApplyFullAuthSnapshot(ctx, p.keep, AuthSyncCursorKey, r.ManifestStartedAtMS); err != nil {
			return err
		}
	}
	for _, u := range p.removed {
		if err := p.sync.Runtime.RemoveCredential(ctx, u); err != nil {
			return err
		}
	}
	if p.full {
		return p.sync.Runtime.ReconcileCredentials(ctx, p.keep)
	}
	return nil
}
