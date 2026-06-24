package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"laz/internal/server/model"
)

func (s *SQLStore) UpsertNodeRuntime(v model.NodeRuntime) error {
	if v.AgentInstallationID == "" || v.NodeInstanceID == "" {
		return ErrNotFound
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = now()
	}
	if err := s.updateNodeStatusInterval(v); err != nil {
		return err
	}
	_, err := s.exec(`insert into node_runtime(agent_installation_id, node_instance_id, agent_status, last_heartbeat_at, agent_version, protocol_version, service_status, recent_message, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?) on conflict(agent_installation_id) do update set node_instance_id = excluded.node_instance_id, agent_status = excluded.agent_status, last_heartbeat_at = excluded.last_heartbeat_at, agent_version = excluded.agent_version, protocol_version = excluded.protocol_version, service_status = excluded.service_status, recent_message = excluded.recent_message, updated_at = excluded.updated_at`,
		v.AgentInstallationID, v.NodeInstanceID, v.AgentStatus, tsOrNil(v.LastHeartbeatAt), v.AgentVersion, v.ProtocolVersion, v.ServiceStatus, v.RecentMessage, ts(v.UpdatedAt))
	return err
}

func (s *SQLStore) updateNodeStatusInterval(v model.NodeRuntime) error {
	var currentStatus string
	var currentID string
	err := s.queryRow(`select id, status from node_status_intervals where agent_installation_id = ? and ended_at_ms is null order by started_at_ms desc limit 1`, v.AgentInstallationID).Scan(&currentID, &currentStatus)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && currentStatus == v.AgentStatus {
		return nil
	}
	atMS := v.UpdatedAt.UnixMilli()
	if err == nil {
		if _, err = s.exec(`update node_status_intervals set ended_at_ms = ? where id = ?`, atMS, currentID); err != nil {
			return err
		}
	}
	_, err = s.exec(`insert into node_status_intervals(id, agent_installation_id, node_instance_id, status, started_at_ms, ended_at_ms) values(?, ?, ?, ?, ?, null)`, NewID("nsi"), v.AgentInstallationID, v.NodeInstanceID, v.AgentStatus, atMS)
	return err
}

const nodeRuntimeSelect = `select agent_installation_id, node_instance_id, agent_status, last_heartbeat_at, agent_version, protocol_version, service_status, recent_message, updated_at from node_runtime`

func (s *SQLStore) GetNodeRuntime(agentInstallationID string) (model.NodeRuntime, error) {
	return scanNodeRuntime(s.queryRow(nodeRuntimeSelect+` where agent_installation_id = ?`, agentInstallationID))
}

