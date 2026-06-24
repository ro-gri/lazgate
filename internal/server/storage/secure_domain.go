package store

import (
	"context"

	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"
)

func (s *SecureStore) CreateAccount(v model.Account) (model.Account, error) {
	return s.inner.CreateAccount(v)
}

func (s *SecureStore) GetAccount(id string) (model.Account, error) { return s.inner.GetAccount(id) }
func (s *SecureStore) ListAccounts() []model.Account               { return s.inner.ListAccounts() }
func (s *SecureStore) UpdateAccountStatus(id string, status model.AccountStatus) (model.Account, error) {
	return s.inner.UpdateAccountStatus(id, status)
}

func (s *SecureStore) CreateNode(v model.Node) (model.Node, error) { return s.inner.CreateNode(v) }
func (s *SecureStore) ListNodes() []model.Node                     { return s.inner.ListNodes() }
func (s *SecureStore) GetNode(id string) (model.Node, error)       { return s.inner.GetNode(id) }
func (s *SecureStore) SoftDeleteNode(id string) (model.Node, error) {
	return s.inner.SoftDeleteNode(id)
}

func (s *SecureStore) CreateNodeInstance(v model.NodeInstance) (model.NodeInstance, error) {
	v.SSHKey = s.encrypt(v.SSHKey)
	created, err := s.inner.CreateNodeInstance(v)
	created.SSHKey = s.decrypt(created.SSHKey)
	return created, err
}

func (s *SecureStore) CreateNodeWithInstanceAndAgentInstallation(node model.Node, instance model.NodeInstance, installation model.AgentInstallation) (model.Node, model.NodeInstance, model.AgentInstallation, error) {
	instance.SSHKey = s.encrypt(instance.SSHKey)
	node, instance, installation, err := s.inner.CreateNodeWithInstanceAndAgentInstallation(node, instance, installation)
	instance.SSHKey = s.decrypt(instance.SSHKey)
	return node, instance, installation, err
}

func (s *SecureStore) GetNodeInstance(id string) (model.NodeInstance, error) {
	v, err := s.inner.GetNodeInstance(id)
	v.SSHKey = s.decrypt(v.SSHKey)
	return v, err
}

func (s *SecureStore) GetActiveNodeInstance(nodeID string) (model.NodeInstance, error) {
	v, err := s.inner.GetActiveNodeInstance(nodeID)
	v.SSHKey = s.decrypt(v.SSHKey)
	return v, err
}

func (s *SecureStore) ListNodeInstances() []model.NodeInstance {
	items := s.inner.ListNodeInstances()
	for i := range items {
		items[i].SSHKey = s.decrypt(items[i].SSHKey)
	}
	return items
}

func (s *SecureStore) CreateAgentInstallation(v model.AgentInstallation) (model.AgentInstallation, error) {
	return s.inner.CreateAgentInstallation(v)
}

func (s *SecureStore) GetAgentInstallation(id string) (model.AgentInstallation, error) {
	return s.inner.GetAgentInstallation(id)
}

func (s *SecureStore) ListAgentInstallations() []model.AgentInstallation {
	return s.inner.ListAgentInstallations()
}

func (s *SecureStore) GetActiveAgentInstallationByCertificateFingerprint(fingerprint string) (model.AgentInstallation, error) {
	return s.inner.GetActiveAgentInstallationByCertificateFingerprint(fingerprint)
}

func (s *SecureStore) LookupActiveAgentInstallation(fingerprint string) (agentprotocol.InstallationRecord, error) {
	return s.inner.LookupActiveAgentInstallation(fingerprint)
}

