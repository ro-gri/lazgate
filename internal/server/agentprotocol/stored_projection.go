package agentprotocol

import (
	"context"
	protocol "laz/internal/protocol"
	"sort"
)

// StoredConnection is the effective desired state of one credential. Material
// is opaque to the sync framework and interpreted by the protocol adapter.
type StoredConnection[M any] struct {
	ConnectionID string
	Active       bool
	UpdatedAtMS  int64
	Material     M
}

// StoredConnectionSource returns effective credential projections for one
// node. Results may be restricted to IDs after fromConnectionID.
type StoredConnectionSource[M any] interface {
	ListStoredConnections(ctx context.Context, nodeID, fromConnectionID string) ([]StoredConnection[M], error)
}

type StoredConnectionSourceFunc[M any] func(ctx context.Context, nodeID, fromConnectionID string) ([]StoredConnection[M], error)

func (f StoredConnectionSourceFunc[M]) ListStoredConnections(ctx context.Context, nodeID, fromConnectionID string) ([]StoredConnection[M], error) {
	return f(ctx, nodeID, fromConnectionID)
}

type StoredConnectionChangeBuilder[M any, T any] interface {
	BuildSyncConnectionChange(ctx context.Context, credential StoredConnection[M]) (T, error)
}

type StoredConnectionChangeBuilderFunc[M any, T any] func(ctx context.Context, credential StoredConnection[M]) (T, error)

func (f StoredConnectionChangeBuilderFunc[M, T]) BuildSyncConnectionChange(ctx context.Context, credential StoredConnection[M]) (T, error) {
	return f(ctx, credential)
}

type StoredConnectionProjector[M any, T any] struct {
	NodeID  string
	Source  StoredConnectionSource[M]
	Builder StoredConnectionChangeBuilder[M, T]
}

func (p StoredConnectionProjector[M, T]) ListSyncChanges(ctx context.Context, query Query) ([]protocol.Change[T], error) {
	if p.Source == nil {
		return nil, ErrMissingStoredConnectionSource
	}
	if p.Builder == nil {
		return nil, ErrMissingStoredConnectionBuilder
	}
	credentials, err := p.Source.ListStoredConnections(ctx, p.NodeID, query.FromConnectionID)
	if err != nil {
		return nil, err
	}
	sort.Slice(credentials, func(i, j int) bool {
		return credentials[i].ConnectionID < credentials[j].ConnectionID
	})
	changes := make([]protocol.Change[T], 0, min(query.PageSize, len(credentials)))
	for _, credential := range credentials {
		if credential.ConnectionID == "" {
			return nil, protocol.ErrInvalidConnectionID
		}
		if query.FromConnectionID != "" && credential.ConnectionID <= query.FromConnectionID {
			continue
		}
		if query.Full {
			if !credential.Active {
				continue
			}
		} else if credential.UpdatedAtMS <= 0 || credential.UpdatedAtMS < query.SinceManifestStartedAtMS {
			continue
		}
		payload, err := p.Builder.BuildSyncConnectionChange(ctx, credential)
		if err != nil {
			return nil, err
		}
		operation := protocol.OperationDelete
		if credential.Active {
			operation = protocol.OperationUpsert
		}
		changes = append(changes, protocol.Change[T]{
			ConnectionID: credential.ConnectionID,
			Operation:    operation,
			UpdatedAtMS:  credential.UpdatedAtMS,
			Payload:      payload,
		})
		if len(changes) == query.PageSize {
			break
		}
	}
	return changes, nil
}
