package view

import (
	"time"

	"laz/internal/server/model"
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

type ConnectionSummary struct {
	Connection Connection `json:"connection"`
	Node       Node       `json:"node"`
	Client     Client     `json:"client"`
}

type AccountSummary struct {
	Account     Account             `json:"account"`
	Clients     []Client            `json:"clients"`
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

type AccessToken struct {
	ID         string       `json:"id"`
	AccountID  string       `json:"account_id"`
	ClientID   string       `json:"client_id,omitempty"`
	Purpose    string       `json:"purpose"`
	Status     model.Status `json:"status"`
	ExpiresAt  time.Time    `json:"expires_at,omitempty"`
	LastUsedAt time.Time    `json:"last_used_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

type PolicyTag struct {
	ID             string       `json:"id"`
	Slug           string       `json:"slug"`
	Name           string       `json:"name"`
	AllowedNodeIDs []string     `json:"allowed_node_ids"`
	ClientLimit    int          `json:"client_limit"`
	Status         model.Status `json:"status"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
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

func FilterAccounts(accounts []model.Account, status model.Status) []model.Account {
	out := []model.Account{}
	for _, account := range accounts {
		if status != "" {
			if account.Status == status {
				out = append(out, account)
			}
			continue
		}
		if account.Status == model.StatusDeleted {
			continue
		}
		out = append(out, account)
	}
	return out
}

func Accounts(accounts []model.Account) []Account {
	return converter.ToAccounts(accounts)
}

func AccountItem(account model.Account) Account {
	return converter.ToAccount(account)
}

func ClientItem(client model.Client) Client {
	return converter.ToClient(client)
}

func Clients(clients []model.Client) []Client {
	return converter.ToClients(clients)
}

func ConnectionItem(connection model.Connection) Connection {
	return converter.ToConnection(connection)
}

func Connections(connections []model.Connection) []Connection {
	return converter.ToConnections(connections)
}

func ConnectionSummaries(items []model.ConnectionSummary) []ConnectionSummary {
	return converter.ToConnectionSummaries(items)
}

func Summary(summary model.AccountSummary) AccountSummary {
	return AccountSummary{
		Account:     AccountItem(summary.Account),
		Clients:     Clients(summary.Clients),
		Connections: ConnectionSummaries(summary.Connections),
		Configs:     IssuedConfigs(summary.Configs),
		Profiles:    ConfigProfiles(summary.Profiles),
		Generated:   summary.Generated,
	}
}

func ActiveClientSummary(token model.AccessToken, summary model.AccountSummary) map[string]any {
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
		"connections":  activeConnectionSummaries(token, summary),
		"configs":      activeIssuedConfigs(token, summary),
		"profiles":     activeConfigProfiles(token, summary),
		"generated_at": summary.Generated,
	}
}

func activeConnectionSummaries(token model.AccessToken, summary model.AccountSummary) []ConnectionSummary {
	items := activeConnections(token, summary)
	out := make([]ConnectionSummary, len(items))
	for i, item := range items {
		out[i] = ConnectionSummary{Connection: ConnectionItem(item.Connection), Node: NodeItem(item.Node), Client: ClientItem(item.Client)}
	}
	return out
}

func activeIssuedConfigs(token model.AccessToken, summary model.AccountSummary) []IssuedConfig {
	activeConnection := map[string]bool{}
	for _, item := range activeConnections(token, summary) {
		activeConnection[item.Connection.ID] = true
	}
	out := []IssuedConfig{}
	for _, cfg := range summary.Configs {
		if cfg.Status == model.StatusActive && activeConnection[cfg.ConnectionID] {
			out = append(out, IssuedConfigItem(cfg))
		}
	}
	return out
}

func activeConfigProfiles(token model.AccessToken, summary model.AccountSummary) []ConfigProfile {
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

func IssuedConfigs(configs []model.IssuedConfig) []IssuedConfig {
	return converter.ToIssuedConfigs(configs)
}

func IssuedConfigItem(cfg model.IssuedConfig) IssuedConfig {
	return converter.ToIssuedConfig(cfg)
}

func ConfigProfiles(profiles []model.ConfigProfile) []ConfigProfile {
	return converter.ToConfigProfiles(profiles)
}

func ConfigProfileItem(profile model.ConfigProfile) ConfigProfile {
	return converter.ToConfigProfile(profile)
}

func Nodes(nodes []model.Node) []Node {
	return converter.ToNodes(nodes)
}

func NodeItem(n model.Node) Node {
	return converter.ToNode(n)
}

func AccessTokens(tokens []model.AccessToken) []AccessToken {
	return converter.ToAccessTokens(tokens)
}

func AccessTokenItem(t model.AccessToken) AccessToken {
	return converter.ToAccessToken(t)
}

func PolicyTags(tags []model.PolicyTag) []PolicyTag {
	return converter.ToPolicyTags(tags)
}

func PolicyTagItem(tag model.PolicyTag) PolicyTag {
	return converter.ToPolicyTag(tag)
}

func AccountPolicyTags(tags []model.AccountPolicyTag) []AccountPolicyTag {
	return converter.ToAccountPolicyTags(tags)
}

func AccountPolicyTagItem(tag model.AccountPolicyTag) AccountPolicyTag {
	return converter.ToAccountPolicyTag(tag)
}

func AuditLogs(logs []model.AuditLog) []AuditLog {
	return converter.ToAuditLogs(logs)
}

func AuditLogItem(log model.AuditLog) AuditLog {
	return converter.ToAuditLog(log)
}
