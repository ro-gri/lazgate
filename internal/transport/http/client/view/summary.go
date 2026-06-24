package view

import (
	"encoding/json"
	"time"

	"laz/internal/model"
)

type Account struct {
	ID          string       `json:"id"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Status      model.Status `json:"status"`
	Note        string       `json:"note,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type Client struct {
	ID        string       `json:"id"`
	AccountID string       `json:"account_id"`
	Slug      string       `json:"slug"`
	Name      string       `json:"name"`
	Status    model.Status `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type Connection struct {
	ID            string         `json:"id"`
	AccountID     string         `json:"account_id"`
	ClientID      string         `json:"client_id"`
	NodeID        string         `json:"node_id"`
	Protocol      model.Protocol `json:"protocol"`
	Status        model.Status   `json:"status"`
	DesiredStatus model.Status   `json:"desired_status"`
	LastSyncAt    time.Time      `json:"last_sync_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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

type ConnectionSummary struct {
	Connection Connection `json:"connection"`
	Node       Node       `json:"node"`
	Client     Client     `json:"client"`
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
	Description string           `json:"description,omitempty"`
	Status      model.Status     `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

type Session struct {
	ID         string       `json:"id"`
	AccountID  string       `json:"account_id"`
	Status     model.Status `json:"status"`
	ExpiresAt  time.Time    `json:"expires_at,omitempty"`
	LastUsedAt time.Time    `json:"last_used_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

type EffectiveClientPolicy struct {
	AllowedNodeIDs []string `json:"allowed_node_ids"`
	ClientLimit    int      `json:"client_limit"`
}

var converter Converter = &ConverterImpl{}

func EffectivePolicyItem(policy model.EffectiveClientPolicy) EffectiveClientPolicy {
	return EffectiveClientPolicy{AllowedNodeIDs: policy.AllowedNodeIDs, ClientLimit: policy.ClientLimit}
}

func AccountItem(account model.Account) Account {
	return converter.ToAccount(account)
}

func ClientItem(client model.Client) Client {
	return converter.ToClient(client)
}

func ConnectionItem(connection model.Connection) Connection {
	return converter.ToConnection(connection)
}

func IssuedConfigItem(cfg model.IssuedConfig) IssuedConfig {
	return converter.ToIssuedConfig(cfg)
}

func ConfigProfileItem(profile model.ConfigProfile) ConfigProfile {
	item := converter.ToConfigProfile(profile)
	var template struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(profile.ConfigTemplate), &template); err == nil {
		item.Description = template.Description
	}
	return item
}

func SessionItem(session model.ClientSession) Session {
	return converter.ToSession(session)
}

func Summary(token model.AccessToken, summary model.AccountSummary) map[string]any {
	var client any
	if token.ClientID != "" {
		for _, item := range summary.Clients {
			if item.ID == token.ClientID {
				client = ClientItem(item)
				break
			}
		}
	}
	return map[string]any{
		"account":      AccountItem(summary.Account),
		"client":       client,
		"client_id":    token.ClientID,
		"clients":      ActiveClients(token, summary),
		"connections":  ActiveConnections(token, summary),
		"configs":      ActiveConfigs(token, summary),
		"profiles":     ActiveProfiles(token, summary),
		"generated_at": summary.Generated,
	}
}

func ActiveClients(token model.AccessToken, summary model.AccountSummary) []Client {
	out := []Client{}
	for _, client := range summary.Clients {
		if client.Status != model.StatusActive {
			continue
		}
		if token.ClientID != "" && client.ID != token.ClientID {
			continue
		}
		out = append(out, ClientItem(client))
	}
	return out
}

func ActiveConfigs(token model.AccessToken, summary model.AccountSummary) []IssuedConfig {
	activeConnection := map[string]bool{}
	for _, item := range activeConnections(token, summary) {
		activeConnection[item.Connection.ID] = true
	}
	out := []IssuedConfig{}
	for _, cfg := range summary.Configs {
		if cfg.Status == model.StatusActive && activeConnection[cfg.ConnectionID] && cfg.Kind != model.ConfigHy2URI {
			out = append(out, IssuedConfigItem(cfg))
		}
	}
	return out
}

func ActiveProfiles(token model.AccessToken, summary model.AccountSummary) []ConfigProfile {
	activeProtocols := map[model.Protocol]bool{}
	for _, item := range activeConnections(token, summary) {
		activeProtocols[item.Connection.Protocol] = true
	}
	out := []ConfigProfile{}
	for _, profile := range summary.Profiles {
		if profile.Status == model.StatusActive && activeProtocols[profile.Protocol] {
			out = append(out, ConfigProfileItem(profile))
		}
	}
	return out
}

func ActiveConnections(token model.AccessToken, summary model.AccountSummary) []ConnectionSummary {
	items := activeConnections(token, summary)
	out := make([]ConnectionSummary, len(items))
	for i, item := range items {
		out[i] = ConnectionSummary{Connection: ConnectionItem(item.Connection), Node: NodeItem(item.Node), Client: ClientItem(item.Client)}
	}
	return out
}

func activeConnections(token model.AccessToken, summary model.AccountSummary) []model.ConnectionSummary {
	out := []model.ConnectionSummary{}
	for _, item := range summary.Connections {
		if item.Connection.Status != model.StatusActive {
			continue
		}
		if token.ClientID != "" && item.Connection.ClientID != token.ClientID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func NodeItem(n model.Node) Node {
	return converter.ToNode(n)
}
