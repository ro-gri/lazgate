package dashboard

import (
	"testing"

	"laz/internal/server/model"
)

func TestUserTrafficRowsAggregateAccountsBeforeLimiting(t *testing.T) {
	accounts := []model.Account{{ID: "a", DisplayName: "Alice"}, {ID: "b", Username: "bob"}}
	connections := []model.Connection{
		{ID: "a1", AccountID: "a"}, {ID: "a2", AccountID: "a"},
		{ID: "a3", AccountID: "a"}, {ID: "b1", AccountID: "b"},
	}
	traffic := map[string]trafficAgg{
		"a1": {rx: 30, tx: 40, total: 70},
		"a2": {rx: 50, tx: 10, total: 60},
		"b1": {rx: 90, tx: 10, total: 100},
	}
	// An online connection without traffic in the selected period still makes its account online.
	online := map[string][]model.NodeOnlineConnection{"a3": {{ConnectionID: "a3"}}}
	rows := userTrafficRows(traffic, accounts, connections, online, 1)
	if len(rows) != 1 || rows[0].AccountID != "a" || rows[0].DisplayName != "Alice" || rows[0].TrafficBytes != 130 || rows[0].RXBytes != 80 || rows[0].TXBytes != 50 || !rows[0].Online {
		t.Fatalf("unexpected top account: %+v", rows)
	}
	rows = userTrafficRows(traffic, accounts, connections, online, 10)
	if len(rows) != 2 || rows[1].DisplayName != "bob" || rows[1].Online {
		t.Fatalf("unexpected account rows: %+v", rows)
	}
}
