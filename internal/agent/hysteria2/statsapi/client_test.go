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

	client := New(server.URL, "secret")
	if err := client.Kick(context.Background(), "cred_1"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "secret" {
		t.Fatalf("unexpected authorization %q", gotAuth)
	}
	if len(gotIDs) != 1 || gotIDs[0] != "cred_1" {
		t.Fatalf("unexpected kick payload %#v", gotIDs)
	}
}
