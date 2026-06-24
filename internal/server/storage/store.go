package store

import (
	"context"
	"time"

	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"
)

type Store interface {
	agentprotocol.InstallationRecordStore
	Close() error

	CreateAccount(model.Account) (model.Account, error)
	GetAccount(id string) (model.Account, error)
	ListAccounts() []model.Account
	UpdateAccountStatus(id string, status model.AccountStatus) (model.Account, error)

	CreateNode(model.Node) (model.Node, error)
	CreateNodeInstance(model.NodeInstance) (model.NodeInstance, error)
	CreateNodeWithInstanceAndAgentInstallation(model.Node, model.NodeInstance, model.AgentInstallation) (model.Node, model.NodeInstance, model.AgentInstallation, error)
	ListNodes() []model.Node
	GetNode(id string) (model.Node, error)
	SoftDeleteNode(id string) (model.Node, error)
	GetNodeInstance(id string) (model.NodeInstance, error)
	GetActiveNodeInstance(nodeID string) (model.NodeInstance, error)
	ListNodeInstances() []model.NodeInstance
	CreateAgentInstallation(model.AgentInstallation) (model.AgentInstallation, error)
	GetAgentInstallation(id string) (model.AgentInstallation, error)
	ListAgentInstallations() []model.AgentInstallation
	GetActiveAgentInstallationByCertificateFingerprint(fingerprint string) (model.AgentInstallation, error)

	CreateConnectionBundle(model.ConnectionBundle) (model.ConnectionBundle, error)
	GetConnection(id string) (model.Connection, error)
	UpdateConnectionDesiredStatus(id string, status model.ConnectionStatus) (model.Connection, error)
	FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.Connection, error)
	ListConnections() []model.Connection
	CountActiveConnectionsForAccount(accountID string) (int, error)
	ListHysteriaSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[string], error)
	CreateHysteria2NodeConfig(model.Hysteria2NodeConfig) (model.Hysteria2NodeConfig, error)
	GetHysteria2NodeConfig(nodeID string) (model.Hysteria2NodeConfig, error)
	PutHysteriaFileSet(model.HysteriaFileSet) error
	GetHysteriaFileSet(string) (model.HysteriaFileSet, error)
	GetHysteriaFileSetManifest(string) (string, error)
	PruneHysteriaFileSets(time.Time) error
	UpdateHysteria2NodeConfig(model.Hysteria2NodeConfig, int64, []model.IssuedConfig) error
	SetHysteria2ConfigResult(string, int64, string) error
	GetHysteria2ConnectionMaterial(connectionID string) (model.Hysteria2ConnectionMaterial, error)
	ListHysteria2ConfigSources(string) ([]model.Hysteria2ConfigSource, error)

	CreateIssuedConfig(model.IssuedConfig) (model.IssuedConfig, error)
	ListIssuedConfigs() []model.IssuedConfig

	CreateConfigProfile(model.ConfigProfile) (model.ConfigProfile, error)
	ListConfigProfiles() []model.ConfigProfile

	CreateAmneziaAPINodeConfig(model.AmneziaAPINodeConfig) (model.AmneziaAPINodeConfig, error)
	CreateAmneziaAPINode(model.Node, model.AmneziaAPINodeConfig) (model.Node, model.AmneziaAPINodeConfig, error)
	GetAmneziaAPINodeConfig(nodeID string) (model.AmneziaAPINodeConfig, error)
	GetAmneziaAPIConnectionRef(connectionID string) (model.AmneziaAPIConnectionRef, error)
	CreateNativeAmneziaWGNode(model.Node, model.AmneziaWGNodeConfig) (model.Node, model.AmneziaWGNodeConfig, error)
	CreateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error)
	GetAmneziaWGNodeConfig(nodeID string) (model.AmneziaWGNodeConfig, error)
	UpdateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig, int64, []model.IssuedConfig) error
	SetAmneziaWGConfigResult(string, int64, string) error
	GetAmneziaWGConnectionMaterial(connectionID string) (model.AmneziaWGConnectionMaterial, error)
	ListAmneziaWGConnectionMaterials(nodeID string) []model.AmneziaWGConnectionMaterial
	ListAmneziaWGSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial], error)

	CreateAuthIdentity(model.AuthIdentity) (model.AuthIdentity, error)
	GetAuthIdentity(id string) (model.AuthIdentity, error)
	UpdateAuthIdentity(model.AuthIdentity) (model.AuthIdentity, error)
	ListAdminIdentityProfiles() []model.AdminIdentityProfile
	FindActiveAdminIdentityByLogin(login string) (model.AuthIdentity, error)
	FindActiveUserIdentityByAccountID(accountID string) (model.AuthIdentity, error)
	CreateAdminInvite(model.AdminInvite) (model.AdminInvite, error)
	ListAdminInvites() []model.AdminInvite
	GetAdminInvite(id string) (model.AdminInvite, error)
	GetAdminInviteByTokenHash(tokenHash string) (model.AdminInvite, error)
	FindActiveSystemOwnerInvite(now time.Time) (model.AdminInvite, error)
	MarkAdminInviteUsed(inviteID string, identityID string) (model.AdminInvite, error)
	RevokeAdminInvite(inviteID string) (model.AdminInvite, error)
	CreateAuthCredential(model.AuthCredential) (model.AuthCredential, error)
	UpdateAuthCredential(model.AuthCredential) (model.AuthCredential, error)
	GetAuthCredential(identityID string, credentialType model.AuthCredentialType) (model.AuthCredential, error)
	CreateAuthSession(model.AuthSession) (model.AuthSession, error)
	GetAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error)
	GetActiveAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error)
	GetActiveAuthSessionByRefreshHash(refreshHash string) (model.AuthSession, error)
	GetAuthSessionByRefreshHashForRotation(refreshHash string, graceWindow time.Duration) (model.AuthSession, error)
	RotateAuthRefreshToken(oldRefreshHash string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time, graceWindow time.Duration) (model.RefreshTokenRotation, error)
	UpdateAuthSessionTokens(sessionID string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time) (model.AuthSession, error)
	UpdateAuthSessionPresence(sessionID string, lastSeenAt time.Time, onlineUntil time.Time) (model.AuthSession, error)
	RevokeAuthSessionsByIdentityID(identityID string) error
	RevokeOtherAuthSessionsByIdentityID(identityID string, keepSessionID string) error
	CreateAuthRecoveryCode(model.AuthRecoveryCode) (model.AuthRecoveryCode, error)
	ListAuthRecoveryCodes(identityID string) []model.AuthRecoveryCode
	MarkAuthRecoveryCodeUsed(id string) (model.AuthRecoveryCode, error)
	CreateAdminProfile(model.AdminProfile) (model.AdminProfile, error)
	GetAdminProfile(identityID string) (model.AdminProfile, error)
	UpdateAdminProfile(model.AdminProfile) (model.AdminProfile, error)

	CreateAccountPortalToken(model.AccountPortalToken) (model.AccountPortalToken, error)
	ListAccountPortalTokens() []model.AccountPortalToken
	GetAccountPortalTokenByHash(hash string) (model.AccountPortalToken, error)
	TouchAccountPortalToken(id string) error
	CreateConnectionSubscriptionToken(model.ConnectionSubscriptionToken) (model.ConnectionSubscriptionToken, error)
	ListConnectionSubscriptionTokens() []model.ConnectionSubscriptionToken
	GetConnectionSubscriptionTokenByHash(hash string) (model.ConnectionSubscriptionToken, error)
	TouchConnectionSubscriptionToken(id string) error

	CreatePolicyTag(model.PolicyTag) (model.PolicyTag, error)
	ListPolicyTags() []model.PolicyTag
	AssignPolicyTag(model.AccountPolicyTag) (model.AccountPolicyTag, error)
	ListAccountPolicyTags(accountID string) []model.AccountPolicyTag

	CreateShortLink(model.ShortLink) (model.ShortLink, error)
	GetShortLink(id string) (model.ShortLink, error)
	GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error)

	CreateAuditLog(model.AuditLog) (model.AuditLog, error)
	ListAuditLogs() []model.AuditLog

	UpsertNodeRuntime(model.NodeRuntime) error
	GetNodeRuntime(agentInstallationID string) (model.NodeRuntime, error)
	ListNodeRuntimes() []model.NodeRuntime
	UpsertNodeOnlineConnections(nodeInstanceID string, connections []model.NodeOnlineConnection) error
	ListNodeOnlineConnections(nodeInstanceID string) []model.NodeOnlineConnection
	ListAllNodeOnlineConnections() []model.NodeOnlineConnection
	ApplyUsageRecords(requestID, nodeInstanceID string, records []model.UsageRecord) ([]model.ConnectionUsageTotal, bool, error)
	ListUsageRecords() []model.UsageRecord
	NodeUsageSummary(instanceID string, fromMS, toMS int64, limit int) (model.NodeUsageSummary, error)
	ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord
	ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval
	CreateRuntimeCommand(model.RuntimeCommand) (model.RuntimeCommand, error)
	ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand
	ListRuntimeCommands(agentInstallationID string, limit int) []model.RuntimeCommand
	CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error

	Summary(accountID string) (model.AccountSummary, error)
}

var _ Store = (*SQLStore)(nil)
var _ Store = (*SecureStore)(nil)
