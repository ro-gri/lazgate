package remote

import (
	"context"

	"laz/internal/server/model"
)

type Provider interface {
	CreateConnection(ctx context.Context, input CreateInput) (Connection, error)
	ApplyConnection(ctx context.Context, input ApplyInput) error
	SetConnectionStatus(ctx context.Context, ref Ref, status model.Status) error
	DeleteConnection(ctx context.Context, ref Ref) error
	ListConnections(ctx context.Context) (ConnectionList, error)
}

type AgentControl interface {
	RefreshUserAuth(ctx context.Context, nodeID string, accountID string, operation string) error
	KickClient(ctx context.Context, nodeID string, credentialID string) error
}

type CreateInput struct {
	AccountID      string
	ClientID       string
	CredentialID   string
	NodeID         string
	Name           string
	ConfigName     string
	Password       string
	TrafficLimitGB int
	ExpirationDays int
	Unlimited      bool
	IncludeIPv6    bool
	Note           string
}

type ApplyInput struct {
	NodeID       string
	AccountID    string
	ClientID     string
	ConnectionID string
	RemoteID     string
	RemoteName   string
	Status       model.Status
	Operation    string
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
