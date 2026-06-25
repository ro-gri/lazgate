package dashboard

import (
	"testing"
	"time"

	"laz/internal/server/model"
)

type fakeStore struct {
	nodes       []model.Node
	accounts    []model.Account
	connections []model.Connection
	usage       []model.UsageRecord
	online      []model.NodeOnlineClient
	runtimes    []model.NodeRuntime
	intervals   []model.NodeStatusInterval
}

func (f fakeStore) ListNodes() []model.Node             { return f.nodes }
func (f fakeStore) ListAccounts() []model.Account       { return f.accounts }
func (f fakeStore) ListConnections() []model.Connection { return f.connections }
func (f fakeStore) ListUsageRecordsRange(fromMS, toMS int64, limit int) []model.UsageRecord {
	var out []model.UsageRecord
	for _, rec := range f.usage {
		if rec.ToMS >= fromMS && rec.FromMS <= toMS {
			out = append(out, rec)
		}
	}
	return out
}
func (f fakeStore) ListAllNodeOnlineClients() []model.NodeOnlineClient { return f.online }
func (f fakeStore) ListNodeRuntimes() []model.NodeRuntime              { return f.runtimes }
func (f fakeStore) ListNodeStatusIntervals(fromMS, toMS int64) []model.NodeStatusInterval {
	var out []model.NodeStatusInterval
	for _, item := range f.intervals {
		end := item.EndedAtMS
		if end == 0 {
			end = toMS
		}
		if item.StartedAtMS <= toMS && end >= fromMS {
			out = append(out, item)
		}
	}
	return out
}

func TestDashboardDefaultRangeAndAggregations(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour).UnixMilli()
	st := baseDashboardStore(now)
	st.usage = []model.UsageRecord{
		{NodeID: "node_1", CredentialID: "con_1", FromMS: from + int64(time.Hour/time.Millisecond), ToMS: from + int64(2*time.Hour/time.Millisecond), RXBytes: 100, TXBytes: 20, TotalBytes: 120},
		{NodeID: "node_2", CredentialID: "con_1", FromMS: from + int64(3*time.Hour/time.Millisecond), ToMS: from + int64(4*time.Hour/time.Millisecond), RXBytes: 200, TXBytes: 30, TotalBytes: 230},
		{NodeID: "node_2", CredentialID: "con_2", FromMS: from + int64(5*time.Hour/time.Millisecond), ToMS: from + int64(6*time.Hour/time.Millisecond), RXBytes: 300, TXBytes: 40, TotalBytes: 340},
		{NodeID: "node_1", CredentialID: "con_old", FromMS: now.Add(-48 * time.Hour).UnixMilli(), ToMS: now.Add(-47 * time.Hour).UnixMilli(), RXBytes: 999, TXBytes: 999, TotalBytes: 1998},
	}
	got := NewForTest(st, func() time.Time { return now }).Build(Request{})
	if got.Range.ToMS != now.UnixMilli() || got.Range.FromMS != from || got.Range.Bucket != "hour" {
		t.Fatalf("unexpected default range: %+v", got.Range)
	}
	if got.Summary.TotalTrafficBytes != 690 {
		t.Fatalf("unexpected total traffic %d", got.Summary.TotalTrafficBytes)
	}
	if len(got.TopUsersByTraffic) < 2 || got.TopUsersByTraffic[0].CredentialID != "con_1" || got.TopUsersByTraffic[0].TrafficBytes != 350 {
		t.Fatalf("unexpected top users: %+v", got.TopUsersByTraffic)
	}
	if len(got.TrafficByNode) < 2 || got.TrafficByNode[0].NodeID != "node_2" || got.TrafficByNode[0].TrafficBytes != 570 {
		t.Fatalf("unexpected traffic by node: %+v", got.TrafficByNode)
	}
	if len(got.TrafficOverTime) < 24 {
		t.Fatalf("expected hourly buckets, got %d", len(got.TrafficOverTime))
	}
}

func TestDashboardRangesChooseExpectedBuckets(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	svc := NewForTest(fakeStore{}, func() time.Time { return now })
	if got := svc.Build(Request{FromMS: now.Add(-24 * time.Hour).UnixMilli(), ToMS: now.UnixMilli()}); got.Range.Bucket != "hour" {
		t.Fatalf("24h bucket = %s", got.Range.Bucket)
	}
	if got := svc.Build(Request{FromMS: now.Add(-7 * 24 * time.Hour).UnixMilli(), ToMS: now.UnixMilli()}); got.Range.Bucket != "day" {
		t.Fatalf("7d bucket = %s", got.Range.Bucket)
	}
	if got := svc.Build(Request{FromMS: now.Add(-30 * 24 * time.Hour).UnixMilli(), ToMS: now.UnixMilli()}); got.Range.Bucket != "day" {
		t.Fatalf("30d bucket = %s", got.Range.Bucket)
	}
	if got := svc.Build(Request{FromMS: now.Add(-90 * 24 * time.Hour).UnixMilli(), ToMS: now.UnixMilli()}); got.Range.Bucket != "week" {
		t.Fatalf("90d bucket = %s", got.Range.Bucket)
	}
}

