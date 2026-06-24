package store

import (
	"fmt"
	"testing"

	"laz/internal/server/model"
)

func TestNodeUsageSummaryAggregatesBeforeLimiting(t *testing.T) {
	st := openTargetSQLite(t)
	account := mustCreateAccount(t, st, "usage-user")
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "usage-node")
	instance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	var newest string
	for i := 0; i < 12; i++ {
		bundle, err := st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, fmt.Sprintf("Phone %d", i)))
		if err != nil {
			t.Fatal(err)
		}
		id := bundle.Connection.ID
		newest = id
		for j, rec := range []model.UsageRecord{
			{ConnectionID: id, FromMS: 10, ToMS: 20, TXBytes: 100, RXBytes: 200},
			{ConnectionID: id, FromMS: 110, ToMS: int64(120 + i), TXBytes: 3, RXBytes: 4},
		} {
			if _, _, err := st.ApplyUsageRecords(fmt.Sprintf("report-%d-%d", i, j), instance.ID, []model.UsageRecord{rec}); err != nil {
				t.Fatal(err)
			}
		}
	}
	summary, err := st.NodeUsageSummary(instance.ID, 100, 200, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Items) != 10 || summary.TXBytes != 36 || summary.RXBytes != 48 {
		t.Fatalf("daily totals must include connections beyond the ten rows: %+v", summary)
	}
	first := summary.Items[0]
	if first.ConnectionID != newest || first.FromMS != 10 || first.ToMS != 131 || first.TXBytes != 103 || first.RXBytes != 204 || first.ConnectionName != "Phone 11" || first.AccountName == "" {
		t.Fatalf("unexpected grouped connection: %+v", first)
	}
	empty, err := st.NodeUsageSummary("missing", 100, 200, 10)
	if err != nil || len(empty.Items) != 0 || empty.TXBytes != 0 || empty.RXBytes != 0 {
		t.Fatalf("empty summary: %+v %v", empty, err)
	}
}
