package view

import (
	"encoding/json"
	"time"

	"laz/internal/server/connections"
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
type Connection struct {
	ID            string                 `json:"id"`
	AccountID     string                 `json:"account_id"`
	NodeID        string                 `json:"node_id"`
	Name          string                 `json:"name"`
	Protocol      model.Protocol         `json:"protocol"`
	DesiredStatus model.ConnectionStatus `json:"desired_status"`
	AppliedStatus model.ConnectionStatus `json:"applied_status"`
	Pending       bool                   `json:"pending"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}
type Node struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   model.NodeType `json:"type"`
	Region string         `json:"region,omitempty"`
	Status model.Status   `json:"status"`
}
type ConnectionSummary struct {
	Connection Connection `json:"connection"`
	Node       Node       `json:"node"`
}
type IssuedConfig struct {
	ID           string           `json:"id"`
	ConnectionID string           `json:"connection_id"`
	Kind         model.ConfigKind `json:"kind"`
	Slug         string           `json:"slug,omitempty"`
	Name         string           `json:"name"`
	Client       string           `json:"client,omitempty"`
	ContentType  string           `json:"content_type,omitempty"`
	Config       string           `json:"config"`
	ImportURL    string           `json:"import_url,omitempty"`
	Status       model.Status     `json:"status"`
}
type ConfigProfile struct {
	ID          string           `json:"id"`
	Protocol    model.Protocol   `json:"protocol"`
	Kind        model.ConfigKind `json:"kind"`
	Slug        string           `json:"slug"`
	Name        string           `json:"name"`
	Client      string           `json:"client,omitempty"`
	ContentType string           `json:"content_type,omitempty"`
	Description string           `json:"description,omitempty"`
	Status      model.Status     `json:"status"`
}
type Session struct {
	ID        string       `json:"id"`
	AccountID string       `json:"account_id"`
	Status    model.Status `json:"status"`
	ExpiresAt time.Time    `json:"expires_at,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}
type EffectiveConnectionPolicy struct {
	AllowedNodeIDs  []string `json:"allowed_node_ids"`
	ConnectionLimit int      `json:"connection_limit"`
}

var converter Converter = &ConverterImpl{}

func EffectivePolicyItem(v model.EffectiveConnectionPolicy) EffectiveConnectionPolicy {
	return EffectiveConnectionPolicy{AllowedNodeIDs: v.AllowedNodeIDs, ConnectionLimit: v.ConnectionLimit}
}
func AccountItem(v model.Account) Account { return converter.ToAccount(v) }
func NodeItem(v model.Node) Node          { return converter.ToNode(v) }
func ConnectionItem(v model.Connection) Connection {
	out := converter.ToConnection(v)
	out.Pending = v.Pending()
	return out
}
func IssuedConfigItem(v model.IssuedConfig) IssuedConfig {
	out := converter.ToIssuedConfig(v)
	if v.Kind == model.ConfigAmneziaConf || v.Kind == model.ConfigAmneziaVPN {
		out.ImportURL, _ = connections.AmneziaImportURL(v)
	}
	return out
}
func ConfigProfileItem(v model.ConfigProfile) ConfigProfile {
	out := converter.ToConfigProfile(v)
	var p struct {
		Description string `json:"description"`
	}
	if json.Unmarshal([]byte(v.ConfigTemplate), &p) == nil {
		out.Description = p.Description
	}
	return out
}
func SessionItem(v model.SessionView) Session { return converter.ToSession(v) }
func Summary(summary model.AccountSummary) map[string]any {
	return map[string]any{"account": AccountItem(summary.Account), "connections": ActiveConnections(summary), "configs": ActiveConfigs(summary), "profiles": ActiveProfiles(summary), "generated_at": summary.Generated}
}
func ActiveConfigs(summary model.AccountSummary) []IssuedConfig {
	active := map[string]bool{}
	for _, v := range activeConnections(summary) {
		active[v.Connection.ID] = true
	}
	out := []IssuedConfig{}
	for _, v := range summary.Configs {
		if v.Status == model.StatusActive && active[v.ConnectionID] && v.Kind != model.ConfigHy2URI {
			out = append(out, IssuedConfigItem(v))
		}
	}
	return out
}
func ActiveProfiles(summary model.AccountSummary) []ConfigProfile {
	protocols := map[model.Protocol]bool{}
	for _, v := range activeConnections(summary) {
		protocols[v.Connection.Protocol] = true
	}
	out := []ConfigProfile{}
	for _, v := range summary.Profiles {
		if v.Status == model.StatusActive && protocols[v.Protocol] {
			out = append(out, ConfigProfileItem(v))
		}
	}
	return out
}
func ActiveConnections(summary model.AccountSummary) []ConnectionSummary {
	items := activeConnections(summary)
	out := make([]ConnectionSummary, len(items))
	for i, v := range items {
		out[i] = ConnectionSummary{Connection: ConnectionItem(v.Connection), Node: converter.ToNode(v.Node)}
	}
	return out
}
func activeConnections(summary model.AccountSummary) []model.ConnectionSummary {
	out := []model.ConnectionSummary{}
	for _, v := range summary.Connections {
		if v.Connection.DesiredStatus != model.ConnectionStatusDeleted {
			out = append(out, v)
		}
	}
	return out
}
