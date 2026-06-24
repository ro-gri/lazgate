package server

import (
	store "laz/internal/server/storage"
	"laz/internal/queue"
	"testing"
)

func TestNewServerBuildsNativeAgentHubsAndConnectionService(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	if srv.hysteriaAgent == nil {
		t.Fatal("server Hysteria agent control was not constructed")
	}
	if srv.amneziaAgent == nil {
		t.Fatal("server Amnezia agent hub was not constructed")
	}
	if srv.connections == nil {
		t.Fatal("connection service was not constructed")
	}
}

func TestServerRejectsMissingEventInfrastructure(t *testing.T) {
	for _, deps := range []Dependencies{{}, {Transport: queue.NopStore{}}} {
		if server, err := New(Config{}, deps); err == nil || server != nil {
			t.Fatal("server accepted incomplete event infrastructure")
		}
	}
}

func TestNewServerAppliesExplicitConfiguration(t *testing.T) {
	cfg := Config{PublicBaseURL: "https://example.test/", WebPrefix: "/admin", AgentGRPCTarget: "node:443", AgentDownloadBase: "https://example.test/download/", SecretKey: "test-key"}
	srv := newTestServer(t, cfg, nil)
	if srv.publicBaseURL != "https://example.test" || srv.webPrefix != "/admin" || srv.agentGRPCTarget != "node:443" || srv.agentDownloadBase != "https://example.test/download" || srv.secretKey != "test-key" {
		t.Fatal("server configuration was not applied")
	}
	if srv.transport == nil || srv.events == nil || srv.worker == nil {
		t.Fatal("server dependencies not assembled")
	}
}