func (s *SQLStore) ListNodeRuntimes() []model.NodeRuntime {
	rows, err := s.query(nodeRuntimeSelect + ` order by updated_at desc`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.NodeRuntime
	for rows.Next() {
		v, err := scanNodeRuntime(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) UpsertNodeOnlineConnections(nodeInstanceID string, connections []model.NodeOnlineConnection) error {
	existing := map[string]model.NodeOnlineConnection{}
	for _, item := range s.ListNodeOnlineConnections(nodeInstanceID) {
		existing[item.NativeAuthID] = item
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(s.rebind(`delete from node_online_connections where node_instance_id = ?`), nodeInstanceID); err != nil {
		return err
	}
	for _, item := range connections {
		if item.NativeAuthID == "" || item.Count <= 0 {
			return fmt.Errorf("invalid online identity or count")
		}
		if item.ConnectionID != "" {
			if err = s.requireConnectionOnInstance(context.Background(), tx, nodeInstanceID, item.ConnectionID); err != nil {
				return err
			}
		}
		first := item.FirstSeenAt
		if previous, ok := existing[item.NativeAuthID]; ok && !previous.FirstSeenAt.IsZero() {
			first = previous.FirstSeenAt
		}
		if first.IsZero() {
			first = item.LastSeenAt
		}
		var connectionID any
		if item.ConnectionID != "" {
			connectionID = item.ConnectionID
		}
		if _, err = tx.Exec(s.rebind(`insert into node_online_connections(node_instance_id, native_auth_id, connection_id, count, first_seen_at, last_seen_at) values(?, ?, ?, ?, ?, ?)`), nodeInstanceID, item.NativeAuthID, connectionID, item.Count, ts(first), ts(item.LastSeenAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLStore) ListNodeOnlineConnections(nodeInstanceID string) []model.NodeOnlineConnection {
	return s.listNodeOnlineConnections(` where node_instance_id = ? order by connection_id`, nodeInstanceID)
}

func (s *SQLStore) ListAllNodeOnlineConnections() []model.NodeOnlineConnection {
	return s.listNodeOnlineConnections(` order by node_instance_id, connection_id`)
}

func (s *SQLStore) listNodeOnlineConnections(suffix string, args ...any) []model.NodeOnlineConnection {
	rows, err := s.query(`select node_instance_id, native_auth_id, coalesce(connection_id, ''), count, first_seen_at, last_seen_at from node_online_connections`+suffix, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.NodeOnlineConnection
	for rows.Next() {
		var v model.NodeOnlineConnection
		var first, last string
		if err := rows.Scan(&v.NodeInstanceID, &v.NativeAuthID, &v.ConnectionID, &v.Count, &first, &last); err == nil {
			v.FirstSeenAt, v.LastSeenAt = parseTS(first), parseTS(last)
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) ApplyUsageRecords(requestID, nodeInstanceID string, records []model.UsageRecord) ([]model.ConnectionUsageTotal, bool, error) {
	if requestID == "" || nodeInstanceID == "" || len(records) == 0 {
		return nil, false, errors.New("usage request_id, node instance and records are required")
	}
	receivedAtMS := now().UnixMilli()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// Serialize duplicate checks and accounting for a logical node, including
	// reconnects and replacement instances. This write locks in both SQL dialects.
	locked, err := tx.Exec(s.rebind(`update nodes set id = id where id = (select node_id from node_instances where id = ?)`), nodeInstanceID)
	if err != nil {
		return nil, false, err
	}
	if affected, err := locked.RowsAffected(); err != nil || affected != 1 {
		return nil, false, ErrNotFound
	}
	var existingNodeInstanceID string
	err = tx.QueryRow(s.rebind(`select node_instance_id from usage_records where request_id = ? limit 1`), requestID).Scan(&existingNodeInstanceID)
	if err == nil {
		if existingNodeInstanceID != nodeInstanceID {
			return nil, true, errors.New("usage request_id belongs to another node instance")
		}
		totals, queryErr := s.usageTotalsForRequest(context.Background(), tx, requestID)
		return totals, true, queryErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	for _, record := range records {
		if record.ConnectionID == "" || record.TXBytes < 0 || record.RXBytes < 0 {
			return nil, false, errors.New("usage report contains invalid values")
		}
		if err = s.requireConnectionOnInstance(context.Background(), tx, nodeInstanceID, record.ConnectionID); err != nil {
			return nil, false, err
		}
		if _, err = tx.Exec(s.rebind(`insert into usage_records(request_id, connection_id, node_instance_id, from_ms, to_ms, received_at_ms, tx_bytes, rx_bytes) values(?, ?, ?, ?, ?, ?, ?, ?)`), requestID, record.ConnectionID, nodeInstanceID, record.FromMS, record.ToMS, receivedAtMS, record.TXBytes, record.RXBytes); err != nil {
			return nil, false, err
		}
		if _, err = tx.Exec(s.rebind(`insert into connection_usage_totals(connection_id, tx_bytes, rx_bytes, updated_at_ms) values(?, ?, ?, ?)
on conflict(connection_id) do update set tx_bytes = connection_usage_totals.tx_bytes + excluded.tx_bytes, rx_bytes = connection_usage_totals.rx_bytes + excluded.rx_bytes, updated_at_ms = excluded.updated_at_ms`), record.ConnectionID, record.TXBytes, record.RXBytes, receivedAtMS); err != nil {
			return nil, false, err
		}
	}
	totals, err := s.usageTotalsForRequest(context.Background(), tx, requestID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return totals, false, nil
}

func (s *SQLStore) usageTotalsForRequest(ctx context.Context, tx *sql.Tx, requestID string) ([]model.ConnectionUsageTotal, error) {
	rows, err := tx.QueryContext(ctx, s.rebind(`select t.connection_id, t.tx_bytes, t.rx_bytes, t.updated_at_ms from connection_usage_totals t join usage_records r on r.connection_id = t.connection_id where r.request_id = ? order by t.connection_id`), requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var totals []model.ConnectionUsageTotal
	for rows.Next() {
		var total model.ConnectionUsageTotal
		if err := rows.Scan(&total.ConnectionID, &total.TXBytes, &total.RXBytes, &total.UpdatedAtMS); err != nil {
			return nil, err
		}
		totals = append(totals, total)
	}
	return totals, rows.Err()
}

func (s *SQLStore) requireConnectionOnInstance(ctx context.Context, tx *sql.Tx, instanceID, connectionID string) error {
	var count int
	err := tx.QueryRowContext(ctx, s.rebind(`select count(*) from connections c join node_instances i on i.node_id = c.node_id where i.id = ? and c.id = ?`), instanceID, connectionID).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("connection %s does not belong to node instance %s", connectionID, instanceID)
	}
	return nil
}

// NodeUsageSummary aggregates retained reports before limiting the recent list.
// Daily totals use the same interval-overlap rule as dashboard traffic.
func (s *SQLStore) NodeUsageSummary(instanceID string, fromMS, toMS int64, limit int) (model.NodeUsageSummary, error) {
	result := model.NodeUsageSummary{Items: []model.NodeConnectionUsage{}}
	rows, err := s.query(`with grouped as (
		select connection_id, min(from_ms) as first_ms, max(to_ms) as last_ms,
		 sum(tx_bytes) as tx, sum(rx_bytes) as rx,
		 sum(case when to_ms >= ? and from_ms < ? then tx_bytes else 0 end) as period_tx,
		 sum(case when to_ms >= ? and from_ms < ? then rx_bytes else 0 end) as period_rx
		from usage_records where node_instance_id = ? group by connection_id
	)
	select g.connection_id, coalesce(nullif(a.display_name, ''), a.username, ''), coalesce(c.name, ''),
	 g.first_ms, g.last_ms, g.tx, g.rx, sum(g.period_tx) over (), sum(g.period_rx) over ()
	from grouped g left join connections c on c.id = g.connection_id
	left join accounts a on a.id = c.account_id
	order by g.last_ms desc, g.connection_id limit ?`, fromMS, toMS, fromMS, toMS, instanceID, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.NodeConnectionUsage
		if err := rows.Scan(&item.ConnectionID, &item.AccountName, &item.ConnectionName, &item.FromMS, &item.ToMS, &item.TXBytes, &item.RXBytes, &result.TXBytes, &result.RXBytes); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (s *SQLStore) ListUsageRecords() []model.UsageRecord {
	return s.ListUsageRecordsRange(0, 0, 200000)
}

func (s *SQLStore) ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord {
	if limit <= 0 {
		limit = 200000
	}
	query := `select request_id, connection_id, node_instance_id, from_ms, to_ms, tx_bytes, rx_bytes from usage_records`
	args := []any{}
	if fromMS > 0 || toMS > 0 {
		query += ` where to_ms >= ? and from_ms <= ?`
		args = append(args, fromMS, toMS)
	}
	query += ` order by from_ms, request_id, connection_id limit ?`
	args = append(args, limit)
	rows, err := s.query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.UsageRecord
	for rows.Next() {
		var v model.UsageRecord
		if err := rows.Scan(&v.RequestID, &v.ConnectionID, &v.NodeInstanceID, &v.FromMS, &v.ToMS, &v.TXBytes, &v.RXBytes); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval {
	rows, err := s.query(`select id, agent_installation_id, node_instance_id, status, started_at_ms, ended_at_ms from node_status_intervals where started_at_ms <= ? and (ended_at_ms is null or ended_at_ms >= ?) order by started_at_ms`, toMS, fromMS)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.NodeStatusInterval
	for rows.Next() {
		var v model.NodeStatusInterval
		var ended sql.NullInt64
		if err := rows.Scan(&v.ID, &v.AgentInstallationID, &v.NodeInstanceID, &v.Status, &v.StartedAtMS, &ended); err == nil {
			if ended.Valid {
				v.EndedAtMS = ended.Int64
			}
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) CreateRuntimeCommand(v model.RuntimeCommand) (model.RuntimeCommand, error) {
	if v.ID == "" {
		v.ID = NewID("cmd")
	}
	if v.Status == "" {
		v.Status = model.RuntimeCommandStatusActive
	}
	if v.Status != model.RuntimeCommandStatusActive && v.Status != model.RuntimeCommandStatusCompleted && v.Status != model.RuntimeCommandStatusError {
		return model.RuntimeCommand{}, fmt.Errorf("unsupported runtime command status %q", v.Status)
	}
	v.IssuedAt, v.UpdatedAt = now(), now()
	_, err := s.exec(`insert into runtime_commands(id, agent_installation_id, type, payload, status, result, error, issued_at, expires_at, updated_at) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, v.ID, v.AgentInstallationID, v.Type, v.Payload, v.Status, v.Result, v.Error, ts(v.IssuedAt), tsOrNil(v.ExpiresAt), ts(v.UpdatedAt))
	return v, err
}

func (s *SQLStore) ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand {
	return s.listRuntimeCommands(` where agent_installation_id = ? and status = ? order by issued_at`, agentInstallationID, model.RuntimeCommandStatusActive)
}

func (s *SQLStore) ListRuntimeCommands(agentInstallationID string, limit int) []model.RuntimeCommand {
	if limit <= 0 {
		limit = 20
	}
	return s.listRuntimeCommands(` where agent_installation_id = ? order by issued_at desc limit ?`, agentInstallationID, limit)
}

func (s *SQLStore) listRuntimeCommands(suffix string, args ...any) []model.RuntimeCommand {
	rows, err := s.query(`select id, agent_installation_id, type, payload, status, result, error, issued_at, expires_at, updated_at from runtime_commands`+suffix, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []model.RuntimeCommand
	for rows.Next() {
		v, err := scanRuntimeCommand(rows)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *SQLStore) CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error {
	if status != model.RuntimeCommandStatusCompleted && status != model.RuntimeCommandStatusError {
		return fmt.Errorf("unsupported runtime command completion status %q", status)
	}
	_, err := s.exec(`update runtime_commands set status = ?, result = ?, error = ?, updated_at = ? where agent_installation_id = ? and id = ?`, status, result, errMsg, ts(now()), agentInstallationID, id)
	return err
}

func scanNodeRuntime(row scanner) (model.NodeRuntime, error) {
	var v model.NodeRuntime
	var heartbeat sql.NullString
	var updated string
	err := row.Scan(&v.AgentInstallationID, &v.NodeInstanceID, &v.AgentStatus, &heartbeat, &v.AgentVersion, &v.ProtocolVersion, &v.ServiceStatus, &v.RecentMessage, &updated)
	v.LastHeartbeatAt = parseNullTS(heartbeat)
	v.UpdatedAt = parseTS(updated)
	return v, convertErr(err)
}

func scanRuntimeCommand(row scanner) (model.RuntimeCommand, error) {
	var v model.RuntimeCommand
	var expires sql.NullString
	var issued, updated string
	err := row.Scan(&v.ID, &v.AgentInstallationID, &v.Type, &v.Payload, &v.Status, &v.Result, &v.Error, &issued, &expires, &updated)
	v.IssuedAt, v.ExpiresAt, v.UpdatedAt = parseTS(issued), parseNullTS(expires), parseTS(updated)
	return v, convertErr(err)
}
