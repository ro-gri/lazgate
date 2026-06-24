package remote

import (
	"context"

	"laz/internal/server/model"
)

type Provider interface {
	CreateConnection(ctx context.Context, input CreateInput) (Connection, error)
	SetConnectionStatus(ctx context.Context, ref Ref, status model.ConnectionStatus) error
	DeleteConnection(ctx context.Context, ref Ref) error
	ListConnections(ctx context.Context) (ConnectionList, error)
}

type CreateInput struct {
	AccountID    string
	ConnectionID string
	NodeID       string
	Name         string
}

type Connection struct {
	Ref     Ref
	Configs []Config
}

type ConnectionList struct {
	Total int              `json:"total"`
	Items []ConnectionInfo `json:"items"`
}

type ConnectionInfo struct {
	Ref    Ref    `json:"ref"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Raw    any    `json:"raw,omitempty"`
}

type Ref struct {
	ID   string
	Name string
}

type Config struct {
	Kind        model.ConfigKind
	Slug        string
	Name        string
	Client      string
	ContentType string
	Value       string
}
