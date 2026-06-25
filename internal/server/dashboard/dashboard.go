package dashboard

import (
	"math"
	"sort"
	"time"

	"laz/internal/server/model"
)

const (
	defaultRange       = 24 * time.Hour
	defaultFreshness   = 90 * time.Second
	defaultRecordLimit = 200000
)

type Store interface {
	ListNodes() []model.Node
	ListAccounts() []model.Account
	ListConnections() []model.Connection
	ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord
	ListAllNodeOnlineClients() []model.NodeOnlineClient
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
	HysteriaStatus    string  `json:"hysteria_status"`
	OnlineUsers       int     `json:"online_users"`
	OnlineConnections int     `json:"online_connections"`
	TrafficBytes      int64   `json:"traffic_bytes"`
	AvailabilityPct   float64 `json:"availability_percent"`
	OfflineDurationMS int64   `json:"offline_duration_ms"`
	LastHeartbeatMS   int64   `json:"last_heartbeat_ms"`
}

type OnlineUserRow struct {
	CredentialID string   `json:"credential_id"`
	DisplayName  string   `json:"display_name"`
	Nodes        []string `json:"nodes"`
	Connections  int      `json:"connections"`
	TrafficBytes int64    `json:"traffic_bytes"`
	LastSeenMS   int64    `json:"last_seen_ms"`
}

