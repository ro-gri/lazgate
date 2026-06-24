package dashboard

import (
	"math"
	"sort"
	"time"

	"laz/internal/server/model"
	"laz/internal/util"
)

const (
	defaultRange       = 24 * time.Hour
	defaultFreshness   = 90 * time.Second
	defaultRecordLimit = 200000
)

type Store interface {
	ListNodes() []model.Node
	ListNodeInstances() []model.NodeInstance
	ListAccounts() []model.Account
	ListConnections() []model.Connection
	ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord
	ListAllNodeOnlineConnections() []model.NodeOnlineConnection
	ListNodeRuntimes() []model.NodeRuntime
	ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval
}

type Service struct {
	store     Store
	now       func() time.Time
	freshness time.Duration
}

func New(st Store) *Service {
	return &Service{store: st, now: func() time.Time { return time.Now().UTC() }, freshness: defaultFreshness}
}

func NewForTest(st Store, now func() time.Time) *Service {
	return &Service{store: st, now: now, freshness: defaultFreshness}
}

type Request struct {
	FromMS int64
	ToMS   int64
	Bucket string
	Limit  int
}

type Response struct {
	Range             RangeBucket      `json:"range"`
	Summary           Summary          `json:"summary"`
	TrafficOverTime   []TrafficBucket  `json:"traffic_over_time"`
	Nodes             []NodeRow        `json:"nodes"`
	OnlineUsers       []OnlineUserRow  `json:"online_users"`
	TopUsersByTraffic []UserTrafficRow `json:"top_users_by_traffic"`
	TrafficByNode     []NodeTrafficRow `json:"traffic_by_node"`
	Downtime          []DowntimeRow    `json:"downtime"`
}

type RangeBucket struct {
	FromMS int64  `json:"from_ms"`
	ToMS   int64  `json:"to_ms"`
	Bucket string `json:"bucket"`
}

type Summary struct {
	OnlineNodes       int     `json:"online_nodes"`
	TotalNodes        int     `json:"total_nodes"`
	OnlineUsers       int     `json:"online_users"`
	TotalTrafficBytes int64   `json:"total_traffic_bytes"`
	AvailabilityPct   float64 `json:"availability_percent"`
	OfflineDurationMS int64   `json:"offline_duration_ms"`
}

