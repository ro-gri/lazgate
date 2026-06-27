package store

import (
	"time"

	"laz/internal/server/model"
)

type Store interface {
	CreateAccount(model.Account) (model.Account, error)
	GetAccount(id string) (model.Account, error)
	ListAccounts() []model.Account
	UpdateAccountStatus(id string, status model.Status) (model.Account, error)

	CreateNode(model.Node) (model.Node, error)
	ListNodes() []model.Node
	GetNode(id string) (model.Node, error)

	CreateClient(model.Client) (model.Client, error)
	GetClientForAccount(accountID, clientID string) (model.Client, error)
	UpdateClientStatus(id string, status model.Status) (model.Client, error)
	CountActiveClientsForAccount(accountID string) (int, error)
	CreateConnection(model.Connection) (model.Connection, error)
	GetConnection(id string) (model.Connection, error)
	UpdateConnectionStatus(id string, status model.Status, lastErr string) (model.Connection, error)
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.Connection, error)
	ListConnections() []model.Connection

	CreateIssuedConfig(model.IssuedConfig) (model.IssuedConfig, error)
	ListIssuedConfigs() []model.IssuedConfig
	RevokeConfigsForConnection(connectionID string) error

	CreateConfigProfile(model.ConfigProfile) (model.ConfigProfile, error)
	ListConfigProfiles() []model.ConfigProfile

	CreateAccessToken(model.AccessToken) (model.AccessToken, error)
	ListAccessTokens() []model.AccessToken
	GetAccessTokenByHash(hash string) (model.AccessToken, error)
	TouchAccessToken(id string) error

	CreateAdminSession(model.AdminSession) (model.AdminSession, error)
	GetAdminSessionByHash(hash string) (model.AdminSession, error)
	TouchAdminSession(id string) error
	RevokeAdminSession(id string) error

	UpsertClientCredential(model.ClientCredential) (model.ClientCredential, error)
	GetClientCredential(accountID string) (model.ClientCredential, error)
	UpdateClientCredentialAuthState(accountID string, failedAttempts int, lockedUntil time.Time) (model.ClientCredential, error)

	CreateClientSession(model.ClientSession) (model.ClientSession, error)
	GetClientSessionByHash(hash string) (model.ClientSession, error)
	TouchClientSession(id string) error
	RevokeClientSession(id string) error
	RevokeClientSessionsForAccount(accountID string) error

	CreatePolicyTag(model.PolicyTag) (model.PolicyTag, error)
	ListPolicyTags() []model.PolicyTag
	AssignPolicyTag(model.AccountPolicyTag) (model.AccountPolicyTag, error)
	ListAccountPolicyTags(accountID string) []model.AccountPolicyTag

	CreateShortLink(model.ShortLink) (model.ShortLink, error)
	GetShortLink(id string) (model.ShortLink, error)
	GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error)

	CreateAuditLog(model.AuditLog) (model.AuditLog, error)
	ListAuditLogs() []model.AuditLog

	CreateEvent(model.Event) (model.Event, error)
	ListPendingEvents(topic string, limit int) []model.Event
	MarkEventDelivered(id string, deliveredAtMS int64) error
	ExpireEvents(nowMS int64) error

	UpsertNodeRuntime(model.NodeRuntime) error
	GetNodeRuntime(nodeID string) (model.NodeRuntime, error)
	ListNodeRuntimes() []model.NodeRuntime
	UpsertNodeOnlineClients(nodeID string, clients []model.NodeOnlineClient) error
	ListNodeOnlineClients(nodeID string) []model.NodeOnlineClient
	ListAllNodeOnlineClients() []model.NodeOnlineClient
	CreateUsageBatch(model.UsageBatch, []model.UsageRecord) (bool, error)
	ListUsageRecords() []model.UsageRecord
	ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord
	ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval
	CreateRuntimeCommand(model.RuntimeCommand) (model.RuntimeCommand, error)
	ListPendingRuntimeCommands(nodeID string) []model.RuntimeCommand
	ListRuntimeCommands(nodeID string, limit int) []model.RuntimeCommand
	CompleteRuntimeCommand(id string, status model.Status, result, errMsg string) error

	Summary(accountID string) (model.AccountSummary, error)
	ClientSummary(accountID, clientID string) (model.AccountSummary, error)
}