func (s *SecureStore) CreateConnectionBundle(v model.ConnectionBundle) (model.ConnectionBundle, error) {
	if v.Hysteria2Material != nil {
		material := *v.Hysteria2Material
		material.Password = s.encrypt(material.Password)
		v.Hysteria2Material = &material
	}
	if v.AmneziaWGMaterial != nil {
		material := *v.AmneziaWGMaterial
		material.ClientPrivateKey = s.encrypt(material.ClientPrivateKey)
		material.PresharedKey = s.encrypt(material.PresharedKey)
		v.AmneziaWGMaterial = &material
	}
	for i := range v.Configs {
		v.Configs[i] = s.encryptConfig(v.Configs[i])
	}
	created, err := s.inner.CreateConnectionBundle(v)
	if created.Hysteria2Material != nil {
		created.Hysteria2Material.Password = s.decrypt(created.Hysteria2Material.Password)
	}
	if created.AmneziaWGMaterial != nil {
		created.AmneziaWGMaterial.ClientPrivateKey = s.decrypt(created.AmneziaWGMaterial.ClientPrivateKey)
		created.AmneziaWGMaterial.PresharedKey = s.decrypt(created.AmneziaWGMaterial.PresharedKey)
	}
	for i := range created.Configs {
		created.Configs[i] = s.decryptConfig(created.Configs[i])
	}
	return created, err
}
func (s *SecureStore) GetConnection(id string) (model.Connection, error) {
	return s.inner.GetConnection(id)
}
func (s *SecureStore) UpdateConnectionDesiredStatus(id string, status model.ConnectionStatus) (model.Connection, error) {
	return s.inner.UpdateConnectionDesiredStatus(id, status)
}
func (s *SecureStore) FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, version int64) ([]model.Connection, error) {
	return s.inner.FinalizeConnectionsForAuthSnapshot(nodeID, accountID, version)
}
func (s *SecureStore) ListConnections() []model.Connection { return s.inner.ListConnections() }
func (s *SecureStore) CountActiveConnectionsForAccount(accountID string) (int, error) {
	return s.inner.CountActiveConnectionsForAccount(accountID)
}

func (s *SecureStore) CreateHysteria2NodeConfig(v model.Hysteria2NodeConfig) (model.Hysteria2NodeConfig, error) {
	v.SettingsJSON = s.encrypt(v.SettingsJSON)
	v.ObfsPassword = s.encrypt(v.ObfsPassword)
	v, err := s.inner.CreateHysteria2NodeConfig(v)
	v.ObfsPassword = s.decrypt(v.ObfsPassword)
	v.SettingsJSON = s.decrypt(v.SettingsJSON)
	return v, err
}
func (s *SecureStore) GetHysteria2NodeConfig(nodeID string) (model.Hysteria2NodeConfig, error) {
	v, err := s.inner.GetHysteria2NodeConfig(nodeID)
	v.ObfsPassword = s.decrypt(v.ObfsPassword)
	v.SettingsJSON = s.decrypt(v.SettingsJSON)
	return v, err
}

func (s *SecureStore) UpdateHysteria2NodeConfig(v model.Hysteria2NodeConfig, expected int64, configs []model.IssuedConfig) error {
	v.SettingsJSON = s.encrypt(v.SettingsJSON)
	v.ObfsPassword = s.encrypt(v.ObfsPassword)
	encrypted := make([]model.IssuedConfig, len(configs))
	for i, c := range configs {
		encrypted[i] = s.encryptConfig(c)
	}
	return s.inner.UpdateHysteria2NodeConfig(v, expected, encrypted)
}
func (s *SecureStore) SetHysteria2ConfigResult(id string, version int64, errorID string) error {
	return s.inner.SetHysteria2ConfigResult(id, version, errorID)
}
func (s *SecureStore) GetHysteria2ConnectionMaterial(connectionID string) (model.Hysteria2ConnectionMaterial, error) {
	v, err := s.inner.GetHysteria2ConnectionMaterial(connectionID)
	v.Password = s.decrypt(v.Password)
	return v, err
}

func (s *SecureStore) ListHysteria2ConfigSources(id string) ([]model.Hysteria2ConfigSource, error) {
	items, err := s.inner.ListHysteria2ConfigSources(id)
	for i := range items {
		items[i].Password = s.decrypt(items[i].Password)
	}
	return items, err
}
func (s *SecureStore) ListHysteriaSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[string], error) {
	return s.inner.ListHysteriaSyncConnections(ctx, nodeID, fromConnectionID)
}

func (s *SecureStore) CreateIssuedConfig(v model.IssuedConfig) (model.IssuedConfig, error) {
	v, err := s.inner.CreateIssuedConfig(s.encryptConfig(v))
	return s.decryptConfig(v), err
}
func (s *SecureStore) ListIssuedConfigs() []model.IssuedConfig {
	items := s.inner.ListIssuedConfigs()
	for i := range items {
		items[i] = s.decryptConfig(items[i])
	}
	return items
}
func (s *SecureStore) CreateConfigProfile(v model.ConfigProfile) (model.ConfigProfile, error) {
	return s.inner.CreateConfigProfile(v)
}
func (s *SecureStore) ListConfigProfiles() []model.ConfigProfile {
	return s.inner.ListConfigProfiles()
}

