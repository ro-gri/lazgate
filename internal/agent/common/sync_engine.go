package common

import (
	"context"
	"errors"

	"laz/internal/protocol"
)

type CursorStore interface {
	LoadSyncCursor(ctx context.Context, key string) (cursorMS int64, ok bool, err error)
	SaveSyncCursor(ctx context.Context, key string, cursorMS int64) error
}

type Source[T any] interface {
	FetchSyncPage(ctx context.Context, request protocol.Request) (protocol.Response[T], error)
}

type Applier[T any] interface {
	BeginSyncPass(ctx context.Context, full bool) (Pass[T], error)
}

type Pass[T any] interface {
	ApplySyncChange(ctx context.Context, change protocol.Change[T]) error
	FinishSyncPass(ctx context.Context, result PassResult) error
}

type PassResult struct {
	Full                bool
	ManifestStartedAtMS int64
	SeenConnectionIDs   map[string]struct{}
}

type Engine[T any] struct {
	CursorKey string
	PageSize  int
	Cursors   CursorStore
	Source    Source[T]
	Applier   Applier[T]
}

func (e Engine[T]) Run(ctx context.Context, full bool) error {
	pageSize := protocol.NormalizePageSize(e.PageSize)
	if err := protocol.ValidatePageSize(pageSize); err != nil {
		return err
	}
	if e.Cursors == nil {
		return errors.New("agent sync cursor store is required")
	}
	if e.Source == nil {
		return errors.New("agent sync source is required")
	}
	if e.Applier == nil {
		return errors.New("agent sync applier is required")
	}
	since := int64(0)
	if !full {
		cursor, ok, err := e.Cursors.LoadSyncCursor(ctx, e.CursorKey)
		if err != nil {
			return err
		}
		if ok {
			since = cursor
		} else {
			full = true
		}
	}
	seen := map[string]struct{}{}
	fromConnectionID := ""
	passCursor := int64(0)
	firstPage := true
	var pass Pass[T]
	for {
		response, err := e.Source.FetchSyncPage(ctx, protocol.Request{
			Full:                     full,
			SinceManifestStartedAtMS: since,
			PageSize:                 pageSize,
			FromConnectionID:         fromConnectionID,
		})
		if err != nil {
			return err
		}
		if err := protocol.ValidateResponse(response, pageSize); err != nil {
			return err
		}
		if firstPage {
			passCursor = response.ManifestStartedAtMS
			if response.Full {
				full = true
			}
			pass, err = e.Applier.BeginSyncPass(ctx, full)
			if err != nil {
				return err
			}
			firstPage = false
		}
		for _, change := range response.Changes {
			if err := pass.ApplySyncChange(ctx, change); err != nil {
				return err
			}
			seen[change.ConnectionID] = struct{}{}
			fromConnectionID = change.ConnectionID
		}
		if len(response.Changes) < pageSize {
			break
		}
	}
	if err := pass.FinishSyncPass(ctx, PassResult{
		Full:                full,
		ManifestStartedAtMS: passCursor,
		SeenConnectionIDs:   seen,
	}); err != nil {
		return err
	}
	return e.Cursors.SaveSyncCursor(ctx, e.CursorKey, passCursor)
}
