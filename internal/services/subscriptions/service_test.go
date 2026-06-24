package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/model"
	"laz/internal/storage"
)

func TestFormatSubscriptionLineAddsCountryLabel(t *testing.T) {
	cfg := model.IssuedConfig{
		Name:   "FirstByte Hysteria2",
		Config: "hy2://secret@example.com:443?sni=hy2.example.com#old-name",
	}
	node := model.Node{Region: "FI-Helsinki"}

	got := FormatSubscriptionLine(cfg, node)

	if !strings.Contains(got, "#%F0%9F%87%AB%F0%9F%87%AE%20Finland%20%C2%B7%20FirstByte") {
		t.Fatalf("expected country label in URI fragment, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "hysteria") {
		t.Fatalf("expected subscription label without duplicate protocol name, got %q", got)
	}
}

func TestSubscriptionLabelRemovesRepeatedProtocolAndCountry(t *testing.T) {
	got := SubscriptionLabel("Netherlands Hysteria2", model.Node{Name: "Amsterdam Hysteria2", Region: "NL"})

	if got != "🇳🇱 Netherlands · Amsterdam" {
		t.Fatalf("unexpected label %q", got)
	}
}

func TestClosedSubscriptionBodyIsBase64Hy2OnlyAndDeviceScoped(t *testing.T) {
	token := model.AccessToken{AccountID: "usr", ClientID: "dev_mac"}
	summary := model.AccountSummary{
		Connections: []model.ConnectionSummary{
			{
				Connection: model.Connection{ID: "acc_mac", ClientID: "dev_mac", Status: model.StatusActive},
				Node:       model.Node{Name: "FirstByte Hysteria2", Region: "FI-Helsinki"},
			},
			{
				Connection: model.Connection{ID: "acc_phone", ClientID: "dev_phone", Status: model.StatusActive},
				Node:       model.Node{Name: "Phone Hysteria2", Region: "NL"},
			},
		},
		Configs: []model.IssuedConfig{
			{ConnectionID: "acc_mac", Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://mac@example.com:443", Status: model.StatusActive},
			{ConnectionID: "acc_mac", Kind: model.ConfigAmneziaVPN, Name: "Amnezia", Config: "amnezia://secret", Status: model.StatusActive},
			{ConnectionID: "acc_phone", Kind: model.ConfigHy2URI, Name: "Phone Hysteria2", Config: "hy2://phone@example.com:443", Status: model.StatusActive},
		},
	}
	svc := New(nil)

	encoded := svc.ClosedSubscriptionBody(token, summary)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if !strings.Contains(body, "hy2://mac@example.com:443") {
		t.Fatalf("expected mac hy2 config, got %q", body)
	}
	for _, unexpected := range []string{"amnezia://secret", "hy2://phone@example.com:443"} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("did not expect %q in subscription body %q", unexpected, body)
		}
	}
}

func TestRoutingSocialAIProfileIncludesExpectedRules(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-social-ai", "Social AI", "laz Social AI", "Unlocked: Telegram, Facebook, Instagram, WhatsApp, Pinterest, YouTube, ChatGPT, Codex, LinkedIn.", "Social AI", []string{"domain:telegram.org", "domain:facebook.com", "domain:instagram.com", "domain:whatsapp.com", "domain:pinterest.ru", "domain:youtube.ru", "domain:openai.com", "domain:linkedin.com"}, []string{"149.154.160.0/20", "157.240.0.0/17"})
	meta := New(st).ProfileMeta("test-social-ai")
	if announce := meta.Announce; announce != "Unlocked: Telegram, Facebook, Instagram, WhatsApp, Pinterest, YouTube, ChatGPT, Codex, LinkedIn." {
		t.Fatalf("expected Social AI announce, got %q", announce)
	}
	routing := meta.Routing
	if !strings.HasPrefix(routing, "happ://routing/onadd/") {
		t.Fatalf("expected routing link, got %q", routing)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(routing, "happ://routing/onadd/"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Name       string   `json:"Name"`
		ProxySites []string `json:"ProxySites"`
		ProxyIp    []string `json:"ProxyIp"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "Social AI" {
		t.Fatalf("expected Social AI profile name, got %q", decoded.Name)
	}
	for _, want := range []string{"domain:telegram.org", "domain:facebook.com", "domain:instagram.com", "domain:whatsapp.com", "domain:pinterest.ru", "domain:youtube.ru", "domain:openai.com", "domain:linkedin.com"} {
		if !containsString(decoded.ProxySites, want) {
			t.Fatalf("expected proxy site %q in %#v", want, decoded.ProxySites)
		}
	}
	for _, want := range []string{"149.154.160.0/20", "157.240.0.0/17"} {
		if !containsString(decoded.ProxyIp, want) {
			t.Fatalf("expected proxy ip %q in %#v", want, decoded.ProxyIp)
		}
	}
}

func TestGetOrCreateHPShortLinkReusesStoredEncryptedURL(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "default", Name: "Default"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, TokenHash: "hash", Purpose: model.TokenPurposeClient})
	if err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-tg", "Telegram", "laz Telegram", "Unlocked: Telegram.", "Telegram", []string{"domain:telegram.org"}, []string{"149.154.160.0/20"})
	svc := New(st)
	calls := 0
	encrypt := func(_ context.Context, rawURL string) (string, error) {
		calls++
		return "happ://crypt5/" + rawURL, nil
	}
	nextID := func() (string, error) { return "short-id", nil }
	targetURL := func(id string) string { return "https://example.com/c/" + id }

	first, err := svc.GetOrCreateHPShortLink(context.Background(), token, "test-tg", targetURL, nextID, encrypt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.GetOrCreateHPShortLink(context.Background(), token, "test-tg", targetURL, nextID, encrypt)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one encryption call, got %d", calls)
	}
	if first.EncryptedURL == "" || first.EncryptedURL != second.EncryptedURL {
		t.Fatalf("expected stable encrypted URL, got %#v and %#v", first, second)
	}
}

func createHPProfile(t *testing.T, st store.Store, slug, name, title, announce, routingName string, sites []string, ips []string) {
	t.Helper()
	var routing any
	if routingName != "" {
		routing = map[string]any{"name": routingName, "proxy_sites": sites, "proxy_ip": ips}
	}
	raw, err := json.Marshal(map[string]any{
		"type":        "happ_subscription_profile",
		"title":       title,
		"description": name,
		"announce":    announce,
		"routing":     routing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateConfigProfile(model.ConfigProfile{
		Protocol:       model.ProtocolHysteria2,
		Kind:           model.ConfigKind("hp_subscription"),
		Slug:           slug,
		Name:           name,
		Client:         "happ",
		ContentType:    "application/json",
		ConfigTemplate: string(raw),
	}); err != nil {
		t.Fatal(err)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