func (s *SecureStore) CreateAmneziaAPINodeConfig(v model.AmneziaAPINodeConfig) (model.AmneziaAPINodeConfig, error) {
	v.APIKey = s.encrypt(v.APIKey)
	v, err := s.inner.CreateAmneziaAPINodeConfig(v)
	v.APIKey = s.decrypt(v.APIKey)
	return v, err
}
func (s *SecureStore) CreateAmneziaAPINode(node model.Node, config model.AmneziaAPINodeConfig) (model.Node, model.AmneziaAPINodeConfig, error) {
	config.APIKey = s.encrypt(config.APIKey)
	node, config, err := s.inner.CreateAmneziaAPINode(node, config)
	config.APIKey = s.decrypt(config.APIKey)
	return node, config, err
}
func (s *SecureStore) GetAmneziaAPINodeConfig(nodeID string) (model.AmneziaAPINodeConfig, error) {
	v, err := s.inner.GetAmneziaAPINodeConfig(nodeID)
	v.APIKey = s.decrypt(v.APIKey)
	return v, err
}
func (s *SecureStore) GetAmneziaAPIConnectionRef(connectionID string) (model.AmneziaAPIConnectionRef, error) {
	return s.inner.GetAmneziaAPIConnectionRef(connectionID)
}
func (s *SecureStore) CreateNativeAmneziaWGNode(node model.Node, config model.AmneziaWGNodeConfig) (model.Node, model.AmneziaWGNodeConfig, error) {
	config.HeaderProtectionKey = s.encrypt(config.HeaderProtectionKey)
	config.ServerPrivateKey = s.encrypt(config.ServerPrivateKey)
	node, config, err := s.inner.CreateNativeAmneziaWGNode(node, config)
	config.HeaderProtectionKey = s.decrypt(config.HeaderProtectionKey)
	config.ServerPrivateKey = s.decrypt(config.ServerPrivateKey)
	return node, config, err
}
func (s *SecureStore) CreateAmneziaWGNodeConfig(v model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error) {
	v.HeaderProtectionKey = s.encrypt(v.HeaderProtectionKey)
	v.ServerPrivateKey = s.encrypt(v.ServerPrivateKey)
	v, err := s.inner.CreateAmneziaWGNodeConfig(v)
	v.HeaderProtectionKey = s.decrypt(v.HeaderProtectionKey)
	v.ServerPrivateKey = s.decrypt(v.ServerPrivateKey)
	return v, err
}
func (s *SecureStore) GetAmneziaWGNodeConfig(nodeID string) (model.AmneziaWGNodeConfig, error) {
	v, err := s.inner.GetAmneziaWGNodeConfig(nodeID)
	v.HeaderProtectionKey = s.decrypt(v.HeaderProtectionKey)
	v.ServerPrivateKey = s.decrypt(v.ServerPrivateKey)
	return v, err
}

func (s *SecureStore) UpdateAmneziaWGNodeConfig(v model.AmneziaWGNodeConfig, expected int64, configs []model.IssuedConfig) error {
	v.HeaderProtectionKey = s.encrypt(v.HeaderProtectionKey)
	v.ServerPrivateKey = s.encrypt(v.ServerPrivateKey)
	items := append([]model.IssuedConfig(nil), configs...)
	for i := range items {
		items[i].Config = s.encrypt(items[i].Config)
	}
	return s.inner.UpdateAmneziaWGNodeConfig(v, expected, items)
}

func (s *SecureStore) SetAmneziaWGConfigResult(nodeID string, version int64, errorID string) error {
	return s.inner.SetAmneziaWGConfigResult(nodeID, version, errorID)
}
func (s *SecureStore) GetAmneziaWGConnectionMaterial(connectionID string) (model.AmneziaWGConnectionMaterial, error) {
	v, err := s.inner.GetAmneziaWGConnectionMaterial(connectionID)
	v.ClientPrivateKey = s.decrypt(v.ClientPrivateKey)
	v.PresharedKey = s.decrypt(v.PresharedKey)
	return v, err
}
func (s *SecureStore) ListAmneziaWGConnectionMaterials(nodeID string) []model.AmneziaWGConnectionMaterial {
	items := s.inner.ListAmneziaWGConnectionMaterials(nodeID)
	for i := range items {
		items[i].ClientPrivateKey = s.decrypt(items[i].ClientPrivateKey)
		items[i].PresharedKey = s.decrypt(items[i].PresharedKey)
	}
	return items
}
func (s *SecureStore) ListAmneziaWGSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial], error) {
	items, err := s.inner.ListAmneziaWGSyncConnections(ctx, nodeID, fromConnectionID)
	for i := range items {
		items[i].Material.NodeConfig.ServerPrivateKey = s.decrypt(items[i].Material.NodeConfig.ServerPrivateKey)
		items[i].Material.NodeConfig.HeaderProtectionKey = s.decrypt(items[i].Material.NodeConfig.HeaderProtectionKey)
		items[i].Material.Material.ClientPrivateKey = s.decrypt(items[i].Material.Material.ClientPrivateKey)
		items[i].Material.Material.PresharedKey = s.decrypt(items[i].Material.Material.PresharedKey)
	}
	return items, err
}