func TestDashboardOnlineFreshnessAndAvailability(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	st := baseDashboardStore(now)
	st.runtimes = []model.NodeRuntime{
		{NodeID: "node_1", AgentStatus: "online", HysteriaServiceStatus: "active", LastHeartbeatAt: now.Add(-20 * time.Second)},
		{NodeID: "node_2", AgentStatus: "online", HysteriaServiceStatus: "active", LastHeartbeatAt: now.Add(-2 * time.Hour)},
	}
	st.online = []model.NodeOnlineClient{
		{NodeID: "node_1", CredentialID: "con_1", Count: 2, LastSeenAt: now.Add(-10 * time.Second)},
		{NodeID: "node_2", CredentialID: "con_2", Count: 1, LastSeenAt: now.Add(-10 * time.Second)},
		{NodeID: "node_1", CredentialID: "con_stale", Count: 1, LastSeenAt: now.Add(-2 * time.Hour)},
	}
	from := now.Add(-24 * time.Hour).UnixMilli()
	st.intervals = []model.NodeStatusInterval{
		{NodeID: "node_1", Status: "online", StartedAtMS: from, EndedAtMS: 0},
		{NodeID: "node_2", Status: "online", StartedAtMS: from, EndedAtMS: now.Add(-3 * time.Hour).UnixMilli()},
		{NodeID: "node_2", Status: "offline", StartedAtMS: now.Add(-3 * time.Hour).UnixMilli(), EndedAtMS: 0},
	}
	got := NewForTest(st, func() time.Time { return now }).Build(Request{FromMS: from, ToMS: now.UnixMilli()})
	if got.Summary.OnlineNodes != 1 || got.Summary.OnlineUsers != 1 {
		t.Fatalf("unexpected current summary: %+v", got.Summary)
	}
	if len(got.OnlineUsers) != 1 || got.OnlineUsers[0].CredentialID != "con_1" || got.OnlineUsers[0].Connections != 2 {
		t.Fatalf("unexpected online users: %+v", got.OnlineUsers)
	}
	var node2 NodeRow
	for _, node := range got.Nodes {
		if node.NodeID == "node_2" {
			node2 = node
		}
	}
	if node2.Status != "offline" || node2.OfflineDurationMS == 0 || node2.AvailabilityPct >= 100 {
		t.Fatalf("unexpected node2 availability: %+v", node2)
	}
	if len(got.Downtime) == 0 || !got.Downtime[0].CurrentlyOffline {
		t.Fatalf("expected current downtime: %+v", got.Downtime)
	}
}

func TestDashboardNodeWithoutHeartbeatIsOfflineForRange(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	st := fakeStore{nodes: []model.Node{{ID: "node_1", Name: "EU-1", Status: model.StatusActive}}}
	got := NewForTest(st, func() time.Time { return now }).Build(Request{})
	if got.Summary.OnlineNodes != 0 || got.Summary.OfflineDurationMS == 0 || got.Summary.AvailabilityPct != 0 {
		t.Fatalf("node without heartbeat should be offline: %+v", got.Summary)
	}
	if len(got.Downtime) != 1 || !got.Downtime[0].CurrentlyOffline {
		t.Fatalf("expected downtime row: %+v", got.Downtime)
	}
}

func baseDashboardStore(now time.Time) fakeStore {
	return fakeStore{
		nodes: []model.Node{
			{ID: "node_1", Name: "EU-1", Status: model.StatusActive},
			{ID: "node_2", Name: "TR-1", Status: model.StatusActive},
		},
		accounts: []model.Account{
			{ID: "acc_1", Username: "alice", DisplayName: "Alice"},
			{ID: "acc_2", Username: "bob", DisplayName: "Bob"},
		},
		connections: []model.Connection{
			{ID: "con_1", AccountID: "acc_1", NodeID: "node_1", RemoteName: "alice_phone"},
			{ID: "con_2", AccountID: "acc_2", NodeID: "node_2", RemoteName: "bob_phone"},
		},
		runtimes: []model.NodeRuntime{
			{NodeID: "node_1", AgentStatus: "online", HysteriaServiceStatus: "active", LastHeartbeatAt: now.Add(-10 * time.Second)},
			{NodeID: "node_2", AgentStatus: "online", HysteriaServiceStatus: "active", LastHeartbeatAt: now.Add(-10 * time.Second)},
		},
	}
}
