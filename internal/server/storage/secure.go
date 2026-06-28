package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"laz/internal/server/model"
)

const encryptedPrefix = "enc:v1:"

type SecureStore struct {
	inner Store
	aead  cipher.AEAD
}

func WrapSecrets(inner Store, key string) (Store, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return inner, nil
	}
	raw, err := secretKeyBytes(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecureStore{inner: inner, aead: aead}, nil
}

func secretKeyBytes(value string) ([]byte, error) {
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	sum := sha256.Sum256([]byte(value))
	return sum[:], nil
}

func (s *SecureStore) encrypt(value string) string {
	if value == "" || strings.HasPrefix(value, encryptedPrefix) {
		return value
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	sealed := s.aead.Seal(nil, nonce, []byte(value), nil)
	payload := append(nonce, sealed...)
	return encryptedPrefix + base64.RawStdEncoding.EncodeToString(payload)
}

func (s *SecureStore) decrypt(value string) string {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil || len(payload) < s.aead.NonceSize() {
		return ""
	}
	nonce := payload[:s.aead.NonceSize()]
	sealed := payload[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func (s *SecureStore) encryptNode(n model.Node) model.Node {
	n.APIKey = s.encrypt(n.APIKey)
	n.SSHKeyPath = s.encrypt(n.SSHKeyPath)
	return n
}

func (s *SecureStore) decryptNode(n model.Node) model.Node {
	n.APIKey = s.decrypt(n.APIKey)
	n.SSHKeyPath = s.decrypt(n.SSHKeyPath)
	return n
}

func (s *SecureStore) encryptConfig(c model.IssuedConfig) model.IssuedConfig {
	c.Config = s.encrypt(c.Config)
	return c
}

func (s *SecureStore) decryptConfig(c model.IssuedConfig) model.IssuedConfig {
	c.Config = s.decrypt(c.Config)
	return c
}

func (s *SecureStore) encryptAccessToken(t model.AccessToken) model.AccessToken {
	t.Token = s.encrypt(t.Token)
	return t
}

func (s *SecureStore) decryptAccessToken(t model.AccessToken) model.AccessToken {
	t.Token = s.decrypt(t.Token)
	return t
}

func (s *SecureStore) CreateAccount(u model.Account) (model.Account, error) {
	return s.inner.CreateAccount(u)
}
func (s *SecureStore) GetAccount(id string) (model.Account, error) { return s.inner.GetAccount(id) }
func (s *SecureStore) ListAccounts() []model.Account               { return s.inner.ListAccounts() }
func (s *SecureStore) UpdateAccountStatus(id string, status model.Status) (model.Account, error) {
	return s.inner.UpdateAccountStatus(id, status)
}

func (s *SecureStore) CreateNode(n model.Node) (model.Node, error) {
	n, err := s.inner.CreateNode(s.encryptNode(n))
	return s.decryptNode(n), err
}

func (s *SecureStore) ListNodes() []model.Node {
	nodes := s.inner.ListNodes()
	for i := range nodes {
		nodes[i] = s.decryptNode(nodes[i])
	}
	return nodes
}

func (s *SecureStore) GetNode(id string) (model.Node, error) {
	n, err := s.inner.GetNode(id)
	return s.decryptNode(n), err
}

func (s *SecureStore) CreateClient(d model.Client) (model.Client, error) {
	return s.inner.CreateClient(d)
}
func (s *SecureStore) GetClientForAccount(accountID, clientID string) (model.Client, error) {
	return s.inner.GetClientForAccount(accountID, clientID)
}
func (s *SecureStore) UpdateClientStatus(id string, status model.Status) (model.Client, error) {
	return s.inner.UpdateClientStatus(id, status)
}
func (s *SecureStore) CountActiveClientsForAccount(accountID string) (int, error) {
	return s.inner.CountActiveClientsForAccount(accountID)
}
func (s *SecureStore) CreateConnection(a model.Connection) (model.Connection, error) {
	return s.inner.CreateConnection(a)
}
func (s *SecureStore) GetConnection(id string) (model.Connection, error) {
	return s.inner.GetConnection(id)
}
func (s *SecureStore) UpdateConnectionStatus(id string, status model.Status, lastErr string) (model.Connection, error) {
	return s.inner.UpdateConnectionStatus(id, status, lastErr)
}
func (s *SecureStore) FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedSnapshotVersionMS int64) ([]model.FinalizedConnection, error) {
	return s.inner.FinalizeConnectionsForAuthSnapshot(nodeID, accountID, appliedSnapshotVersionMS)
}
func (s *SecureStore) ListConnections() []model.Connection { return s.inner.ListConnections() }

func (s *SecureStore) CreateIssuedConfig(c model.IssuedConfig) (model.IssuedConfig, error) {
	c, err := s.inner.CreateIssuedConfig(s.encryptConfig(c))
	return s.decryptConfig(c), err
}

func (s *SecureStore) ListIssuedConfigs() []model.IssuedConfig {
	configs := s.inner.ListIssuedConfigs()
	for i := range configs {
		configs[i] = s.decryptConfig(configs[i])
	}
	return configs
}

func (s *SecureStore) RevokeConfigsForConnection(connectionID string) error {
	return s.inner.RevokeConfigsForConnection(connectionID)
}

func (s *SecureStore) CreateConfigProfile(p model.ConfigProfile) (model.ConfigProfile, error) {
	return s.inner.CreateConfigProfile(p)
}
func (s *SecureStore) ListConfigProfiles() []model.ConfigProfile {
	return s.inner.ListConfigProfiles()
}

func (s *SecureStore) CreateAccessToken(t model.AccessToken) (model.AccessToken, error) {
	t, err := s.inner.CreateAccessToken(s.encryptAccessToken(t))
	return s.decryptAccessToken(t), err
}

func (s *SecureStore) ListAccessTokens() []model.AccessToken {
	tokens := s.inner.ListAccessTokens()
	for i := range tokens {
		tokens[i] = s.decryptAccessToken(tokens[i])
	}
	return tokens
}

func (s *SecureStore) GetAccessTokenByHash(hash string) (model.AccessToken, error) {
	t, err := s.inner.GetAccessTokenByHash(hash)
	return s.decryptAccessToken(t), err
}
func (s *SecureStore) TouchAccessToken(id string) error { return s.inner.TouchAccessToken(id) }

func (s *SecureStore) CreateAdminSession(session model.AdminSession) (model.AdminSession, error) {
	session.Token = ""
	session.CSRFToken = ""
	return s.inner.CreateAdminSession(session)
}
func (s *SecureStore) GetAdminSessionByHash(hash string) (model.AdminSession, error) {
	return s.inner.GetAdminSessionByHash(hash)
}
func (s *SecureStore) TouchAdminSession(id string) error  { return s.inner.TouchAdminSession(id) }
func (s *SecureStore) RevokeAdminSession(id string) error { return s.inner.RevokeAdminSession(id) }

func (s *SecureStore) UpsertClientCredential(c model.ClientCredential) (model.ClientCredential, error) {
	return s.inner.UpsertClientCredential(c)
}
func (s *SecureStore) GetClientCredential(accountID string) (model.ClientCredential, error) {
	return s.inner.GetClientCredential(accountID)
}
func (s *SecureStore) UpdateClientCredentialAuthState(accountID string, failedAttempts int, lockedUntil time.Time) (model.ClientCredential, error) {
	return s.inner.UpdateClientCredentialAuthState(accountID, failedAttempts, lockedUntil)
}

func (s *SecureStore) CreateClientSession(session model.ClientSession) (model.ClientSession, error) {
	session.Token = ""
	return s.inner.CreateClientSession(session)
}
func (s *SecureStore) GetClientSessionByHash(hash string) (model.ClientSession, error) {
	return s.inner.GetClientSessionByHash(hash)
}
func (s *SecureStore) TouchClientSession(id string) error  { return s.inner.TouchClientSession(id) }
func (s *SecureStore) RevokeClientSession(id string) error { return s.inner.RevokeClientSession(id) }
func (s *SecureStore) RevokeClientSessionsForAccount(accountID string) error {
	return s.inner.RevokeClientSessionsForAccount(accountID)
}

func (s *SecureStore) CreatePolicyTag(tag model.PolicyTag) (model.PolicyTag, error) {
	return s.inner.CreatePolicyTag(tag)
}
func (s *SecureStore) ListPolicyTags() []model.PolicyTag { return s.inner.ListPolicyTags() }
func (s *SecureStore) AssignPolicyTag(tag model.AccountPolicyTag) (model.AccountPolicyTag, error) {
	return s.inner.AssignPolicyTag(tag)
}
func (s *SecureStore) ListAccountPolicyTags(accountID string) []model.AccountPolicyTag {
	return s.inner.ListAccountPolicyTags(accountID)
}

func (s *SecureStore) CreateShortLink(link model.ShortLink) (model.ShortLink, error) {
	return s.inner.CreateShortLink(link)
}
func (s *SecureStore) GetShortLink(id string) (model.ShortLink, error) {
	return s.inner.GetShortLink(id)
}
func (s *SecureStore) GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error) {
	return s.inner.GetShortLinkByTokenProfile(tokenID, profile)
}

func (s *SecureStore) CreateAuditLog(log model.AuditLog) (model.AuditLog, error) {
	return s.inner.CreateAuditLog(log)
}
func (s *SecureStore) ListAuditLogs() []model.AuditLog { return s.inner.ListAuditLogs() }

func (s *SecureStore) CreateEvent(event model.Event) (model.Event, error) {
	return s.inner.CreateEvent(event)
}
func (s *SecureStore) ListPendingEvents(topic string, limit int) []model.Event {
	return s.inner.ListPendingEvents(topic, limit)
}
func (s *SecureStore) MarkEventDelivered(id string, deliveredAtMS int64) error {
	return s.inner.MarkEventDelivered(id, deliveredAtMS)
}
func (s *SecureStore) ExpireEvents(nowMS int64) error { return s.inner.ExpireEvents(nowMS) }

func (s *SecureStore) UpsertNodeRuntime(runtime model.NodeRuntime) error {
	return s.inner.UpsertNodeRuntime(runtime)
}
func (s *SecureStore) GetNodeRuntime(nodeID string) (model.NodeRuntime, error) {
	return s.inner.GetNodeRuntime(nodeID)
}
func (s *SecureStore) ListNodeRuntimes() []model.NodeRuntime {
	return s.inner.ListNodeRuntimes()
}
func (s *SecureStore) UpsertNodeOnlineClients(nodeID string, clients []model.NodeOnlineClient) error {
	return s.inner.UpsertNodeOnlineClients(nodeID, clients)
}
func (s *SecureStore) ListNodeOnlineClients(nodeID string) []model.NodeOnlineClient {
	return s.inner.ListNodeOnlineClients(nodeID)
}
func (s *SecureStore) ListAllNodeOnlineClients() []model.NodeOnlineClient {
	return s.inner.ListAllNodeOnlineClients()
}
func (s *SecureStore) CreateUsageBatch(batch model.UsageBatch, records []model.UsageRecord) (bool, error) {
	return s.inner.CreateUsageBatch(batch, records)
}
func (s *SecureStore) ListUsageRecords() []model.UsageRecord {
	return s.inner.ListUsageRecords()
}
func (s *SecureStore) ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord {
	return s.inner.ListUsageRecordsRange(fromMS, toMS, limit)
}
func (s *SecureStore) ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval {
	return s.inner.ListNodeStatusIntervals(fromMS, toMS)
}
func (s *SecureStore) CreateRuntimeCommand(command model.RuntimeCommand) (model.RuntimeCommand, error) {
	return s.inner.CreateRuntimeCommand(command)
}
func (s *SecureStore) ListPendingRuntimeCommands(nodeID string) []model.RuntimeCommand {
	return s.inner.ListPendingRuntimeCommands(nodeID)
}
func (s *SecureStore) ListRuntimeCommands(nodeID string, limit int) []model.RuntimeCommand {
	return s.inner.ListRuntimeCommands(nodeID, limit)
}
func (s *SecureStore) CompleteRuntimeCommand(id string, status model.Status, result, errMsg string) error {
	return s.inner.CompleteRuntimeCommand(id, status, result, errMsg)
}

func (s *SecureStore) Summary(accountID string) (model.AccountSummary, error) {
	summary, err := s.inner.Summary(accountID)
	return s.decryptSummary(summary, err)
}

func (s *SecureStore) ClientSummary(accountID, clientID string) (model.AccountSummary, error) {
	summary, err := s.inner.ClientSummary(accountID, clientID)
	return s.decryptSummary(summary, err)
}

func (s *SecureStore) decryptSummary(summary model.AccountSummary, err error) (model.AccountSummary, error) {
	if err != nil {
		return model.AccountSummary{}, err
	}
	for i := range summary.Connections {
		summary.Connections[i].Node = s.decryptNode(summary.Connections[i].Node)
	}
	for i := range summary.Configs {
		summary.Configs[i] = s.decryptConfig(summary.Configs[i])
	}
	return summary, nil
}