func (s *SecureStore) CreateAccountPortalToken(v model.AccountPortalToken) (model.AccountPortalToken, error) {
	v.Token = s.encrypt(v.Token)
	v, err := s.inner.CreateAccountPortalToken(v)
	v.Token = s.decrypt(v.Token)
	return v, err
}
func (s *SecureStore) ListAccountPortalTokens() []model.AccountPortalToken {
	items := s.inner.ListAccountPortalTokens()
	for i := range items {
		items[i].Token = s.decrypt(items[i].Token)
	}
	return items
}
func (s *SecureStore) GetAccountPortalTokenByHash(hash string) (model.AccountPortalToken, error) {
	v, err := s.inner.GetAccountPortalTokenByHash(hash)
	v.Token = s.decrypt(v.Token)
	return v, err
}
func (s *SecureStore) TouchAccountPortalToken(id string) error {
	return s.inner.TouchAccountPortalToken(id)
}
func (s *SecureStore) CreateConnectionSubscriptionToken(v model.ConnectionSubscriptionToken) (model.ConnectionSubscriptionToken, error) {
	v.Token = s.encrypt(v.Token)
	v, err := s.inner.CreateConnectionSubscriptionToken(v)
	v.Token = s.decrypt(v.Token)
	return v, err
}
func (s *SecureStore) ListConnectionSubscriptionTokens() []model.ConnectionSubscriptionToken {
	items := s.inner.ListConnectionSubscriptionTokens()
	for i := range items {
		items[i].Token = s.decrypt(items[i].Token)
	}
	return items
}
func (s *SecureStore) GetConnectionSubscriptionTokenByHash(hash string) (model.ConnectionSubscriptionToken, error) {
	v, err := s.inner.GetConnectionSubscriptionTokenByHash(hash)
	v.Token = s.decrypt(v.Token)
	return v, err
}
func (s *SecureStore) TouchConnectionSubscriptionToken(id string) error {
	return s.inner.TouchConnectionSubscriptionToken(id)
}

func (s *SecureStore) UpsertNodeRuntime(v model.NodeRuntime) error {
	return s.inner.UpsertNodeRuntime(v)
}
func (s *SecureStore) GetNodeRuntime(id string) (model.NodeRuntime, error) {
	return s.inner.GetNodeRuntime(id)
}
func (s *SecureStore) ListNodeRuntimes() []model.NodeRuntime { return s.inner.ListNodeRuntimes() }
func (s *SecureStore) UpsertNodeOnlineConnections(id string, items []model.NodeOnlineConnection) error {
	return s.inner.UpsertNodeOnlineConnections(id, items)
}
func (s *SecureStore) ListNodeOnlineConnections(id string) []model.NodeOnlineConnection {
	return s.inner.ListNodeOnlineConnections(id)
}
func (s *SecureStore) ListAllNodeOnlineConnections() []model.NodeOnlineConnection {
	return s.inner.ListAllNodeOnlineConnections()
}
func (s *SecureStore) ApplyUsageRecords(requestID, nodeInstanceID string, records []model.UsageRecord) ([]model.ConnectionUsageTotal, bool, error) {
	return s.inner.ApplyUsageRecords(requestID, nodeInstanceID, records)
}
func (s *SecureStore) ListUsageRecords() []model.UsageRecord { return s.inner.ListUsageRecords() }
func (s *SecureStore) NodeUsageSummary(instanceID string, fromMS, toMS int64, limit int) (model.NodeUsageSummary, error) {
	return s.inner.NodeUsageSummary(instanceID, fromMS, toMS, limit)
}
func (s *SecureStore) ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord {
	return s.inner.ListUsageRecordsRange(fromMS, toMS, limit)
}
func (s *SecureStore) ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval {
	return s.inner.ListNodeStatusIntervals(fromMS, toMS)
}

func (s *SecureStore) Summary(accountID string) (model.AccountSummary, error) {
	summary, err := s.inner.Summary(accountID)
	for i := range summary.Configs {
		summary.Configs[i] = s.decryptConfig(summary.Configs[i])
	}
	return summary, err
}
