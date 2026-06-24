package store

import "laz/internal/server/model"

func (s *SQLStore) ListHysteria2ConfigSources(nodeID string) ([]model.Hysteria2ConfigSource, error) {
	rows, err := s.query(`select c.id,c.name,m.password_encrypted from connections c join hysteria2_connection_materials m on m.connection_id=c.id where c.node_id=? and c.desired_status != ? order by c.id`, nodeID, model.ConnectionStatusDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.Hysteria2ConfigSource{}
	for rows.Next() {
		var v model.Hysteria2ConfigSource
		if err := rows.Scan(&v.ConnectionID, &v.Name, &v.Password); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *SQLStore) UpdateHysteria2NodeConfig(c model.Hysteria2NodeConfig, expected int64, configs []model.IssuedConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(s.rebind(`update hysteria2_node_configs set endpoint_host=?, endpoint_port=?, obfs_enabled=?, obfs_type=?, obfs_password_encrypted=?, masquerade_url=?, settings_encrypted=?, config_version_ms=?, config_error_id='', updated_at=?,masquerade_fileset_id=nullif(?,'') where node_id=? and config_version_ms=?`), c.EndpointHost, c.EndpointPort, boolInt(c.ObfsEnabled), c.ObfsType, c.ObfsPassword, c.MasqueradeURL, c.SettingsJSON, c.ConfigVersionMS, ts(c.UpdatedAt), c.MasqueradeFileSetID, c.NodeID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	var count int
	if err = tx.QueryRow(s.rebind(`select count(*) from connections where node_id=? and desired_status != ?`), c.NodeID, model.ConnectionStatusDeleted).Scan(&count); err != nil {
		return err
	}
	if count != len(configs) {
		return ErrConflict
	}
	for _, v := range configs {
		if _, err = tx.Exec(s.rebind(`update issued_configs set config_encrypted=?, updated_at=? where connection_id=? and kind=? and status != ?`), v.Config, ts(c.UpdatedAt), v.ConnectionID, model.ConfigHy2URI, model.StatusDeleted); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *SQLStore) SetHysteria2ConfigResult(id string, version int64, errorID string) error {
	if errorID != "" {
		_, err := s.exec(`update hysteria2_node_configs set config_error_id=? where node_id=? and config_version_ms=?`, errorID, id, version)
		return err
	}
	_, err := s.exec(`update hysteria2_node_configs set applied_config_version_ms=?,config_error_id='',applied_masquerade_fileset_id=masquerade_fileset_id where node_id=? and config_version_ms=?`, version, id, version)
	return err
}
