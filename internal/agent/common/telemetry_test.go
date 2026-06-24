package common

import (
	"context"
	"errors"
	agentstore "laz/internal/agent/common/store"
	"testing"
	"time"
)

type testTelemetrySource struct {
	calls  int
	result NativeTelemetry
}

func (s *testTelemetrySource) ReadTelemetry(context.Context, time.Time) NativeTelemetry {
	s.calls++
	return s.result
}

type testAuthLookup struct {
	calls map[string]int
	err   error
}

func (l testAuthLookup) GetConnectionIDByNativeAuthID(_ context.Context, id string) (string, error) {
	l.calls[id]++
	if l.err != nil {
		return "", l.err
	}
	if id == "known" {
		return "connection", nil
	}
	return "", agentstore.ErrNotFound
}
func TestTelemetrySharesCollectionAndLookup(t *testing.T) {
	source := &testTelemetrySource{result: NativeTelemetry{Traffic: []NativeTrafficRecord{{NativeAuthID: "known", RXBytes: 2, TXBytes: 3}, {NativeAuthID: "unknown", RXBytes: 100}}, Online: []NativeOnlineRecord{{NativeAuthID: "known", Count: 2}, {NativeAuthID: "unknown", Count: 1}}}}
	lookup := testAuthLookup{calls: map[string]int{}}
	result := NewTelemetryCollector(source, lookup).Collect(context.Background())
	if source.calls != 1 || lookup.calls["known"] != 1 || lookup.calls["unknown"] != 1 {
		t.Fatalf("calls: %d %v", source.calls, lookup.calls)
	}
	if result.TrafficError != nil || result.OnlineError != nil || len(result.Traffic.Records) != 1 || result.Traffic.Records[0].ConnectionID != "connection" {
		t.Fatalf("traffic: %+v", result)
	}
	if len(result.Online.Connections) != 2 || result.Online.Connections[1].ConnectionId != "" || result.Online.Connections[1].NativeAuthId != "unknown" {
		t.Fatalf("online: %+v", result.Online)
	}
	if result.Traffic.CollectedMS != result.Online.AtMs {
		t.Fatal("different collection timestamps")
	}
}
func TestTelemetryPartialFailureDoesNotDiscardOtherReport(t *testing.T) {
	source := &testTelemetrySource{result: NativeTelemetry{TrafficError: errors.New("traffic failed"), Online: []NativeOnlineRecord{{NativeAuthID: "unknown", Count: 1}}}}
	result := NewTelemetryCollector(source, testAuthLookup{calls: map[string]int{}}).Collect(context.Background())
	if result.TrafficError == nil || result.Online == nil || len(result.Online.Connections) != 1 {
		t.Fatalf("snapshot: %+v", result)
	}
}
func TestTelemetryDatabaseFailureIsNotUnknownIdentity(t *testing.T) {
	source := &testTelemetrySource{result: NativeTelemetry{Traffic: []NativeTrafficRecord{{NativeAuthID: "known"}}, Online: []NativeOnlineRecord{{NativeAuthID: "known", Count: 1}}}}
	result := NewTelemetryCollector(source, testAuthLookup{calls: map[string]int{}, err: errors.New("db failed")}).Collect(context.Background())
	if result.TrafficError == nil || result.OnlineError == nil || result.Online != nil {
		t.Fatalf("snapshot: %+v", result)
	}
}
