package store

import (
	"fmt"
	"testing"

	"laz/internal/server/model"
)

func TestSQLiteUsageRecordsAtomicity(t *testing.T) {
	testUsageRecordsAtomicity(t, openTargetSQLite(t))
}

func testUsageRecordsAtomicity(t *testing.T, st *SQLStore) {
	t.Helper()
	account := mustCreateAccount(t, st, "traffic-test")
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "traffic-test")
	instance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(testHysteriaBundle(account.ID, node.ID, "Traffic"))
	if err != nil {
		t.Fatal(err)
	}
	records := []model.UsageRecord{{ConnectionID: bundle.Connection.ID, FromMS: 100, ToMS: 200, RXBytes: 10, TXBytes: 20}}
	invalid := append(append([]model.UsageRecord{}, records...), model.UsageRecord{ConnectionID: "missing", RXBytes: 1})
	if _, _, err := st.ApplyUsageRecords("atomic-request", instance.ID, invalid); err == nil {
		t.Fatal("invalid second record must reject the whole message")
	}
	var count int
	if err := st.queryRow(`select count(*) from usage_records where request_id = ?`, "atomic-request").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial history survived rollback: count=%d err=%v", count, err)
	}
	if err := st.queryRow(`select count(*) from connection_usage_totals where connection_id = ?`, bundle.Connection.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial totals survived rollback: count=%d err=%v", count, err)
	}

	start := make(chan struct{})
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			totals, _, err := st.ApplyUsageRecords("atomic-request", instance.ID, records)
			if err == nil && (len(totals) != 1 || totals[0].RXBytes != 10 || totals[0].TXBytes != 20) {
				err = fmt.Errorf("unexpected totals after concurrent retry: %+v", totals)
			}
			results <- err
		}()
	}
	close(start)
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	otherInstance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ApplyUsageRecords("atomic-request", otherInstance.ID, records); err == nil {
		t.Fatal("another instance must not replay an existing request")
	}
	if _, _, err := st.ApplyUsageRecords("empty-request", instance.ID, nil); err == nil {
		t.Fatal("empty message cannot leave a deduplication record and must be rejected")
	}
}
