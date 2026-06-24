package store

import (
	"laz/internal/server/model"
	"strings"
)

// Update the desired configuration and all issued client profiles atomically.
func (s *SQLStore) UpdateAmneziaWGNodeConfig(v model.AmneziaWGNodeConfig, expected int64, configs []model.IssuedConfig) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	columns := strings.Split(amneziaWGConfigColumns, ", ")
	args := amneziaWGConfigArgs(v)
	var sets []string
	var values []any
	for i, key := range columns {
		if key == "node_id" || key == "created_at" {
			continue
		}
		sets = append(sets, key+" = ?")
		values = append(values, args[i])
	}
	values = append(values, v.NodeID, expected)
	result, err := tx.Exec(s.rebind(`update amneziawg_node_configs set `+strings.Join(sets, ", ")+` where node_id = ? and config_version_ms = ?`), values...)
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
	var materialCount int
	if err := tx.QueryRow(s.rebind(`select count(*) from amneziawg_connection_materials where node_id = ?`), v.NodeID).Scan(&materialCount); err != nil {
		return err
	}
	if materialCount != len(configs) {
		return ErrConflict
	}
	for _, c := range configs {
		if _, err = tx.Exec(s.rebind(`update issued_configs set config_encrypted = ?, updated_at = ? where connection_id = ? and kind = ? and status != ?`), c.Config, ts(v.UpdatedAt), c.ConnectionID, model.ConfigAmneziaConf, model.StatusDeleted); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(s.rebind(`update connections set desired_version_ms = case when desired_version_ms >= ? then desired_version_ms + 1 else ? end, updated_at = ? where node_id = ? and desired_status != ?`), v.ConfigVersionMS, v.ConfigVersionMS, ts(v.UpdatedAt), v.NodeID, model.ConnectionStatusDeleted); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStore) SetAmneziaWGConfigResult(nodeID string, version int64, errorID string) error {
	if errorID != "" {
		_, err := s.exec(`update amneziawg_node_configs set config_error_id = ? where node_id = ? and config_version_ms = ?`, errorID, nodeID, version)
		return err
	}
	_, err := s.exec(`update amneziawg_node_configs set applied_config_version_ms = ?, config_error_id = '' where node_id = ? and config_version_ms = ?`, version, nodeID, version)
	return err
}
