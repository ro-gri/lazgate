package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/server/model"
)

func TestSecureStoreEncryptsSecretsAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	inner, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapSecrets(inner, "test-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	account, err := wrapped.CreateAccount(model.Account{Username: "qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := wrapped.CreateClient(model.Client{AccountID: account.ID, Slug: "mac", Name: "Mac"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := wrapped.CreateNode(model.Node{
		Name:       "hy",
		Type:       model.NodeTypeBlitzHysteria,
		APIKey:     "node-api-secret",
		SSHKeyPath: "/secret/key/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := wrapped.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "hy", Config: "hy2://secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: "raw-token", TokenHash: "hash", Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, secret := range []string{"node-api-secret", "/secret/key/path", "hy2://secret", "raw-token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("secret %q leaked into store file: %s", secret, body)
		}
	}
	if !strings.Contains(body, encryptedPrefix) {
		t.Fatalf("expected encrypted payload in store file: %s", body)
	}

	gotNode, err := wrapped.GetNode(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotNode.APIKey != "node-api-secret" || gotNode.SSHKeyPath != "/secret/key/path" {
		t.Fatalf("unexpected decrypted node: %+v", gotNode)
	}
	configs := wrapped.ListIssuedConfigs()
	if len(configs) != 1 || configs[0].Config != "hy2://secret" {
		t.Fatalf("unexpected decrypted configs: %+v", configs)
	}
	tokens := wrapped.ListAccessTokens()
	if len(tokens) != 1 || tokens[0].Token != "raw-token" {
		t.Fatalf("unexpected decrypted tokens: %+v", tokens)
	}
}

func TestSecureStoreStoresSessionsHashOnly(t *testing.T) {
	inner, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapSecrets(inner, "test-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.CreateAdminSession(model.AdminSession{
		Token:         "admin-raw",
		TokenHash:     "admin-hash",
		CSRFToken:     "csrf-raw",
		CSRFTokenHash: "csrf-hash",
		PrincipalName: "admin",
		Role:          "owner",
	}); err != nil {
		t.Fatal(err)
	}
	account, err := wrapped.CreateAccount(model.Account{Username: "qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.CreateClientSession(model.ClientSession{AccountID: account.ID, Token: "client-raw", TokenHash: "client-hash"}); err != nil {
		t.Fatal(err)
	}
	adminSession, err := wrapped.GetAdminSessionByHash("admin-hash")
	if err != nil {
		t.Fatal(err)
	}
	if adminSession.Token != "" || adminSession.CSRFToken != "" {
		t.Fatalf("admin session raw secrets were stored: %+v", adminSession)
	}
	clientSession, err := wrapped.GetClientSessionByHash("client-hash")
	if err != nil {
		t.Fatal(err)
	}
	if clientSession.Token != "" {
		t.Fatalf("client session raw token was stored: %+v", clientSession)
	}
}
