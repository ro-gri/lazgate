package statsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKickSendsHysteriaStatsAPIArrayPayload(t *testing.T) {
	var gotAuth string
	var gotIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/kick" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %q", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content-type %q", got)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotIDs); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	nativeAuthID := "22222222-2222-4222-8222-222222222222"
	client := New(server.URL, "secret")
	if err := client.Kick(context.Background(), nativeAuthID); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "secret" {
		t.Fatalf("unexpected authorization %q", gotAuth)
	}
	if len(gotIDs) != 1 || gotIDs[0] != nativeAuthID {
		t.Fatalf("unexpected kick payload %#v", gotIDs)
	}
}

func TestTrafficNeverRequestsCounterReset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traffic" || r.URL.RawQuery != "" {
			t.Fatalf("unexpected traffic request %q?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connection-1": map[string]int64{"tx": 20, "rx": 10}})
	}))
	defer server.Close()
	records, err := New(server.URL, "").Traffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].NativeAuthID != "connection-1" || records[0].TXBytes != 20 || records[0].RXBytes != 10 {
		t.Fatalf("unexpected records: %+v", records)
	}
}
