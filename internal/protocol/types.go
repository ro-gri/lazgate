package protocol

import (
	"errors"
	"fmt"
	"sort"
)

const (
	DefaultPageSize = 100
	MaxPageSize     = 1000
)

type Operation string

const (
	OperationUpsert Operation = "upsert"
	OperationDelete Operation = "delete"
)

type Request struct {
	Full                     bool
	SinceManifestStartedAtMS int64
	PageSize                 int
	FromConnectionID         string
}

type Change[T any] struct {
	ConnectionID string
	Operation    Operation
	UpdatedAtMS  int64
	Payload      T
}

type Response[T any] struct {
	ManifestStartedAtMS int64
	Full                bool
	Changes             []Change[T]
}

var (
	ErrInvalidPageSize     = errors.New("invalid sync page_size")
	ErrInvalidConnectionID = errors.New("invalid sync connection_id")
	ErrUnsortedChanges     = errors.New("sync changes must be sorted by connection_id")
)

func SortChanges[T any](changes []Change[T]) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].ConnectionID < changes[j].ConnectionID })
}

func NormalizePageSize(pageSize int) int {
	if pageSize == 0 {
		return DefaultPageSize
	}
	return pageSize
}

func ValidatePageSize(pageSize int) error {
	if pageSize < 1 || pageSize > MaxPageSize {
		return fmt.Errorf("%w: got %d, want 1..%d", ErrInvalidPageSize, pageSize, MaxPageSize)
	}
	return nil
}

func ValidateResponse[T any](response Response[T], pageSize int) error {
	if err := ValidatePageSize(pageSize); err != nil {
		return err
	}
	if len(response.Changes) > pageSize {
		return fmt.Errorf("%w: response has %d changes for page_size %d", ErrInvalidPageSize, len(response.Changes), pageSize)
	}
	previous := ""
	for _, change := range response.Changes {
		if change.ConnectionID == "" {
			return ErrInvalidConnectionID
		}
		if previous != "" && change.ConnectionID <= previous {
			return ErrUnsortedChanges
		}
		previous = change.ConnectionID
	}
	return nil
}
