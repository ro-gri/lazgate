package agentprotocol

import (
	"context"
	protocol "laz/internal/protocol"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
)

type ManifestRequest interface {
	GetNodeId() string
	GetFull() bool
	GetSinceManifestStartedAt() int64
	GetPageSize() uint32
	GetFromConnectionId() string
}

type ManifestHandler[T any, O any] struct {
	Projector Projector[T]
	Builder   protocol.ManifestResponseBuilder[T, O]
	Now       func() time.Time
}

func (h ManifestHandler[T, O]) Handle(ctx context.Context, input ManifestRequest) O {
	if input == nil {
		return h.Builder.EmptySyncResponse()
	}
	nodeID := input.GetNodeId()
	full := input.GetFull() || input.GetSinceManifestStartedAt() == 0
	response, err := BuildResponse(ctx, h.Projector, protocol.Request{
		Full:                     full,
		SinceManifestStartedAtMS: input.GetSinceManifestStartedAt(),
		PageSize:                 int(input.GetPageSize()),
		FromConnectionID:         input.GetFromConnectionId(),
	}, h.Now)
	if err != nil {
		return h.Builder.ErrorSyncResponse(nodeID, full, err)
	}
	return h.Builder.BuildSyncResponse(nodeID, response)
}

type StoredManifestHandler[M any, T any, O any] struct {
	NodeID        string
	Source        StoredConnectionSource[M]
	ChangeBuilder StoredConnectionChangeBuilder[M, T]
	Builder       protocol.ManifestResponseBuilder[T, O]
	Now           func() time.Time
}

func NewStoredManifestHandler[M any, T any, O any](
	listConnections StoredConnectionSourceFunc[M],
	buildChange StoredConnectionChangeBuilderFunc[M, T],
	builder protocol.ManifestResponseBuilder[T, O],
) StoredManifestHandler[M, T, O] {
	var source StoredConnectionSource[M]
	if listConnections != nil {
		source = listConnections
	}
	var changeBuilder StoredConnectionChangeBuilder[M, T]
	if buildChange != nil {
		changeBuilder = buildChange
	}
	return StoredManifestHandler[M, T, O]{
		Source:        source,
		ChangeBuilder: changeBuilder,
		Builder:       builder,
		Now:           time.Now,
	}
}

func (h StoredManifestHandler[M, T, O]) BuildAuthManifestResponse(ctx context.Context, input *commonproto.AuthManifestRequest) O {
	nodeID := ""
	if input != nil {
		nodeID = input.GetNodeId()
	}
	if h.NodeID != "" {
		nodeID = h.NodeID
	}
	return ManifestHandler[T, O]{
		Projector: StoredConnectionProjector[M, T]{
			NodeID:  nodeID,
			Source:  h.Source,
			Builder: h.ChangeBuilder,
		},
		Builder: h.Builder,
		Now:     h.Now,
	}.Handle(ctx, input)
}
