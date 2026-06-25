package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"laz/internal/server/model"
	"laz/internal/server/storage"
)

func TestTokensPostReusesUserClientToken(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "https://net.example", "/admin")

	first := postToken(t, srv, account.ID)
	second := postToken(t, srv, account.ID)

	if first.Token == "" {
		t.Fatal("expected first token")
	}
	if first.Token != second.Token {
		t.Fatalf("expected stable token, got %q and %q", first.Token, second.Token)
	}
	if first.ConfigPage == "" || first.ConfigPage != second.ConfigPage {
		t.Fatalf("expected stable config page URL, got %q and %q", first.ConfigPage, second.ConfigPage)
	}
	if first.Subscription != "" || second.Subscription != "" {
		t.Fatalf("did not expect account-level subscription URLs, got %q and %q", first.Subscription, second.Subscription)
	}
	if got := len(st.ListAccessTokens()); got != 1 {
		t.Fatalf("expected one stored token, got %d", got)
	}
}

func TestTokensPostSeparatesUserAndDeviceTokens(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty", DisplayName: "Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "mac", Name: "Mac"})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "https://net.example", "/admin")

	accountToken := postToken(t, srv, account.ID)
	clientToken := postDeviceToken(t, srv, account.ID, client.ID)

	if accountToken.Token == clientToken.Token {
		t.Fatalf("expected account and client tokens to differ, both were %q", accountToken.Token)
	}
	if accountToken.Subscription != "" {
		t.Fatalf("did not expect account-level subscription URL, got %q", accountToken.Subscription)
	}
	if clientToken.Subscription == "" {
		t.Fatal("expected client-level subscription URL")
	}
	records := st.ListAccessTokens()
	if got := len(records); got != 2 {
		t.Fatalf("expected two stored tokens, got %d", got)
	}
}

func postToken(t *testing.T, srv *Server, accountID string) tokenResponse {
	return postDeviceToken(t, srv, accountID, "")
}

func postDeviceToken(t *testing.T, srv *Server, accountID, clientID string) tokenResponse {
	t.Helper()
	body := bytes.NewBufferString(`{"account_id":"` + accountID + `","client_id":"` + clientID + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tokens", body)
	req.Header.Set("Authorization", "Bearer secret")
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
