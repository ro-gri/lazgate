package store

import (
	"time"

	"laz/internal/model"
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

	Summary(accountID string) (model.AccountSummary, error)
	ClientSummary(accountID, clientID string) (model.AccountSummary, error)
}