type UserTrafficRow struct {
	CredentialID string `json:"credential_id"`
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
	runtimes := mapByNodeRuntime(s.store.ListNodeRuntimes())
	onlineFresh := s.currentOnline(s.store.ListAllNodeOnlineClients(), runtimes, now)
	usage := s.store.ListUsageRecordsRange(fromMS, toMS, defaultRecordLimit)
	credentialNames := s.credentialNames()
	nodeNames := map[string]string{}
	for _, node := range nodes {
		nodeNames[node.ID] = node.Name
	}

	total := trafficAgg{}
	byCredential := map[string]trafficAgg{}
	byNode := map[string]trafficAgg{}
	for _, rec := range usage {
		total.rx += rec.RXBytes
		total.tx += rec.TXBytes
		total.total += rec.TotalBytes
		cred := byCredential[rec.CredentialID]
		cred.rx += rec.RXBytes
		cred.tx += rec.TXBytes
		cred.total += rec.TotalBytes
		byCredential[rec.CredentialID] = cred
		node := byNode[rec.NodeID]
		node.rx += rec.RXBytes
		node.tx += rec.TXBytes
		node.total += rec.TotalBytes
		byNode[rec.NodeID] = node
	}

	intervals := s.store.ListNodeStatusIntervals(fromMS, toMS)
	intervalsByNode := map[string][]model.NodeStatusInterval{}
	for _, item := range intervals {
		intervalsByNode[item.NodeID] = append(intervalsByNode[item.NodeID], item)
	}

	nodeRows := make([]NodeRow, 0, len(nodes))
	downtime := []DowntimeRow{}
	onlineNodes := 0
	var totalOffline int64
	var availabilityOnline int64
	rangeDuration := maxInt64(1, toMS-fromMS)
	for _, node := range nodes {
		rt := runtimes[node.ID]
		status := currentNodeStatus(rt, now, s.freshness)
		if status == "online" {
			onlineNodes++
		}
		offline := offlineDuration(intervalsByNode[node.ID], rt, fromMS, toMS, now, s.freshness)
		onlineDuration := maxInt64(0, rangeDuration-offline)
		availabilityOnline += onlineDuration
		totalOffline += offline
		onlineCount, connectionCount := onlineCountsForNode(onlineFresh, node.ID)
		row := NodeRow{
			NodeID:            node.ID,
			Name:              node.Name,
			Status:            status,
			HysteriaStatus:    valueOr(rt.HysteriaServiceStatus, "unknown"),
			OnlineUsers:       onlineCount,
			OnlineConnections: connectionCount,
			TrafficBytes:      byNode[node.ID].total,
			AvailabilityPct:   pct(onlineDuration, rangeDuration),
			OfflineDurationMS: offline,
			LastHeartbeatMS:   timeMS(rt.LastHeartbeatAt),
		}
		nodeRows = append(nodeRows, row)
		if offline > 0 {
			downtime = append(downtime, DowntimeRow{NodeID: node.ID, Name: node.Name, OfflineDurationMS: offline, CurrentlyOffline: status != "online"})
		}
	}
	sort.Slice(nodeRows, func(i, j int) bool { return nodeRows[i].Name < nodeRows[j].Name })
	sort.Slice(downtime, func(i, j int) bool { return downtime[i].OfflineDurationMS > downtime[j].OfflineDurationMS })

	onlineUsers := onlineUserRows(onlineFresh, credentialNames, nodeNames, byCredential, limit)
	topUsers := userTrafficRows(byCredential, credentialNames, onlineFresh, limit)
	trafficByNode := nodeTrafficRows(byNode, nodes, runtimes, now, s.freshness, limit)
	totalNodeDuration := rangeDuration * int64(maxInt(1, len(nodes)))
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
		buckets[i].TotalBytes += rec.TotalBytes
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

func (s *Service) currentOnline(items []model.NodeOnlineClient, runtimes map[string]model.NodeRuntime, now time.Time) map[string][]model.NodeOnlineClient {
	out := map[string][]model.NodeOnlineClient{}
	cutoff := now.Add(-s.freshness)
	for _, item := range items {
		rt := runtimes[item.NodeID]
		if currentNodeStatus(rt, now, s.freshness) != "online" {
			continue
		}
		if item.LastSeenAt.Before(cutoff) {
			continue
		}
		out[item.CredentialID] = append(out[item.CredentialID], item)
	}
	return out
}

func (s *Service) credentialNames() map[string]string {
	accounts := map[string]model.Account{}
	for _, account := range s.store.ListAccounts() {
		accounts[account.ID] = account
	}
	names := map[string]string{}
	for _, c := range s.store.ListConnections() {
		account := accounts[c.AccountID]
		name := account.DisplayName
		if name == "" {
			name = account.Username
		}
		if name == "" {
			name = c.RemoteName
		}
		names[c.ID] = name
	}
	return names
}

func onlineUserRows(online map[string][]model.NodeOnlineClient, names, nodeNames map[string]string, traffic map[string]trafficAgg, limit int) []OnlineUserRow {
	rows := make([]OnlineUserRow, 0, len(online))
	for credentialID, items := range online {
		nodes := map[string]bool{}
		var count int
		var last time.Time
		for _, item := range items {
			nodes[valueOr(nodeNames[item.NodeID], item.NodeID)] = true
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
		rows = append(rows, OnlineUserRow{CredentialID: credentialID, DisplayName: displayName(names, credentialID), Nodes: nodeList, Connections: count, TrafficBytes: traffic[credentialID].total, LastSeenMS: timeMS(last)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TrafficBytes == rows[j].TrafficBytes {
			return rows[i].DisplayName < rows[j].DisplayName
		}
		return rows[i].TrafficBytes > rows[j].TrafficBytes
	})
	return limitRows(rows, limit)
}

func userTrafficRows(traffic map[string]trafficAgg, names map[string]string, online map[string][]model.NodeOnlineClient, limit int) []UserTrafficRow {
	rows := make([]UserTrafficRow, 0, len(traffic))
	for credentialID, agg := range traffic {
		rows = append(rows, UserTrafficRow{CredentialID: credentialID, DisplayName: displayName(names, credentialID), TrafficBytes: agg.total, RXBytes: agg.rx, TXBytes: agg.tx, Online: len(online[credentialID]) > 0})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TrafficBytes > rows[j].TrafficBytes })
	return limitRows(rows, limit)
}

func nodeTrafficRows(traffic map[string]trafficAgg, nodes []model.Node, runtimes map[string]model.NodeRuntime, now time.Time, freshness time.Duration, limit int) []NodeTrafficRow {
	rows := make([]NodeTrafficRow, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, NodeTrafficRow{NodeID: node.ID, Name: node.Name, TrafficBytes: traffic[node.ID].total, Online: currentNodeStatus(runtimes[node.ID], now, freshness) == "online"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TrafficBytes > rows[j].TrafficBytes })
	return limitRows(rows, limit)
}

func offlineDuration(intervals []model.NodeStatusInterval, runtime model.NodeRuntime, fromMS, toMS int64, now time.Time, freshness time.Duration) int64 {
	if runtime.NodeID == "" || (runtime.LastHeartbeatAt.IsZero() && len(intervals) == 0) {
		return toMS - fromMS
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
	if runtime.NodeID == "" || runtime.LastHeartbeatAt.IsZero() {
		return "offline"
	}
	if now.Sub(runtime.LastHeartbeatAt) > freshness {
		return "offline"
	}
	if runtime.AgentStatus != "online" {
		return valueOr(runtime.AgentStatus, "offline")
	}
	if runtime.HysteriaServiceStatus != "" && runtime.HysteriaServiceStatus != "active" {
		return "degraded"
	}
	return "online"
}

func onlineCountsForNode(online map[string][]model.NodeOnlineClient, nodeID string) (int, int) {
	var users, connections int
	for _, items := range online {
		hasNode := false
		for _, item := range items {
			if item.NodeID != nodeID {
				continue
			}
			hasNode = true
			connections += item.Count
		}
		if hasNode {
			users++
		}
	}
	return users, connections
}

func mapByNodeRuntime(items []model.NodeRuntime) map[string]model.NodeRuntime {
	out := map[string]model.NodeRuntime{}
	for _, item := range items {
		out[item.NodeID] = item
	}
	return out
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

func timeMS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func displayName(names map[string]string, credentialID string) string {
	if name := names[credentialID]; name != "" {
		return name
	}
	return credentialID
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
