package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"
)

func defaultNode(v model.Node) model.Node {
	t := now()
	if v.ID == "" {
		v.ID = NewID("nod")
	}
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	v.CreatedAt = t
	v.UpdatedAt = t
	return v
}

func (s *SQLStore) CreateNode(v model.Node) (model.Node, error) {
	v = defaultNode(v)
	_, err := s.exec(`insert into nodes(id, name, type, region, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.Name, v.Type, v.Region, v.Status, ts(v.CreatedAt), ts(v.UpdatedAt))
	return v, err
}

func (s *SQLStore) ListNodes() []model.Node {
	rows, err := s.query(`select id, name, type, region, status, created_at, updated_at from nodes order by name, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Node
	for rows.Next() {
		v, err := scanNode(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) GetNode(id string) (model.Node, error) {
	return scanNode(s.queryRow(`select id, name, type, region, status, created_at, updated_at from nodes where id = ?`, id))
}

func (s *SQLStore) SoftDeleteNode(id string) (model.Node, error) {
	t := now()
	version := t.UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return model.Node{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`update connections set desired_status = ?, desired_version_ms = case when desired_version_ms >= ? then desired_version_ms + 1 else ? end, updated_at = ? where node_id = ? and desired_status != ?`), model.StatusDeleted, version, version, ts(t), id, model.StatusDeleted); err != nil {
		return model.Node{}, err
	}
	if _, err = tx.Exec(s.rebind(`update agent_installations set status = ?, updated_at = ? where node_instance_id in (select id from node_instances where node_id = ?) and status = ?`), model.StatusDeleted, ts(t), id, model.StatusActive); err != nil {
		return model.Node{}, err
	}
	if _, err = tx.Exec(s.rebind(`update node_instances set status = ?, retired_at = ?, updated_at = ? where node_id = ? and status = ?`), model.StatusDeleted, ts(t), ts(t), id, model.StatusActive); err != nil {
		return model.Node{}, err
	}
	if _, err = tx.Exec(s.rebind(`update nodes set status = ?, updated_at = ? where id = ?`), model.StatusDeleted, ts(t), id); err != nil {
		return model.Node{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Node{}, err
	}
	return s.GetNode(id)
}

func defaultNodeInstance(v model.NodeInstance) model.NodeInstance {
	t := now()
	if v.ID == "" {
		v.ID = NewID("nins")
	}
	if v.SSHPort == 0 {
		v.SSHPort = 22
	}
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	if v.ActivatedAt.IsZero() && v.Status == model.StatusActive {
		v.ActivatedAt = t
	}
	v.CreatedAt = t
	v.UpdatedAt = t
	return v
}

func (s *SQLStore) CreateNodeInstance(v model.NodeInstance) (model.NodeInstance, error) {
	if _, err := s.GetNode(v.NodeID); err != nil {
		return model.NodeInstance{}, err
	}
	v = defaultNodeInstance(v)
	_, err := s.exec(`insert into node_instances(id, node_id, host, ssh_port, ssh_user, ssh_key, status, created_at, updated_at, activated_at, retired_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.NodeID, v.Host, v.SSHPort, v.SSHUser, v.SSHKey, v.Status, ts(v.CreatedAt), ts(v.UpdatedAt), tsOrNil(v.ActivatedAt), tsOrNil(v.RetiredAt))
	return v, err
}

func (s *SQLStore) GetNodeInstance(id string) (model.NodeInstance, error) {
	return scanNodeInstance(s.queryRow(nodeInstanceSelect+` where id = ?`, id))
}

func (s *SQLStore) GetActiveNodeInstance(nodeID string) (model.NodeInstance, error) {
	return scanNodeInstance(s.queryRow(nodeInstanceSelect+` where node_id = ? and status = ? order by activated_at desc, created_at desc limit 1`, nodeID, model.StatusActive))
}

func (s *SQLStore) ListNodeInstances() []model.NodeInstance {
	rows, err := s.query(nodeInstanceSelect + ` order by created_at, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.NodeInstance
	for rows.Next() {
		v, err := scanNodeInstance(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

const nodeInstanceSelect = `select id, node_id, host, ssh_port, ssh_user, ssh_key, status, created_at, updated_at, activated_at, retired_at from node_instances`

func defaultAgentInstallation(v model.AgentInstallation, instanceID string) model.AgentInstallation {
	t := now()
	if v.ID == "" {
		v.ID = NewID("ain")
	}
	v.NodeInstanceID = instanceID
	v.AgentType = strings.TrimSpace(v.AgentType)
	v.CertificateFingerprintSHA256 = strings.ToLower(strings.TrimSpace(v.CertificateFingerprintSHA256))
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	v.CreatedAt = t
	v.UpdatedAt = t
	return v
}

func validateAgentInstallation(v model.AgentInstallation) error {
	if v.NodeInstanceID == "" || v.AgentType == "" || v.CertificateFingerprintSHA256 == "" {
		return errors.New("agent installation instance, type and certificate fingerprint are required")
	}
	return nil
}

func (s *SQLStore) CreateAgentInstallation(v model.AgentInstallation) (model.AgentInstallation, error) {
	if _, err := s.GetNodeInstance(v.NodeInstanceID); err != nil {
		return model.AgentInstallation{}, err
	}
	v = defaultAgentInstallation(v, v.NodeInstanceID)
	if err := validateAgentInstallation(v); err != nil {
		return model.AgentInstallation{}, err
	}
	_, err := s.exec(`insert into agent_installations(id, node_instance_id, agent_type, status, certificate_fingerprint_sha256, certificate_serial, certificate_not_before, certificate_not_after, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.NodeInstanceID, v.AgentType, v.Status, v.CertificateFingerprintSHA256, v.CertificateSerial, tsOrNil(v.CertificateNotBefore), tsOrNil(v.CertificateNotAfter), ts(v.CreatedAt), ts(v.UpdatedAt))
	return v, err
}

func (s *SQLStore) GetActiveAgentInstallationByCertificateFingerprint(fingerprint string) (model.AgentInstallation, error) {
	return scanAgentInstallation(s.queryRow(agentInstallationSelect+` where certificate_fingerprint_sha256 = ? and status = ?`, strings.ToLower(strings.TrimSpace(fingerprint)), model.StatusActive))
}

func (s *SQLStore) LookupActiveAgentInstallation(fingerprint string) (agentprotocol.InstallationRecord, error) {
	var record agentprotocol.InstallationRecord
	err := s.queryRow(`select ai.id, ni.node_id, ai.agent_type
		from agent_installations ai
		join node_instances ni on ni.id = ai.node_instance_id
		where ai.certificate_fingerprint_sha256 = ? and ai.status = ?`,
		strings.ToLower(strings.TrimSpace(fingerprint)), model.StatusActive,
	).Scan(&record.ID, &record.NodeID, &record.AgentType)
	return record, err
}

func (s *SQLStore) GetAgentInstallation(id string) (model.AgentInstallation, error) {
	return scanAgentInstallation(s.queryRow(agentInstallationSelect+` where id = ?`, id))
}

func (s *SQLStore) ListAgentInstallations() []model.AgentInstallation {
	rows, err := s.query(agentInstallationSelect + ` order by created_at, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.AgentInstallation
	for rows.Next() {
		v, err := scanAgentInstallation(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

const agentInstallationSelect = `select id, node_instance_id, agent_type, status, certificate_fingerprint_sha256, certificate_serial, certificate_not_before, certificate_not_after, created_at, updated_at from agent_installations`

func (s *SQLStore) CreateNodeWithInstanceAndAgentInstallation(node model.Node, instance model.NodeInstance, installation model.AgentInstallation) (model.Node, model.NodeInstance, model.AgentInstallation, error) {
	node = defaultNode(node)
	instance.NodeID = node.ID
	instance = defaultNodeInstance(instance)
	installation = defaultAgentInstallation(installation, instance.ID)
	if err := validateAgentInstallation(installation); err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`insert into nodes(id, name, type, region, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`), node.ID, node.Name, node.Type, node.Region, node.Status, ts(node.CreatedAt), ts(node.UpdatedAt)); err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	if _, err = tx.Exec(s.rebind(`insert into node_instances(id, node_id, host, ssh_port, ssh_user, ssh_key, status, created_at, updated_at, activated_at, retired_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), instance.ID, instance.NodeID, instance.Host, instance.SSHPort, instance.SSHUser, instance.SSHKey, instance.Status, ts(instance.CreatedAt), ts(instance.UpdatedAt), tsOrNil(instance.ActivatedAt), tsOrNil(instance.RetiredAt)); err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	if _, err = tx.Exec(s.rebind(`insert into agent_installations(id, node_instance_id, agent_type, status, certificate_fingerprint_sha256, certificate_serial, certificate_not_before, certificate_not_after, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), installation.ID, installation.NodeInstanceID, installation.AgentType, installation.Status, installation.CertificateFingerprintSHA256, installation.CertificateSerial, tsOrNil(installation.CertificateNotBefore), tsOrNil(installation.CertificateNotAfter), ts(installation.CreatedAt), ts(installation.UpdatedAt)); err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Node{}, model.NodeInstance{}, model.AgentInstallation{}, err
	}
	return node, instance, installation, nil
}

func (s *SQLStore) CreateConnectionBundle(bundle model.ConnectionBundle) (model.ConnectionBundle, error) {
	v := bundle.Connection
	if _, err := s.GetAccount(v.AccountID); err != nil {
		return model.ConnectionBundle{}, err
	}
	node, err := s.GetNode(v.NodeID)
	if err != nil {
		return model.ConnectionBundle{}, err
	}
	if err = validateConnectionBundle(node, bundle); err != nil {
		return model.ConnectionBundle{}, err
	}
	t := now()
	if v.ID == "" {
		v.ID = NewID("con")
	}
	if strings.TrimSpace(v.Name) == "" {
		return model.ConnectionBundle{}, errors.New("connection name is required")
	}
	if v.DesiredStatus == "" {
		v.DesiredStatus = model.ConnectionStatusActive
	}
	if v.AppliedStatus == "" {
		v.AppliedStatus = model.ConnectionStatusAbsent
	}
	if v.DesiredVersionMS == 0 {
		v.DesiredVersionMS = t.UnixMilli()
	}
	v.CreatedAt, v.UpdatedAt = t, t
	tx, err := s.db.Begin()
	if err != nil {
		return model.ConnectionBundle{}, err
	}
	defer tx.Rollback()
	// Lock configuration before connections, matching the settings update order.
	if bundle.AmneziaWGMaterial != nil && bundle.ExpectedNodeConfigVersionMS > 0 {
		result, err := tx.Exec(s.rebind(`update amneziawg_node_configs set config_version_ms = config_version_ms where node_id = ? and config_version_ms = ?`), v.NodeID, bundle.ExpectedNodeConfigVersionMS)
		if err != nil {
			return model.ConnectionBundle{}, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return model.ConnectionBundle{}, err
		}
		if n != 1 {
			return model.ConnectionBundle{}, ErrConflict
		}
	}
	if _, err = tx.Exec(s.rebind(`insert into connections(id, account_id, node_id, name, protocol, desired_status, applied_status, desired_version_ms, applied_version_ms, last_error_id, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), v.ID, v.AccountID, v.NodeID, v.Name, v.Protocol, v.DesiredStatus, v.AppliedStatus, v.DesiredVersionMS, v.AppliedVersionMS, v.LastErrorID, ts(t), ts(t)); err != nil {
		if isUniqueConstraintError(err) {
			return model.ConnectionBundle{}, ErrDuplicateConnection
		}
		return model.ConnectionBundle{}, err
	}
	bundle.Connection = v
	if bundle.Hysteria2Material != nil {
		material := *bundle.Hysteria2Material
		material.ConnectionID = v.ID
		material.CreatedAt, material.UpdatedAt = t, t
		if _, err = tx.Exec(s.rebind(`insert into hysteria2_connection_materials(connection_id, password_encrypted, password_hash, created_at, updated_at) values(?, ?, ?, ?, ?)`), material.ConnectionID, material.Password, material.PasswordHash, ts(t), ts(t)); err != nil {
			return model.ConnectionBundle{}, err
		}
		bundle.Hysteria2Material = &material
	}
	if bundle.AmneziaWGMaterial != nil {
		material := *bundle.AmneziaWGMaterial
		material.ConnectionID, material.NodeID = v.ID, v.NodeID
		material.CreatedAt, material.UpdatedAt = t, t
		if _, err = tx.Exec(s.rebind(`insert into amneziawg_connection_materials(connection_id, node_id, client_public_key, client_private_key_encrypted, preshared_key_encrypted, assigned_ipv4, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?)`), material.ConnectionID, material.NodeID, material.ClientPublicKey, material.ClientPrivateKey, material.PresharedKey, material.AssignedIPv4, ts(t), ts(t)); err != nil {
			return model.ConnectionBundle{}, err
		}
		bundle.AmneziaWGMaterial = &material
	}
	if bundle.AmneziaAPIRef != nil {
		ref := *bundle.AmneziaAPIRef
		ref.ConnectionID = v.ID
		ref.CreatedAt, ref.UpdatedAt = t, t
		if _, err = tx.Exec(s.rebind(`insert into amnezia_api_connection_refs(connection_id, remote_id, remote_name, created_at, updated_at) values(?, ?, ?, ?, ?)`), ref.ConnectionID, ref.RemoteID, ref.RemoteName, ts(t), ts(t)); err != nil {
			return model.ConnectionBundle{}, err
		}
		bundle.AmneziaAPIRef = &ref
	}
	for i := range bundle.Configs {
		config := &bundle.Configs[i]
		config.ID, config.ConnectionID, config.Status = NewID("cfg"), v.ID, model.StatusActive
		config.CreatedAt, config.UpdatedAt = t, t
		if _, err = tx.Exec(s.rebind(`insert into issued_configs(id, connection_id, kind, slug, name, client, content_type, config_encrypted, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), config.ID, config.ConnectionID, config.Kind, config.Slug, config.Name, config.Client, config.ContentType, config.Config, config.Status, ts(t), ts(t)); err != nil {
			return model.ConnectionBundle{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return model.ConnectionBundle{}, err
	}
	return bundle, nil
}

func validateConnectionBundle(node model.Node, bundle model.ConnectionBundle) error {
	connection := bundle.Connection
	if len(bundle.Configs) == 0 {
		return errors.New("connection requires at least one issued config")
	}
	for _, config := range bundle.Configs {
		if config.Kind == "" || strings.TrimSpace(config.Name) == "" || strings.TrimSpace(config.Config) == "" {
			return errors.New("issued config kind, name and content are required")
		}
	}
	switch node.Type {
	case model.NodeTypeNativeHy2:
		if connection.Protocol != model.ProtocolHysteria2 || bundle.Hysteria2Material == nil || bundle.AmneziaWGMaterial != nil || bundle.AmneziaAPIRef != nil {
			return errors.New("native Hysteria2 connection requires only Hysteria2 material")
		}
		if strings.TrimSpace(bundle.Hysteria2Material.Password) == "" || strings.TrimSpace(bundle.Hysteria2Material.PasswordHash) == "" {
			return errors.New("native Hysteria2 connection password and password hash are required")
		}
	case model.NodeTypeNativeAmneziaWG:
		if connection.Protocol != model.ProtocolAmneziaWG || bundle.AmneziaWGMaterial == nil || bundle.Hysteria2Material != nil || bundle.AmneziaAPIRef != nil {
			return errors.New("native AmneziaWG connection requires only AmneziaWG material")
		}
		material := bundle.AmneziaWGMaterial
		if material.NodeID != "" && material.NodeID != connection.NodeID {
			return errors.New("native AmneziaWG connection material node mismatch")
		}
		if strings.TrimSpace(material.ClientPublicKey) == "" || strings.TrimSpace(material.ClientPrivateKey) == "" || strings.TrimSpace(material.PresharedKey) == "" || strings.TrimSpace(material.AssignedIPv4) == "" {
			return errors.New("native AmneziaWG connection material is incomplete")
		}
	case model.NodeTypeAmneziaAPI:
		if connection.Protocol != model.ProtocolAmneziaWG || bundle.AmneziaAPIRef == nil || bundle.Hysteria2Material != nil || bundle.AmneziaWGMaterial != nil {
			return errors.New("Amnezia API connection requires only an external connection reference")
		}
		if strings.TrimSpace(bundle.AmneziaAPIRef.RemoteID) == "" {
			return errors.New("Amnezia API remote connection ID is required")
		}
	default:
		return fmt.Errorf("unsupported connection node type %q", node.Type)
	}
	return nil
}

func (s *SQLStore) GetConnection(id string) (model.Connection, error) {
	return scanConnection(s.queryRow(connectionSelect+` where id = ?`, id))
}

func (s *SQLStore) ListConnections() []model.Connection {
	rows, err := s.query(connectionSelect + ` order by created_at, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.Connection
	for rows.Next() {
		v, err := scanConnection(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

const connectionSelect = `select id, account_id, node_id, name, protocol, desired_status, applied_status, desired_version_ms, applied_version_ms, last_error_id, created_at, updated_at from connections`

func (s *SQLStore) UpdateConnectionDesiredStatus(id string, status model.ConnectionStatus) (model.Connection, error) {
	if status != model.ConnectionStatusActive && status != model.ConnectionStatusHeld && status != model.ConnectionStatusDeleted {
		return model.Connection{}, fmt.Errorf("unsupported connection desired status %q", status)
	}
	t := now()
	version := t.UnixMilli()
	_, err := s.exec(`update connections set desired_status = ?, desired_version_ms = case when desired_version_ms >= ? then desired_version_ms + 1 else ? end, last_error_id = '', updated_at = ? where id = ?`, status, version, version, ts(t), id)
	if err != nil {
		return model.Connection{}, err
	}
	return s.GetConnection(id)
}

func (s *SQLStore) FinalizeConnectionsForAuthSnapshot(nodeID, accountID string, appliedVersionMS int64) ([]model.Connection, error) {
	rows, err := s.query(connectionSelect+` where node_id = ? and account_id = ? and desired_version_ms <= ? and (applied_status != desired_status or applied_version_ms < desired_version_ms) order by id`, nodeID, accountID, appliedVersionMS)
	if err != nil {
		return nil, err
	}
	var candidates []model.Connection
	for rows.Next() {
		v, scanErr := scanConnection(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		candidates = append(candidates, v)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i := range candidates {
		v := &candidates[i]
		if _, err = tx.Exec(s.rebind(`update connections set applied_status = desired_status, applied_version_ms = desired_version_ms, last_error_id = '', updated_at = ? where id = ? and desired_version_ms <= ?`), ts(now()), v.ID, appliedVersionMS); err != nil {
			return nil, err
		}
		v.AppliedStatus = v.DesiredStatus
		v.AppliedVersionMS = v.DesiredVersionMS
		if v.DesiredStatus == model.ConnectionStatusDeleted {
			if _, err = tx.Exec(s.rebind(`update issued_configs set status = ?, updated_at = ? where connection_id = ? and status != ?`), model.StatusDeleted, ts(now()), v.ID, model.StatusDeleted); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *SQLStore) CountActiveConnectionsForAccount(accountID string) (int, error) {
	var count int
	err := s.queryRow(`select count(*) from connections where account_id = ? and desired_status != ?`, accountID, model.StatusDeleted).Scan(&count)
	return count, err
}

func (s *SQLStore) CreateHysteria2NodeConfig(v model.Hysteria2NodeConfig) (model.Hysteria2NodeConfig, error) {
	t := now()
	if v.EndpointPort == 0 {
		v.EndpointPort = 443
	}
	if v.ObfsType == "" && v.ObfsEnabled {
		v.ObfsType = "salamander"
	}
	if v.ConfigVersionMS == 0 {
		v.ConfigVersionMS = t.UnixMilli()
	}
	v.CreatedAt = t
	v.UpdatedAt = t
	_, err := s.exec(`insert into hysteria2_node_configs(node_id, endpoint_host, endpoint_port, obfs_enabled, obfs_type, obfs_password_encrypted, use_ipv6, masquerade_url, config_version_ms, created_at, updated_at, settings_encrypted, applied_config_version_ms, config_error_id, masquerade_fileset_id) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, nullif(?,''))`,
		v.NodeID, v.EndpointHost, v.EndpointPort, boolInt(v.ObfsEnabled), v.ObfsType, v.ObfsPassword, boolInt(v.UseIPv6), v.MasqueradeURL, v.ConfigVersionMS, ts(v.CreatedAt), ts(v.UpdatedAt), v.SettingsJSON, v.AppliedConfigVersionMS, v.ConfigErrorID, v.MasqueradeFileSetID)
	return v, err
}

func (s *SQLStore) GetHysteria2NodeConfig(nodeID string) (model.Hysteria2NodeConfig, error) {
	return scanHysteria2NodeConfig(s.queryRow(`select node_id, endpoint_host, endpoint_port, obfs_enabled, obfs_type, obfs_password_encrypted, use_ipv6, masquerade_url, config_version_ms, created_at, updated_at, settings_encrypted, applied_config_version_ms, config_error_id,coalesce(masquerade_fileset_id,'') from hysteria2_node_configs where node_id = ?`, nodeID))
}

func (s *SQLStore) GetHysteria2ConnectionMaterial(connectionID string) (model.Hysteria2ConnectionMaterial, error) {
	return scanHysteria2ConnectionMaterial(s.queryRow(`select connection_id, password_encrypted, password_hash, created_at, updated_at from hysteria2_connection_materials where connection_id = ?`, connectionID))
}

func (s *SQLStore) ListHysteriaSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[string], error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`select c.id, c.desired_status, c.desired_version_ms, a.status, m.password_hash from connections c join accounts a on a.id = c.account_id left join hysteria2_connection_materials m on m.connection_id = c.id where c.node_id = ? and c.protocol = ? and c.id > ? order by c.id`), nodeID, model.ProtocolHysteria2, fromConnectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentprotocol.StoredConnection[string]
	for rows.Next() {
		var id, passwordHash string
		var desired model.ConnectionStatus
		var accountStatus model.AccountStatus
		var version int64
		if err = rows.Scan(&id, &desired, &version, &accountStatus, &passwordHash); err != nil {
			return nil, err
		}
		out = append(out, agentprotocol.StoredConnection[string]{ConnectionID: id, Active: accountStatus == model.AccountStatusActive && desired == model.ConnectionStatusActive && passwordHash != "", UpdatedAtMS: version, Material: passwordHash})
	}
	return out, rows.Err()
}

func (s *SQLStore) CreateAmneziaAPINodeConfig(v model.AmneziaAPINodeConfig) (model.AmneziaAPINodeConfig, error) {
	t := now()
	v.CreatedAt, v.UpdatedAt = t, t
	_, err := s.exec(`insert into amnezia_api_node_configs(node_id, base_url, api_key_encrypted, created_at, updated_at) values(?, ?, ?, ?, ?)`, v.NodeID, v.BaseURL, v.APIKey, ts(t), ts(t))
	return v, err
}

func (s *SQLStore) CreateAmneziaAPINode(node model.Node, config model.AmneziaAPINodeConfig) (model.Node, model.AmneziaAPINodeConfig, error) {
	node = defaultNode(node)
	config.NodeID = node.ID
	t := now()
	config.CreatedAt, config.UpdatedAt = t, t
	tx, err := s.db.Begin()
	if err != nil {
		return model.Node{}, model.AmneziaAPINodeConfig{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`insert into nodes(id, name, type, region, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`), node.ID, node.Name, node.Type, node.Region, node.Status, ts(node.CreatedAt), ts(node.UpdatedAt)); err != nil {
		return model.Node{}, model.AmneziaAPINodeConfig{}, err
	}
	if _, err = tx.Exec(s.rebind(`insert into amnezia_api_node_configs(node_id, base_url, api_key_encrypted, created_at, updated_at) values(?, ?, ?, ?, ?)`), config.NodeID, config.BaseURL, config.APIKey, ts(t), ts(t)); err != nil {
		return model.Node{}, model.AmneziaAPINodeConfig{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Node{}, model.AmneziaAPINodeConfig{}, err
	}
	return node, config, nil
}

func (s *SQLStore) GetAmneziaAPINodeConfig(nodeID string) (model.AmneziaAPINodeConfig, error) {
	var v model.AmneziaAPINodeConfig
	var created, updated string
	err := s.queryRow(`select node_id, base_url, api_key_encrypted, created_at, updated_at from amnezia_api_node_configs where node_id = ?`, nodeID).Scan(&v.NodeID, &v.BaseURL, &v.APIKey, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func (s *SQLStore) GetAmneziaAPIConnectionRef(connectionID string) (model.AmneziaAPIConnectionRef, error) {
	var v model.AmneziaAPIConnectionRef
	var created, updated string
	err := s.queryRow(`select connection_id, remote_id, remote_name, created_at, updated_at from amnezia_api_connection_refs where connection_id = ?`, connectionID).Scan(&v.ConnectionID, &v.RemoteID, &v.RemoteName, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func defaultAmneziaWGNodeConfig(v model.AmneziaWGNodeConfig) model.AmneziaWGNodeConfig {
	t := now()
	if v.EndpointPort == 0 {
		v.EndpointPort = 443
	}
	if v.ListenPort == 0 {
		v.ListenPort = v.EndpointPort
	}
	if v.ServerIPv4 == "" {
		v.ServerIPv4 = "10.8.1.1"
	}
	if v.ClientIPv4CIDR == "" {
		v.ClientIPv4CIDR = "10.8.1.0/24"
	}
	if v.FirstClientIPv4 == "" {
		v.FirstClientIPv4 = "10.8.1.2"
	}
	if v.LastClientIPv4 == "" {
		v.LastClientIPv4 = "10.8.1.254"
	}
	if v.DNS == "" {
		v.DNS = "1.1.1.1, 1.0.0.1"
	}
	if v.MTU == 0 {
		v.MTU = 1376
	}
	if v.PersistentKeepalive == 0 {
		v.PersistentKeepalive = 25
	}
	if v.ClientRouteAllowedIPs == "" {
		v.ClientRouteAllowedIPs = "0.0.0.0/0"
	}
	if v.ConfigVersionMS == 0 {
		v.ConfigVersionMS = t.UnixMilli()
	}
	v.CreatedAt, v.UpdatedAt = t, t
	return v
}

const amneziaWGConfigColumns = `node_id, endpoint_host, endpoint_port, listen_port, server_ipv4, client_ipv4_cidr, first_client_ipv4, last_client_ipv4, dns, mtu, persistent_keepalive, client_route_allowed_ips, server_private_key_encrypted, server_public_key, jc, jmin, jmax, s1, s2, s3, s4, h1, h2, h3, h4, i1, i2, i3, i4, i5, config_version_ms, created_at, updated_at, header_protection_key_encrypted, content_padding_addition, timings_json, random_trailers, disable_cookies, applied_config_version_ms, config_error_id, client_route_mode, client_excluded_routes`

func (s *SQLStore) CreateAmneziaWGNodeConfig(v model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error) {
	v = defaultAmneziaWGNodeConfig(v)
	_, err := s.exec(`insert into amneziawg_node_configs(`+amneziaWGConfigColumns+`) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, amneziaWGConfigArgs(v)...)
	return v, err
}

func (s *SQLStore) CreateNativeAmneziaWGNode(node model.Node, config model.AmneziaWGNodeConfig) (model.Node, model.AmneziaWGNodeConfig, error) {
	node = defaultNode(node)
	config.NodeID = node.ID
	config = defaultAmneziaWGNodeConfig(config)
	tx, err := s.db.Begin()
	if err != nil {
		return model.Node{}, model.AmneziaWGNodeConfig{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`insert into nodes(id, name, type, region, status, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?)`), node.ID, node.Name, node.Type, node.Region, node.Status, ts(node.CreatedAt), ts(node.UpdatedAt)); err != nil {
		return model.Node{}, model.AmneziaWGNodeConfig{}, err
	}
	if _, err = tx.Exec(s.rebind(`insert into amneziawg_node_configs(`+amneziaWGConfigColumns+`) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`), amneziaWGConfigArgs(config)...); err != nil {
		return model.Node{}, model.AmneziaWGNodeConfig{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.Node{}, model.AmneziaWGNodeConfig{}, err
	}
	return node, config, nil
}

func amneziaWGConfigArgs(v model.AmneziaWGNodeConfig) []any {
	return []any{v.NodeID, v.EndpointHost, v.EndpointPort, v.ListenPort, v.ServerIPv4, v.ClientIPv4CIDR, v.FirstClientIPv4, v.LastClientIPv4, v.DNS, v.MTU, v.PersistentKeepalive, v.ClientRouteAllowedIPs, v.ServerPrivateKey, v.ServerPublicKey, v.JC, v.JMin, v.JMax, v.S1, v.S2, v.S3, v.S4, v.H1, v.H2, v.H3, v.H4, v.I1, v.I2, v.I3, v.I4, v.I5, v.ConfigVersionMS, ts(v.CreatedAt), ts(v.UpdatedAt), v.HeaderProtectionKey, v.ContentPaddingAddition, v.TimingsJSON, v.RandomTrailers, v.DisableCookies, v.AppliedConfigVersionMS, v.ConfigErrorID, v.ClientRouteMode, v.ClientExcludedRoutes}
}

func (s *SQLStore) GetAmneziaWGNodeConfig(nodeID string) (model.AmneziaWGNodeConfig, error) {
	return scanAmneziaWGNodeConfig(s.queryRow(`select `+amneziaWGConfigColumns+` from amneziawg_node_configs where node_id = ?`, nodeID))
}

func (s *SQLStore) GetAmneziaWGConnectionMaterial(connectionID string) (model.AmneziaWGConnectionMaterial, error) {
	return scanAmneziaWGConnectionMaterial(s.queryRow(amneziaWGMaterialSelect+` where connection_id = ?`, connectionID))
}

func (s *SQLStore) ListAmneziaWGConnectionMaterials(nodeID string) []model.AmneziaWGConnectionMaterial {
	rows, err := s.query(amneziaWGMaterialSelect+` where node_id = ? order by assigned_ipv4, connection_id`, nodeID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.AmneziaWGConnectionMaterial
	for rows.Next() {
		v, err := scanAmneziaWGConnectionMaterial(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

const amneziaWGMaterialSelect = `select connection_id, node_id, client_public_key, client_private_key_encrypted, preshared_key_encrypted, assigned_ipv4, created_at, updated_at from amneziawg_connection_materials`

func (s *SQLStore) ListAmneziaWGSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial], error) {
	config, err := s.GetAmneziaWGNodeConfig(nodeID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`select c.id, c.account_id, c.node_id, c.name, c.protocol, c.desired_status, c.applied_status, c.desired_version_ms, c.applied_version_ms, c.last_error_id, c.created_at, c.updated_at, a.status, m.connection_id, m.node_id, m.client_public_key, m.client_private_key_encrypted, m.preshared_key_encrypted, m.assigned_ipv4, m.created_at, m.updated_at from connections c join accounts a on a.id = c.account_id left join amneziawg_connection_materials m on m.connection_id = c.id where c.node_id = ? and c.protocol = ? and c.id > ? order by c.id`), nodeID, model.ProtocolAmneziaWG, fromConnectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial]
	for rows.Next() {
		var connection model.Connection
		var material model.AmneziaWGConnectionMaterial
		var accountStatus model.AccountStatus
		var connectionCreated, connectionUpdated, materialCreated, materialUpdated string
		if err = rows.Scan(&connection.ID, &connection.AccountID, &connection.NodeID, &connection.Name, &connection.Protocol, &connection.DesiredStatus, &connection.AppliedStatus, &connection.DesiredVersionMS, &connection.AppliedVersionMS, &connection.LastErrorID, &connectionCreated, &connectionUpdated, &accountStatus, &material.ConnectionID, &material.NodeID, &material.ClientPublicKey, &material.ClientPrivateKey, &material.PresharedKey, &material.AssignedIPv4, &materialCreated, &materialUpdated); err != nil {
			return nil, err
		}
		connection.CreatedAt, connection.UpdatedAt = parseTS(connectionCreated), parseTS(connectionUpdated)
		material.CreatedAt, material.UpdatedAt = parseTS(materialCreated), parseTS(materialUpdated)
		active := accountStatus == model.AccountStatusActive && connection.DesiredStatus == model.ConnectionStatusActive && material.ConnectionID != ""
		out = append(out, agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial]{ConnectionID: connection.ID, Active: active, UpdatedAtMS: connection.DesiredVersionMS, Material: model.AmneziaWGSyncMaterial{NodeConfig: config, Connection: connection, Material: material}})
	}
	return out, rows.Err()
}

func (s *SQLStore) CreateAccountPortalToken(v model.AccountPortalToken) (model.AccountPortalToken, error) {
	t := now()
	if v.ID == "" {
		v.ID = NewID("apt")
	}
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	v.CreatedAt, v.UpdatedAt = t, t
	_, err := s.exec(`insert into account_portal_tokens(id, account_id, token_encrypted, token_hash, status, expires_at, last_used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, v.ID, v.AccountID, v.Token, v.TokenHash, v.Status, tsOrNil(v.ExpiresAt), tsOrNil(v.LastUsedAt), ts(t), ts(t))
	return v, err
}

func (s *SQLStore) ListAccountPortalTokens() []model.AccountPortalToken {
	rows, err := s.query(accountPortalTokenSelect + ` order by created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.AccountPortalToken
	for rows.Next() {
		v, err := scanAccountPortalToken(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) GetAccountPortalTokenByHash(hash string) (model.AccountPortalToken, error) {
	return scanAccountPortalToken(s.queryRow(accountPortalTokenSelect+` where token_hash = ?`, hash))
}

func (s *SQLStore) TouchAccountPortalToken(id string) error {
	_, err := s.exec(`update account_portal_tokens set last_used_at = ?, updated_at = ? where id = ?`, ts(now()), ts(now()), id)
	return err
}

const accountPortalTokenSelect = `select id, account_id, token_encrypted, token_hash, status, expires_at, last_used_at, created_at, updated_at from account_portal_tokens`

func (s *SQLStore) CreateConnectionSubscriptionToken(v model.ConnectionSubscriptionToken) (model.ConnectionSubscriptionToken, error) {
	t := now()
	if v.ID == "" {
		v.ID = NewID("cst")
	}
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	v.CreatedAt, v.UpdatedAt = t, t
	_, err := s.exec(`insert into connection_subscription_tokens(id, connection_id, token_encrypted, token_hash, status, expires_at, last_used_at, created_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?)`, v.ID, v.ConnectionID, v.Token, v.TokenHash, v.Status, tsOrNil(v.ExpiresAt), tsOrNil(v.LastUsedAt), ts(t), ts(t))
	return v, err
}

func (s *SQLStore) ListConnectionSubscriptionTokens() []model.ConnectionSubscriptionToken {
	rows, err := s.query(connectionSubscriptionTokenSelect + ` order by created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.ConnectionSubscriptionToken
	for rows.Next() {
		v, err := scanConnectionSubscriptionToken(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) GetConnectionSubscriptionTokenByHash(hash string) (model.ConnectionSubscriptionToken, error) {
	return scanConnectionSubscriptionToken(s.queryRow(connectionSubscriptionTokenSelect+` where token_hash = ?`, hash))
}

func (s *SQLStore) TouchConnectionSubscriptionToken(id string) error {
	_, err := s.exec(`update connection_subscription_tokens set last_used_at = ?, updated_at = ? where id = ?`, ts(now()), ts(now()), id)
	return err
}

const connectionSubscriptionTokenSelect = `select id, connection_id, token_encrypted, token_hash, status, expires_at, last_used_at, created_at, updated_at from connection_subscription_tokens`

func (s *SQLStore) CreateShortLink(v model.ShortLink) (model.ShortLink, error) {
	if v.ID == "" {
		v.ID = NewID("sl")
	}
	if v.Status == "" {
		v.Status = model.StatusActive
	}
	v.CreatedAt = now()
	_, err := s.exec(`insert into short_links(id, subscription_token_id, profile, encrypted_url, status, created_at) values(?, ?, ?, ?, ?, ?)`, v.ID, v.SubscriptionTokenID, v.Profile, v.EncryptedURL, v.Status, ts(v.CreatedAt))
	return v, err
}

func (s *SQLStore) GetShortLink(id string) (model.ShortLink, error) {
	return scanShortLink(s.queryRow(shortLinkSelect+` where id = ?`, id))
}

func (s *SQLStore) GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error) {
	return scanShortLink(s.queryRow(shortLinkSelect+` where subscription_token_id = ? and profile = ?`, tokenID, profile))
}

const shortLinkSelect = `select id, subscription_token_id, profile, encrypted_url, status, created_at from short_links`

func (s *SQLStore) Summary(accountID string) (model.AccountSummary, error) {
	account, err := s.GetAccount(accountID)
	if err != nil {
		return model.AccountSummary{}, err
	}
	nodes := map[string]model.Node{}
	for _, node := range s.ListNodes() {
		nodes[node.ID] = node
	}
	var connections []model.ConnectionSummary
	protocols := map[model.Protocol]bool{}
	connectionIDs := map[string]bool{}
	for _, connection := range s.ListConnections() {
		if connection.AccountID != accountID {
			continue
		}
		connections = append(connections, model.ConnectionSummary{Connection: connection, Node: nodes[connection.NodeID]})
		connectionIDs[connection.ID] = true
		if connection.DesiredStatus != model.ConnectionStatusDeleted {
			protocols[connection.Protocol] = true
		}
	}
	var configs []model.IssuedConfig
	for _, config := range s.ListIssuedConfigs() {
		if connectionIDs[config.ConnectionID] {
			configs = append(configs, config)
		}
	}
	var profiles []model.ConfigProfile
	for _, profile := range s.ListConfigProfiles() {
		if profile.Status == model.StatusActive && protocols[profile.Protocol] {
			profiles = append(profiles, profile)
		}
	}
	return model.AccountSummary{Account: account, Connections: connections, Configs: configs, Profiles: profiles, Generated: now()}, nil
}

func scanNode(row scanner) (model.Node, error) {
	var v model.Node
	var created, updated string
	err := row.Scan(&v.ID, &v.Name, &v.Type, &v.Region, &v.Status, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanNodeInstance(row scanner) (model.NodeInstance, error) {
	var v model.NodeInstance
	var created, updated string
	var activated, retired sql.NullString
	err := row.Scan(&v.ID, &v.NodeID, &v.Host, &v.SSHPort, &v.SSHUser, &v.SSHKey, &v.Status, &created, &updated, &activated, &retired)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	v.ActivatedAt, v.RetiredAt = parseNullTS(activated), parseNullTS(retired)
	return v, convertErr(err)
}

func scanAgentInstallation(row scanner) (model.AgentInstallation, error) {
	var v model.AgentInstallation
	var notBefore, notAfter sql.NullString
	var created, updated string
	err := row.Scan(&v.ID, &v.NodeInstanceID, &v.AgentType, &v.Status, &v.CertificateFingerprintSHA256, &v.CertificateSerial, &notBefore, &notAfter, &created, &updated)
	v.CertificateNotBefore, v.CertificateNotAfter = parseNullTS(notBefore), parseNullTS(notAfter)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanConnection(row scanner) (model.Connection, error) {
	var v model.Connection
	var created, updated string
	err := row.Scan(&v.ID, &v.AccountID, &v.NodeID, &v.Name, &v.Protocol, &v.DesiredStatus, &v.AppliedStatus, &v.DesiredVersionMS, &v.AppliedVersionMS, &v.LastErrorID, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanHysteria2NodeConfig(row scanner) (model.Hysteria2NodeConfig, error) {
	var v model.Hysteria2NodeConfig
	var obfs, ipv6 int
	var created, updated string
	err := row.Scan(&v.NodeID, &v.EndpointHost, &v.EndpointPort, &obfs, &v.ObfsType, &v.ObfsPassword, &ipv6, &v.MasqueradeURL, &v.ConfigVersionMS, &created, &updated, &v.SettingsJSON, &v.AppliedConfigVersionMS, &v.ConfigErrorID, &v.MasqueradeFileSetID)
	v.ObfsEnabled, v.UseIPv6 = obfs != 0, ipv6 != 0
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanHysteria2ConnectionMaterial(row scanner) (model.Hysteria2ConnectionMaterial, error) {
	var v model.Hysteria2ConnectionMaterial
	var created, updated string
	err := row.Scan(&v.ConnectionID, &v.Password, &v.PasswordHash, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanAmneziaWGNodeConfig(row scanner) (model.AmneziaWGNodeConfig, error) {
	var v model.AmneziaWGNodeConfig
	var created, updated string
	err := row.Scan(&v.NodeID, &v.EndpointHost, &v.EndpointPort, &v.ListenPort, &v.ServerIPv4, &v.ClientIPv4CIDR, &v.FirstClientIPv4, &v.LastClientIPv4, &v.DNS, &v.MTU, &v.PersistentKeepalive, &v.ClientRouteAllowedIPs, &v.ServerPrivateKey, &v.ServerPublicKey, &v.JC, &v.JMin, &v.JMax, &v.S1, &v.S2, &v.S3, &v.S4, &v.H1, &v.H2, &v.H3, &v.H4, &v.I1, &v.I2, &v.I3, &v.I4, &v.I5, &v.ConfigVersionMS, &created, &updated, &v.HeaderProtectionKey, &v.ContentPaddingAddition, &v.TimingsJSON, &v.RandomTrailers, &v.DisableCookies, &v.AppliedConfigVersionMS, &v.ConfigErrorID, &v.ClientRouteMode, &v.ClientExcludedRoutes)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanAmneziaWGConnectionMaterial(row scanner) (model.AmneziaWGConnectionMaterial, error) {
	var v model.AmneziaWGConnectionMaterial
	var created, updated string
	err := row.Scan(&v.ConnectionID, &v.NodeID, &v.ClientPublicKey, &v.ClientPrivateKey, &v.PresharedKey, &v.AssignedIPv4, &created, &updated)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanAccountPortalToken(row scanner) (model.AccountPortalToken, error) {
	var v model.AccountPortalToken
	var expires, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&v.ID, &v.AccountID, &v.Token, &v.TokenHash, &v.Status, &expires, &lastUsed, &created, &updated)
	v.ExpiresAt, v.LastUsedAt = parseNullTS(expires), parseNullTS(lastUsed)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanConnectionSubscriptionToken(row scanner) (model.ConnectionSubscriptionToken, error) {
	var v model.ConnectionSubscriptionToken
	var expires, lastUsed sql.NullString
	var created, updated string
	err := row.Scan(&v.ID, &v.ConnectionID, &v.Token, &v.TokenHash, &v.Status, &expires, &lastUsed, &created, &updated)
	v.ExpiresAt, v.LastUsedAt = parseNullTS(expires), parseNullTS(lastUsed)
	v.CreatedAt, v.UpdatedAt = parseTS(created), parseTS(updated)
	return v, convertErr(err)
}

func scanShortLink(row scanner) (model.ShortLink, error) {
	var v model.ShortLink
	var created string
	err := row.Scan(&v.ID, &v.SubscriptionTokenID, &v.Profile, &v.EncryptedURL, &v.Status, &created)
	v.CreatedAt = parseTS(created)
	return v, convertErr(err)
}

func nullableTS(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return ts(value)
}
