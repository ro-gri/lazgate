package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"laz/internal/server/model"
)

func TestTokenScopesAreStableAndSeparated(t *testing.T) {
	st := newRouteTestStore(t)
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "Amsterdam", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection:        model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "secret", PasswordHash: "secret-hash"},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Phone", Config: "hy2://secret@example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection := bundle.Connection
	srv := newTestServer(t, Config{PublicBaseURL: "https://net.example", WebPrefix: "/admin"}, st)

	portal1 := postScopedToken(t, srv, map[string]any{"scope": "portal", "account_id": account.ID})
	portal2 := postScopedToken(t, srv, map[string]any{"scope": "portal", "account_id": account.ID})
	if portal1.Token == "" || portal1.Token != portal2.Token || portal1.ConfigPage == "" || portal1.Subscription != "" {
		t.Fatalf("unexpected portal token responses: first=%+v second=%+v", portal1, portal2)
	}
	if len(st.ListAccountPortalTokens()) != 1 || len(st.ListConnectionSubscriptionTokens()) != 0 {
		t.Fatalf("unexpected token counts after portal issue: portal=%d subscription=%d", len(st.ListAccountPortalTokens()), len(st.ListConnectionSubscriptionTokens()))
	}

	sub1 := postScopedToken(t, srv, map[string]any{"scope": "subscription", "connection_id": connection.ID})
	sub2 := postScopedToken(t, srv, map[string]any{"scope": "subscription", "connection_id": connection.ID})
	if sub1.Token == "" || sub1.Token != sub2.Token || sub1.Subscription == "" || sub1.ConfigPage != "" {
		t.Fatalf("unexpected subscription responses: first=%+v second=%+v", sub1, sub2)
	}
	if sub1.Token == portal1.Token {
		t.Fatal("portal and subscription scopes reused the same token")
	}
	if len(st.ListAccountPortalTokens()) != 1 || len(st.ListConnectionSubscriptionTokens()) != 1 {
		t.Fatalf("unexpected final token counts: portal=%d subscription=%d", len(st.ListAccountPortalTokens()), len(st.ListConnectionSubscriptionTokens()))
	}
}

func postScopedToken(t *testing.T, srv *Server, input map[string]any) tokenResponse {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/tokens", bytes.NewReader(raw))
	session, rawSession := createRouteTestAdminSession(t, srv, nil, "token-owner", model.AdminRoleOwner)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(session.ID))
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var out tokenResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type tokenResponse struct {
	Token        string `json:"token"`
	Subscription string `json:"subscription"`
	ConfigPage   string `json:"config_page"`
}
