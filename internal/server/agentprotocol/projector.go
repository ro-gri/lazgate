package agentprotocol

import (
	"context"
	protocol "laz/internal/protocol"

	"time"
)

type Projector[T any] interface {
	ListSyncChanges(ctx context.Context, query Query) ([]protocol.Change[T], error)
}

type Query struct {
	Full                     bool
	SinceManifestStartedAtMS int64
	FromConnectionID         string
	PageSize                 int
	ManifestStartedAtMS      int64
}

func BuildResponse[T any](ctx context.Context, projector Projector[T], request protocol.Request, now func() time.Time) (protocol.Response[T], error) {
	if err := protocol.ValidatePageSize(request.PageSize); err != nil {
		return protocol.Response[T]{}, err
	}
	if now == nil {
		now = time.Now
	}
	manifestStartedAtMS := now().UTC().UnixMilli()
	changes, err := projector.ListSyncChanges(ctx, Query{
		Full:                     request.Full,
		SinceManifestStartedAtMS: request.SinceManifestStartedAtMS,
		FromConnectionID:         request.FromConnectionID,
		PageSize:                 request.PageSize,
		ManifestStartedAtMS:      manifestStartedAtMS,
	})
	if err != nil {
		return protocol.Response[T]{}, err
	}
	protocol.SortChanges(changes)
	if len(changes) > request.PageSize {
		changes = changes[:request.PageSize]
	}
	response := protocol.Response[T]{ManifestStartedAtMS: manifestStartedAtMS, Full: request.Full, Changes: changes}
	return response, protocol.ValidateResponse(response, request.PageSize)
}

func PageChanges[T any](changes []protocol.Change[T], fromConnectionID string, pageSize int) ([]protocol.Change[T], error) {
	pageSize = protocol.NormalizePageSize(pageSize)
	if err := protocol.ValidatePageSize(pageSize); err != nil {
		return nil, err
	}
	protocol.SortChanges(changes)
	page := make([]protocol.Change[T], 0, min(pageSize, len(changes)))
	for _, change := range changes {
		if change.ConnectionID == "" {
			return nil, protocol.ErrInvalidConnectionID
		}
		if fromConnectionID != "" && change.ConnectionID <= fromConnectionID {
			continue
		}
		page = append(page, change)
		if len(page) == pageSize {
			break
		}
	}
	return page, nil
}