type TrafficBucket struct {
	BucketStartMS int64  `json:"bucket_start_ms"`
	BucketLabel   string `json:"bucket_label"`
	RXBytes       int64  `json:"rx_bytes"`
	TXBytes       int64  `json:"tx_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
}

type NodeRow struct {
	NodeID            string  `json:"node_id"`
	Name              string  `json:"name"`
	Status            string  `json:"status"`
	ServiceStatus     string  `json:"service_status"`
	OnlineUsers       int     `json:"online_users"`
	OnlineConnections int     `json:"online_connections"`
	TrafficBytes      int64   `json:"traffic_bytes"`
	AvailabilityPct   float64 `json:"availability_percent"`
	OfflineDurationMS int64   `json:"offline_duration_ms"`
	LastHeartbeatMS   int64   `json:"last_heartbeat_ms"`
}

type OnlineUserRow struct {
	ConnectionID  string   `json:"connection_id"`
	NativeAuthIDs []string `json:"native_auth_ids"`
	DisplayName   string   `json:"display_name"`
	Nodes         []string `json:"nodes"`
	Connections   int      `json:"connections"`
	TrafficBytes  int64    `json:"traffic_bytes"`
	LastSeenMS    int64    `json:"last_seen_ms"`
}

type UserTrafficRow struct {
	AccountID    string `json:"account_id"`
	DisplayName  string `json:"display_name"`
	TrafficBytes int64  `json:"traffic_bytes"`
	RXBytes      int64  `json:"rx_bytes"`
	TXBytes      int64  `json:"tx_bytes"`
	Online       bool   `json:"online"`
}

type NodeTrafficRow struct {
	NodeID       string `json:"node_id"`
	Name         string `json:"name"`
	TrafficBytes int64  `json:"traffic_bytes"`
	Online       bool   `json:"online"`
}

type DowntimeRow struct {
	NodeID            string `json:"node_id"`
	Name              string `json:"name"`
	OfflineDurationMS int64  `json:"offline_duration_ms"`
	CurrentlyOffline  bool   `json:"currently_offline"`
}

type trafficAgg struct {
	rx    int64
	tx    int64
	total int64
}

func (s *Service) Build(req Request) Response {
	now := s.now().UTC()
	fromMS, toMS := normalizeRange(req, now)
	bucket := bucketKind(req.Bucket, toMS-fromMS)
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	nodes := s.store.ListNodes()
	instances := s.store.ListNodeInstances()
	instanceToNode, activeInstance := nodeInstanceMaps(instances)
	runtimes := mapByInstanceRuntime(s.store.ListNodeRuntimes())
	onlineFresh := s.currentOnline(s.store.ListAllNodeOnlineConnections(), runtimes, now)
	usage := s.store.ListUsageRecordsRange(fromMS, toMS, defaultRecordLimit)
	connectionNames := s.connectionNames()
	nodeNames := map[string]string{}
	for _, node := range nodes {
		nodeNames[node.ID] = node.Name
	}

	total := trafficAgg{}
	byConnection := map[string]trafficAgg{}
	byNode := map[string]trafficAgg{}
	for _, rec := range usage {
		total.rx += rec.RXBytes
		total.tx += rec.TXBytes
		total.total += rec.TotalBytes()
		connectionTraffic := byConnection[rec.ConnectionID]
		connectionTraffic.rx += rec.RXBytes
		connectionTraffic.tx += rec.TXBytes
		connectionTraffic.total += rec.TotalBytes()
		byConnection[rec.ConnectionID] = connectionTraffic
		nodeID := instanceToNode[rec.NodeInstanceID]
		node := byNode[nodeID]
		node.rx += rec.RXBytes
		node.tx += rec.TXBytes
		node.total += rec.TotalBytes()
		byNode[nodeID] = node
	}

	intervals := s.store.ListNodeStatusIntervals(fromMS, toMS)
	intervalsByInstance := map[string][]model.NodeStatusInterval{}
	for _, item := range intervals {
		intervalsByInstance[item.NodeInstanceID] = append(intervalsByInstance[item.NodeInstanceID], item)
	}

	nodeRows := make([]NodeRow, 0, len(nodes))
	downtime := []DowntimeRow{}
	onlineNodes := 0
	var totalOffline int64
	var availabilityOnline int64
	var totalNodeDuration int64
	for _, node := range nodes {
		instanceID := activeInstance[node.ID]
		rt := runtimes[instanceID]
		status := currentNodeStatus(rt, now, s.freshness)
		if status == "online" {
			onlineNodes++
		}
		effectiveFromMS := effectiveNodeFromMS(node, fromMS, toMS)
		effectiveDuration := maxInt64(0, toMS-effectiveFromMS)
		offline := offlineDuration(node, intervalsByInstance[instanceID], rt, effectiveFromMS, toMS, now, s.freshness)
		if offline > effectiveDuration {
			offline = effectiveDuration
		}
		onlineDuration := maxInt64(0, effectiveDuration-offline)
		availabilityOnline += onlineDuration
		totalNodeDuration += effectiveDuration
		totalOffline += offline
		onlineCount, connectionCount := onlineCountsForInstance(onlineFresh, instanceID, s.store.ListConnections())
		row := NodeRow{
			NodeID:            node.ID,
			Name:              node.Name,
			Status:            status,
			ServiceStatus:     valueOr(rt.ServiceStatus, "unknown"),
			OnlineUsers:       onlineCount,
			OnlineConnections: connectionCount,
			TrafficBytes:      byNode[node.ID].total,
			AvailabilityPct:   pct(onlineDuration, effectiveDuration),
			OfflineDurationMS: offline,
			LastHeartbeatMS:   util.UnixMilli(rt.LastHeartbeatAt),
		}
		nodeRows = append(nodeRows, row)
		if offline > 0 {
			downtime = append(downtime, DowntimeRow{NodeID: node.ID, Name: node.Name, OfflineDurationMS: offline, CurrentlyOffline: status != "online"})
		}
	}
	sort.Slice(nodeRows, func(i, j int) bool { return nodeRows[i].Name < nodeRows[j].Name })
	sort.Slice(downtime, func(i, j int) bool { return downtime[i].OfflineDurationMS > downtime[j].OfflineDurationMS })

	onlineUsers := onlineUserRows(onlineFresh, connectionNames, nodeNames, instanceToNode, byConnection, limit)
	topUsers := userTrafficRows(byConnection, s.store.ListAccounts(), s.store.ListConnections(), onlineFresh, limit)
	trafficByNode := nodeTrafficRows(byNode, nodes, activeInstance, runtimes, now, s.freshness, limit)
	return Response{
		Range:             RangeBucket{FromMS: fromMS, ToMS: toMS, Bucket: bucket},
		Summary:           Summary{OnlineNodes: onlineNodes, TotalNodes: len(nodes), OnlineUsers: len(onlineFresh), TotalTrafficBytes: total.total, AvailabilityPct: pct(availabilityOnline, totalNodeDuration), OfflineDurationMS: totalOffline},
		TrafficOverTime:   trafficBuckets(usage, fromMS, toMS, bucket),
		Nodes:             nodeRows,
		OnlineUsers:       onlineUsers,
		TopUsersByTraffic: topUsers,
		TrafficByNode:     trafficByNode,
		Downtime:          downtime,
	}
}

func normalizeRange(req Request, now time.Time) (int64, int64) {
	to := req.ToMS
	if to <= 0 {
		to = now.UnixMilli()
	}
	from := req.FromMS
	if from <= 0 {
		from = to - int64(defaultRange/time.Millisecond)
	}
	if from >= to {
		from = to - int64(defaultRange/time.Millisecond)
	}
	maxRange := int64((366 * 24 * time.Hour) / time.Millisecond)
	if to-from > maxRange {
		from = to - maxRange
	}
	return from, to
}

func bucketKind(raw string, durationMS int64) string {
	if raw == "hour" || raw == "day" || raw == "week" {
		return raw
	}
	duration := time.Duration(durationMS) * time.Millisecond
	if duration <= 48*time.Hour {
		return "hour"
	}
	if duration <= 45*24*time.Hour {
		return "day"
	}
	return "week"
}

func bucketSize(kind string) time.Duration {
	switch kind {
	case "hour":
		return time.Hour
	case "day":
		return 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

func trafficBuckets(records []model.UsageRecord, fromMS, toMS int64, kind string) []TrafficBucket {
	size := bucketSize(kind)
	from := time.UnixMilli(fromMS).UTC().Truncate(size)
	if from.UnixMilli() > fromMS {
		from = from.Add(-size)
	}
	var buckets []TrafficBucket
	index := map[int64]int{}
	for t := from; t.UnixMilli() < toMS; t = t.Add(size) {
		key := t.UnixMilli()
		index[key] = len(buckets)
		buckets = append(buckets, TrafficBucket{BucketStartMS: key, BucketLabel: bucketLabel(t, kind)})
	}
	for _, rec := range records {
		key := time.UnixMilli(maxInt64(fromMS, rec.FromMS)).UTC().Truncate(size).UnixMilli()
		i, ok := index[key]
		if !ok {
			continue
		}
		buckets[i].RXBytes += rec.RXBytes
		buckets[i].TXBytes += rec.TXBytes
		buckets[i].TotalBytes += rec.TotalBytes()
	}
	return buckets
}

func bucketLabel(t time.Time, kind string) string {
	switch kind {
	case "hour":
		return t.Format("15:04")
	case "day":
		return t.Format("Jan 02")
	default:
		return t.Format("2006-01-02")
	}
}

func (s *Service) currentOnline(items []model.NodeOnlineConnection, runtimes map[string]model.NodeRuntime, now time.Time) map[string][]model.NodeOnlineConnection {
	out := map[string][]model.NodeOnlineConnection{}
	cutoff := now.Add(-s.freshness)
	for _, item := range items {
		rt := runtimes[item.NodeInstanceID]
		if currentNodeStatus(rt, now, s.freshness) != "online" {
			continue
		}
		if item.LastSeenAt.Before(cutoff) {
			continue
		}
		key := item.ConnectionID
		if key == "" {
			key = "unknown:" + item.NodeInstanceID + ":" + item.NativeAuthID
		}
		out[key] = append(out[key], item)
	}
	return out
}

func (s *Service) connectionNames() map[string]string {
	names := map[string]string{}
	accounts := map[string]model.Account{}
	for _, account := range s.store.ListAccounts() {
		accounts[account.ID] = account
	}
	for _, connection := range s.store.ListConnections() {
		account := accounts[connection.AccountID]
		name := account.DisplayName
		if name == "" {
			name = account.Username
		}
		if connection.Name != "" {
			if name != "" {
				name += " · "
			}
			name += connection.Name
		}
		names[connection.ID] = name
	}
	return names
}

func onlineUserRows(online map[string][]model.NodeOnlineConnection, names, nodeNames, instanceToNode map[string]string, traffic map[string]trafficAgg, limit int) []OnlineUserRow {
	rows := make([]OnlineUserRow, 0, len(online))
	for connectionID, items := range online {
		nodes := map[string]bool{}
		nativeIDs := map[string]bool{}
		var count int
		var last time.Time
		for _, item := range items {
			nativeIDs[item.NativeAuthID] = true
			nodeID := instanceToNode[item.NodeInstanceID]
			nodes[valueOr(nodeNames[nodeID], nodeID)] = true
			count += item.Count
			if item.LastSeenAt.After(last) {
				last = item.LastSeenAt
			}
		}
		nodeList := make([]string, 0, len(nodes))
		for node := range nodes {
			nodeList = append(nodeList, node)
		}
		sort.Strings(nodeList)
		ids := make([]string, 0, len(nativeIDs))
		for id := range nativeIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		id := items[0].ConnectionID
		name := displayName(names, id)
		if id == "" {
			name = "Неизвестное подключение"
		}
		rows = append(rows, OnlineUserRow{ConnectionID: id, NativeAuthIDs: ids, DisplayName: name, Nodes: nodeList, Connections: count, TrafficBytes: traffic[connectionID].total, LastSeenMS: util.UnixMilli(last)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TrafficBytes == rows[j].TrafficBytes {
			return rows[i].DisplayName < rows[j].DisplayName
		}
		return rows[i].TrafficBytes > rows[j].TrafficBytes
	})
	return limitRows(rows, limit)
}

func userTrafficRows(traffic map[string]trafficAgg, accounts []model.Account, connections []model.Connection, online map[string][]model.NodeOnlineConnection, limit int) []UserTrafficRow {
	byAccount := map[string]trafficAgg{}
	accountOnline := map[string]bool{}
	for _, connection := range connections {
		if len(online[connection.ID]) > 0 {
			accountOnline[connection.AccountID] = true
		}
		if usage, ok := traffic[connection.ID]; ok {
			agg := byAccount[connection.AccountID]
			agg.rx += usage.rx
			agg.tx += usage.tx
			agg.total += usage.total
			byAccount[connection.AccountID] = agg
		}
	}
	rows := make([]UserTrafficRow, 0, len(byAccount))
	for _, account := range accounts {
		agg, ok := byAccount[account.ID]
		if !ok {
			continue
		}
		name := valueOr(account.DisplayName, valueOr(account.Username, account.ID))
		rows = append(rows, UserTrafficRow{AccountID: account.ID, DisplayName: name, TrafficBytes: agg.total, RXBytes: agg.rx, TXBytes: agg.tx, Online: accountOnline[account.ID]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TrafficBytes == rows[j].TrafficBytes {
			return rows[i].AccountID < rows[j].AccountID
		}
		return rows[i].TrafficBytes > rows[j].TrafficBytes
	})
	return limitRows(rows, limit)
}

func nodeTrafficRows(traffic map[string]trafficAgg, nodes []model.Node, activeInstance map[string]string, runtimes map[string]model.NodeRuntime, now time.Time, freshness time.Duration, limit int) []NodeTrafficRow {
	rows := make([]NodeTrafficRow, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, NodeTrafficRow{NodeID: node.ID, Name: node.Name, TrafficBytes: traffic[node.ID].total, Online: currentNodeStatus(runtimes[activeInstance[node.ID]], now, freshness) == "online"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TrafficBytes > rows[j].TrafficBytes })
	return limitRows(rows, limit)
}

func effectiveNodeFromMS(node model.Node, fromMS, toMS int64) int64 {
	if node.CreatedAt.IsZero() {
		return fromMS
	}
	createdAtMS := node.CreatedAt.UnixMilli()
	if createdAtMS > toMS {
		return toMS
	}
	if createdAtMS > fromMS {
		return createdAtMS
	}
	return fromMS
}

func offlineDuration(node model.Node, intervals []model.NodeStatusInterval, runtime model.NodeRuntime, fromMS, toMS int64, now time.Time, freshness time.Duration) int64 {
	if runtime.AgentInstallationID == "" || (runtime.LastHeartbeatAt.IsZero() && len(intervals) == 0) {
		if node.CreatedAt.IsZero() {
			return 0
		}
		return intersectDuration(fromMS, toMS, node.CreatedAt.UnixMilli(), toMS)
	}
	var offline int64
	for _, item := range intervals {
		end := item.EndedAtMS
		if end == 0 {
			end = toMS
		}
		if item.Status != "online" {
			offline += intersectDuration(fromMS, toMS, item.StartedAtMS, end)
		}
	}
	if currentNodeStatus(runtime, now, freshness) != "online" && !runtime.LastHeartbeatAt.IsZero() {
		staleStart := runtime.LastHeartbeatAt.Add(freshness).UnixMilli()
		offline += intersectDuration(fromMS, toMS, staleStart, toMS)
	}
	if offline > toMS-fromMS {
		return toMS - fromMS
	}
	return offline
}

func currentNodeStatus(runtime model.NodeRuntime, now time.Time, freshness time.Duration) string {
	if runtime.AgentInstallationID == "" || runtime.LastHeartbeatAt.IsZero() {
		return "offline"
	}
	if now.Sub(runtime.LastHeartbeatAt) > freshness {
		return "offline"
	}
	if runtime.AgentStatus != "online" {
		return valueOr(runtime.AgentStatus, "offline")
	}
	if runtime.ServiceStatus != "" && runtime.ServiceStatus != "active" {
		return "degraded"
	}
	return "online"
}

func onlineCountsForInstance(online map[string][]model.NodeOnlineConnection, instanceID string, connectionsList []model.Connection) (int, int) {
	var users, connections int
	accountByConnection := map[string]string{}
	for _, connection := range connectionsList {
		accountByConnection[connection.ID] = connection.AccountID
	}
	seenAccounts := map[string]bool{}
	for connectionID, items := range online {
		hasNode := false
		for _, item := range items {
			if item.NodeInstanceID != instanceID {
				continue
			}
			hasNode = true
			connections += item.Count
		}
		if hasNode {
			seenAccounts[accountByConnection[connectionID]] = true
		}
	}
	for accountID := range seenAccounts {
		if accountID != "" {
			users++
		}
	}
	return users, connections
}

func mapByInstanceRuntime(items []model.NodeRuntime) map[string]model.NodeRuntime {
	out := map[string]model.NodeRuntime{}
	for _, item := range items {
		out[item.NodeInstanceID] = item
	}
	return out
}

func nodeInstanceMaps(items []model.NodeInstance) (map[string]string, map[string]string) {
	instanceToNode, active := map[string]string{}, map[string]string{}
	for _, item := range items {
		instanceToNode[item.ID] = item.NodeID
		if item.Status == model.StatusActive {
			active[item.NodeID] = item.ID
		}
	}
	return instanceToNode, active
}

func intersectDuration(from, to, a, b int64) int64 {
	start := maxInt64(from, a)
	end := minInt64(to, b)
	if end <= start {
		return 0
	}
	return end - start
}

func pct(num, den int64) float64 {
	if den <= 0 {
		return 100
	}
	value := (float64(num) / float64(den)) * 100
	return math.Round(value*10) / 10
}

func displayName(names map[string]string, connectionID string) string {
	if name := names[connectionID]; name != "" {
		return name
	}
	return connectionID
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func limitRows[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
