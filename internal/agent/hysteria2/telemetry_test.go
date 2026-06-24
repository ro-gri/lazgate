package hysteria2

import (
	"context"
	"laz/internal/legacy/agent/hysteria2/statsapi"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTelemetryReadsBothStatsEndpoints(t *testing.T) {
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/traffic":
			w.Write([]byte(`{"native":{"tx":20,"rx":10}}`))
		case "/online":
			w.Write([]byte(`{"native":2}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	result := (telemetrySource{stats: statsapi.New(server.URL, "")}).ReadTelemetry(context.Background(), time.Now())
	if result.TrafficError != nil || result.OnlineError != nil || calls["/traffic"] != 1 || calls["/online"] != 1 || len(result.Traffic) != 1 || len(result.Online) != 1 {
		t.Fatalf("calls %v snapshot %+v", calls, result)
	}
	if result.Traffic[0].NativeAuthID != "native" || result.Online[0].Count != 2 {
		t.Fatalf("snapshot %+v", result)
	}
}
func TestTelemetryOnlineSurvivesTrafficFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/online" {
			w.Write([]byte(`{"native":1}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	result := (telemetrySource{stats: statsapi.New(server.URL, "")}).ReadTelemetry(context.Background(), time.Now())
	if result.TrafficError == nil || result.OnlineError != nil || len(result.Online) != 1 {
		t.Fatalf("snapshot %+v", result)
	}
}
