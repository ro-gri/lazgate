package view

import (
	"time"

	"laz/internal/server/model"
)

type Account struct {
	ID          string              `json:"id"`
	Username    string              `json:"username"`
	DisplayName string              `json:"display_name"`
	Status      model.AccountStatus `json:"status"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}
type Node struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      model.NodeType `json:"type"`
	Region    string         `json:"region,omitempty"`
	Status    model.Status   `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
type Connection struct {
	ID               string                 `json:"id"`
	AccountID        string                 `json:"account_id"`
	NodeID           string                 `json:"node_id"`
	Name             string                 `json:"name"`
	Protocol         model.Protocol         `json:"protocol"`
	DesiredStatus    model.ConnectionStatus `json:"desired_status"`
	AppliedStatus    model.ConnectionStatus `json:"applied_status"`
	DesiredVersionMS int64                  `json:"desired_version_ms"`
	AppliedVersionMS int64                  `json:"applied_version_ms"`
	Pending          bool                   `json:"pending"`
	LastErrorID      string                 `json:"last_error_id,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}
type ConnectionSummary struct {
	Connection Connection `json:"connection"`
	Node       Node       `json:"node"`
}
type AccountSummary struct {
	Account     Account             `json:"account"`
	Connections []ConnectionSummary `json:"connections"`
	Configs     []IssuedConfig      `json:"configs"`
	Profiles    []ConfigProfile     `json:"profiles"`
	Generated   time.Time           `json:"generated_at"`
}
type IssuedConfig struct {
	ID           string           `json:"id"`
	ConnectionID string           `json:"connection_id"`
	Kind         model.ConfigKind `json:"kind"`
	Slug         string           `json:"slug,omitempty"`
	Name         string           `json:"name"`
	Client       string           `json:"client,omitempty"`
	ContentType  string           `json:"content_type,omitempty"`
	Status       model.Status     `json:"status"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}
type ConfigProfile struct {
	ID          string           `json:"id"`
	Protocol    model.Protocol   `json:"protocol"`
	Kind        model.ConfigKind `json:"kind"`
	Slug        string           `json:"slug"`
	Name        string           `json:"name"`
	Client      string           `json:"client,omitempty"`
	ContentType string           `json:"content_type,omitempty"`
	Status      model.Status     `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}
type AccountPortalToken struct {
	ID         string       `json:"id"`
	AccountID  string       `json:"account_id"`
	Status     model.Status `json:"status"`
	ExpiresAt  time.Time    `json:"expires_at,omitempty"`
	LastUsedAt time.Time    `json:"last_used_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}
type ConnectionSubscriptionToken struct {
	ID           string       `json:"id"`
	ConnectionID string       `json:"connection_id"`
	Status       model.Status `json:"status"`
	ExpiresAt    time.Time    `json:"expires_at,omitempty"`
	LastUsedAt   time.Time    `json:"last_used_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}
type PolicyTag struct {
	ID              string       `json:"id"`
	Slug            string       `json:"slug"`
	Name            string       `json:"name"`
	AllowedNodeIDs  []string     `json:"allowed_node_ids"`
	ConnectionLimit int          `json:"connection_limit"`
	Status          model.Status `json:"status"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}
type AccountPolicyTag struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	TagID     string    `json:"tag_id"`
	CreatedAt time.Time `json:"created_at"`
}
type AuditLog struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Details    string    `json:"details,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

var converter Converter = &ConverterImpl{}

func FilterAccounts(items []model.Account, status model.AccountStatus) []model.Account {
	out := []model.Account{}
	for _, v := range items {
		if status != "" {
			if v.Status == status {
				out = append(out, v)
			}
			continue
		}
		if v.Status != model.AccountStatusDeleted {
			out = append(out, v)
		}
	}
	return out
}
func Accounts(v []model.Account) []Account { return converter.ToAccounts(v) }
func AccountItem(v model.Account) Account  { return converter.ToAccount(v) }
func Nodes(v []model.Node) []Node          { return converter.ToNodes(v) }
func NodeItem(v model.Node) Node           { return converter.ToNode(v) }
func ConnectionItem(v model.Connection) Connection {
	out := converter.ToConnection(v)
	out.Pending = v.Pending()
	return out
}
func Connections(v []model.Connection) []Connection {
	out := make([]Connection, len(v))
	for i := range v {
		out[i] = ConnectionItem(v[i])
	}
	return out
}
func ConnectionSummaries(v []model.ConnectionSummary) []ConnectionSummary {
	out := make([]ConnectionSummary, len(v))
	for i := range v {
		out[i] = ConnectionSummary{Connection: ConnectionItem(v[i].Connection), Node: NodeItem(v[i].Node)}
	}
	return out
}
func Summary(v model.AccountSummary) AccountSummary {
	return AccountSummary{Account: AccountItem(v.Account), Connections: ConnectionSummaries(v.Connections), Configs: IssuedConfigs(v.Configs), Profiles: ConfigProfiles(v.Profiles), Generated: v.Generated}
}
func IssuedConfigs(v []model.IssuedConfig) []IssuedConfig    { return converter.ToIssuedConfigs(v) }
func IssuedConfigItem(v model.IssuedConfig) IssuedConfig     { return converter.ToIssuedConfig(v) }
func ConfigProfiles(v []model.ConfigProfile) []ConfigProfile { return converter.ToConfigProfiles(v) }
func ConfigProfileItem(v model.ConfigProfile) ConfigProfile  { return converter.ToConfigProfile(v) }
func AccountPortalTokens(v []model.AccountPortalToken) []AccountPortalToken {
	return converter.ToAccountPortalTokens(v)
}
func ConnectionSubscriptionTokens(v []model.ConnectionSubscriptionToken) []ConnectionSubscriptionToken {
	return converter.ToConnectionSubscriptionTokens(v)
}
func PolicyTags(v []model.PolicyTag) []PolicyTag { return converter.ToPolicyTags(v) }
func PolicyTagItem(v model.PolicyTag) PolicyTag  { return converter.ToPolicyTag(v) }
func AccountPolicyTags(v []model.AccountPolicyTag) []AccountPolicyTag {
	return converter.ToAccountPolicyTags(v)
}
func AccountPolicyTagItem(v model.AccountPolicyTag) AccountPolicyTag {
	return converter.ToAccountPolicyTag(v)
}
func AuditLogs(v []model.AuditLog) []AuditLog { return converter.ToAuditLogs(v) }
func AuditLogItem(v model.AuditLog) AuditLog  { return converter.ToAuditLog(v) }
