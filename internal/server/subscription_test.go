package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	commontokens "laz/internal/common/tokens"
	"laz/internal/model"
	"laz/internal/services/connections"
	subscriptionssvc "laz/internal/services/subscriptions"
	"laz/internal/storage"
	clientapi "laz/internal/transport/http/client"
)

func TestRoutesDisallowRobots(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Body.String(); got != "Account-agent: *\nDisallow: /\n" {
		t.Fatalf("unexpected robots.txt body %q", got)
	}
	if got := rr.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("unexpected X-Robots-Tag %q", got)
	}
}

func TestRoutesServeBlankRootPage(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("unexpected X-Robots-Tag %q", got)
	}
	if !strings.Contains(rr.Body.String(), "This service is intentionally not public.") {
		t.Fatalf("unexpected root body %q", rr.Body.String())
	}
}

func TestRoutesServeCustomBlankRootPage(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")
	srv.SetBlankPageHTML("<!doctype html><title>Custom</title><main>Custom public page</main>")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Custom public page") {
		t.Fatalf("unexpected custom root body %q", rr.Body.String())
	}
}

func TestRoutesSetNoIndexOnNotFound(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	if got := rr.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("unexpected X-Robots-Tag %q", got)
	}
}

func TestFormatSubscriptionLineAddsCountryLabel(t *testing.T) {
	cfg := model.IssuedConfig{
		Name:   "FirstByte Hysteria2",
		Config: "hy2://secret@example.com:443?sni=hy2.example.com#old-name",
	}
	node := model.Node{Region: "FI-Helsinki"}

	got := subscriptionssvc.FormatSubscriptionLine(cfg, node)

	if !strings.Contains(got, "#%F0%9F%87%AB%F0%9F%87%AE%20Finland%20%C2%B7%20FirstByte") {
		t.Fatalf("expected country label in URI fragment, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "hysteria") {
		t.Fatalf("expected subscription label without duplicate protocol name, got %q", got)
	}
}

func TestSubscriptionBodyIsBase64Compatible(t *testing.T) {
	lines := []string{
		subscriptionssvc.FormatSubscriptionLine(
			model.IssuedConfig{Name: "Amsterdam Hysteria2", Config: "hy2://secret@example.com:443"},
			model.Node{Region: "NL"},
		),
	}
	body := strings.Join(lines, "\n") + "\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(body))

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "%F0%9F%87%B3%F0%9F%87%B1%20Netherlands") {
		t.Fatalf("expected decoded subscription to contain Netherlands label, got %q", decoded)
	}
}

func TestSubscriptionEndpointIsClosedBase64Hy2Only(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "FirstByte Hysteria2", Type: model.NodeTypeBlitzHysteria, Region: "FI-Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://secret@example.com:443#old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigAmneziaVPN, Name: "Amnezia", Config: "amnezia://secret"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-tg", "Telegram", "laz Telegram", "Unlocked: Telegram.", "Telegram", []string{"domain:telegram.org"}, []string{"149.154.160.0/20"})
	srv := NewServer(st, "secret", "", "/admin")
	req := httptest.NewRequest(http.MethodGet, "/sub/"+rawToken, nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "hy2://") || strings.Contains(rr.Body.String(), "amnezia://") {
		t.Fatalf("expected base64-only body, got %q", rr.Body.String())
	}
	decoded, err := base64.StdEncoding.DecodeString(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if !strings.Contains(body, "hy2://") {
		t.Fatalf("expected hy2 config, got %q", body)
	}
	if strings.Contains(body, "amnezia://") {
		t.Fatalf("did not expect amnezia config in subscription, got %q", body)
	}
	if !strings.Contains(body, "%F0%9F%87%AB%F0%9F%87%AE%20Finland%20%C2%B7%20FirstByte") {
		t.Fatalf("expected formatted country label, got %q", body)
	}
}

func TestSubscriptionEndpointsRejectUserLevelToken(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	rawToken := "account-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")

	for _, path := range []string{"/sub/" + rawToken, "/hp/" + rawToken} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestSubscriptionEndpointFiltersByDeviceToken(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "rogri"})
	if err != nil {
		t.Fatal(err)
	}
	iphone, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	mac, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "mac", Name: "Mac"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "Core Hysteria2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	iphoneAccess, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: iphone.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	macConnection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: mac.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: iphoneAccess.ID, Kind: model.ConfigHy2URI, Name: "iPhone", Config: "hy2://iphone@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: macConnection.ID, Kind: model.ConfigHy2URI, Name: "Mac", Config: "hy2://mac@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "mac-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: mac.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")
	req := httptest.NewRequest(http.MethodGet, "/sub/"+rawToken, nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	decoded, err := base64.StdEncoding.DecodeString(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	body := string(decoded)
	if !strings.Contains(body, "hy2://mac@example.com:443") {
		t.Fatalf("expected mac config, got %q", body)
	}
	if strings.Contains(body, "hy2://iphone@example.com:443") {
		t.Fatalf("did not expect iphone config, got %q", body)
	}
}

func TestAdminSummaryDoesNotExposeConfigValues(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "Core Hysteria2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "Core", Config: "hy2://secret@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+account.ID+"/summary", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var summary model.AccountSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Configs) != 1 {
		t.Fatalf("expected one config metadata item, got %d", len(summary.Configs))
	}
	if summary.Configs[0].Config != "" {
		t.Fatalf("admin summary exposed config value %q", summary.Configs[0].Config)
	}
}

func TestHPSubscriptionSendsRoutingHeaderOnly(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "FirstByte Hysteria2", Type: model.NodeTypeBlitzHysteria, Region: "FI-Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://secret@example.com:443#old"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-tg", "Telegram", "laz Telegram", "Unlocked: Telegram.", "Telegram", []string{"domain:telegram.org"}, []string{"149.154.160.0/20"})
	srv := NewServer(st, "secret", "", "/admin")
	req := httptest.NewRequest(http.MethodGet, "/hp/"+rawToken+"?profile=test-tg", nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if routing := rr.Header().Get("routing"); !strings.HasPrefix(routing, "happ://routing/onadd/") {
		t.Fatalf("expected routing header, got %q", routing)
	}
	if title := rr.Header().Get("profile-title"); title != "laz Telegram" {
		t.Fatalf("expected Telegram profile title, got %q", title)
	}
	if announce := decodeAnnounce(t, rr.Header().Get("announce")); announce != "Unlocked: Telegram." {
		t.Fatalf("expected Telegram announce, got %q", announce)
	}
	if strings.Contains(rr.Body.String(), "happ://routing/") {
		t.Fatalf("did not expect routing link in body, got %q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "hy2://") {
		t.Fatalf("expected hy2 server line in body, got %q", rr.Body.String())
	}
}

func TestHPSubscriptionAllProfileHasNoRoutingHeader(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "FirstByte Hysteria2", Type: model.NodeTypeBlitzHysteria, Region: "FI-Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://secret@example.com:443#old"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-all", "All", "laz All", "Unlocked: all traffic through VPN.", "", nil, nil)
	srv := NewServer(st, "secret", "", "/admin")
	req := httptest.NewRequest(http.MethodGet, "/hp/"+rawToken+"?profile=test-all", nil)
	rr := httptest.NewRecorder()

	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if routing := rr.Header().Get("routing"); routing != "" {
		t.Fatalf("did not expect routing header, got %q", routing)
	}
	if title := rr.Header().Get("profile-title"); title != "laz All" {
		t.Fatalf("expected All profile title, got %q", title)
	}
	if announce := decodeAnnounce(t, rr.Header().Get("announce")); announce != "Unlocked: all traffic through VPN." {
		t.Fatalf("expected All announce, got %q", announce)
	}
	if !strings.Contains(rr.Body.String(), "hy2://") {
		t.Fatalf("expected hy2 server line in body, got %q", rr.Body.String())
	}
}

func TestHPRoutingSocialAIProfileIncludesExpectedRules(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-social-ai", "Social AI", "laz Social AI", "Unlocked: Telegram, Facebook, Instagram, WhatsApp, Pinterest, YouTube, ChatGPT, Codex, LinkedIn.", "Social AI", []string{
		"domain:telegram.org",
		"domain:facebook.com",
		"domain:instagram.com",
		"domain:whatsapp.com",
		"domain:pinterest.com",
		"domain:pinterest.ru",
		"domain:youtube.com",
		"domain:youtube.ru",
		"domain:googlevideo.com",
		"domain:chatgpt.com",
		"domain:openai.com",
		"domain:linkedin.com",
	}, []string{
		"149.154.160.0/20",
		"157.240.0.0/17",
	})
	meta := subscriptionssvc.New(st).ProfileMeta("test-social-ai")
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
	for _, want := range []string{
		"domain:telegram.org",
		"domain:facebook.com",
		"domain:instagram.com",
		"domain:whatsapp.com",
		"domain:pinterest.com",
		"domain:pinterest.ru",
		"domain:youtube.com",
		"domain:youtube.ru",
		"domain:googlevideo.com",
		"domain:chatgpt.com",
		"domain:openai.com",
		"domain:linkedin.com",
	} {
		if !containsString(decoded.ProxySites, want) {
			t.Fatalf("expected proxy site %q in %#v", want, decoded.ProxySites)
		}
	}
	for _, want := range []string{
		"149.154.160.0/20",
		"157.240.0.0/17",
	} {
		if !containsString(decoded.ProxyIp, want) {
			t.Fatalf("expected proxy ip %q in %#v", want, decoded.ProxyIp)
		}
	}
}

func TestRemoteNameForUsesAccountIDSuffix(t *testing.T) {
	got := connections.RemoteNameFor("rogri", "mac", "usr_1234567890abcdef")
	if got != "rogri_mac_12345678" {
		t.Fatalf("expected readable unique remote name, got %q", got)
	}
}

func TestClientHPLinkUsesStoredEncryptedShortLink(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "FirstByte Hysteria2", Type: model.NodeTypeBlitzHysteria, Region: "FI-Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://secret@example.com:443#old"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	var clientToken model.AccessToken
	clientRawToken := "client-token"
	clientToken, err = st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: clientRawToken, TokenHash: commontokens.Hash(clientRawToken), Purpose: model.TokenPurposeClient})
	if err != nil {
		t.Fatal(err)
	}
	createHPProfile(t, st, "test-tg", "Telegram", "laz Telegram", "Unlocked: Telegram.", "Telegram", []string{"domain:telegram.org"}, []string{"149.154.160.0/20"})

	oldEncryptor := clientapi.HPLinkEncryptor
	encryptedInputs := []string{}
	clientapi.HPLinkEncryptor = func(_ context.Context, rawURL string) (string, error) {
		encryptedInputs = append(encryptedInputs, rawURL)
		return "happ://crypt5/" + rawURL, nil
	}
	defer func() { clientapi.HPLinkEncryptor = oldEncryptor }()

	srv := NewServer(st, "secret", "https://example.com", "/admin")
	first := postHPLink(t, srv, rawToken, "qwty", "test-tg", client.ID)
	second := postHPLink(t, srv, rawToken, "qwty", "test-tg", client.ID)

	if first.URL == "" || first.URL != second.URL {
		t.Fatalf("expected stable encrypted link, got %q and %q", first.URL, second.URL)
	}
	if len(encryptedInputs) != 1 {
		t.Fatalf("expected one encryption call, got %d", len(encryptedInputs))
	}
	if !strings.HasPrefix(encryptedInputs[0], "https://example.com/c/") {
		t.Fatalf("expected short /c/ target, got %q", encryptedInputs[0])
	}
	if strings.Contains(encryptedInputs[0], "/hp/") || strings.Contains(encryptedInputs[0], "stable-token") {
		t.Fatalf("short target leaked long token URL: %q", encryptedInputs[0])
	}
	link, err := st.GetShortLinkByTokenProfile(clientToken.ID, "test-tg")
	if err != nil {
		t.Fatal(err)
	}
	if link.EncryptedURL != first.URL || link.TargetURL != encryptedInputs[0] {
		t.Fatalf("short link was not stored correctly: %#v", link)
	}

	req := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(link.TargetURL, "https://example.com"), nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if routing := rr.Header().Get("routing"); !strings.HasPrefix(routing, "happ://routing/onadd/") {
		t.Fatalf("expected routing header, got %q", routing)
	}
	if !strings.Contains(rr.Body.String(), "hy2://") {
		t.Fatalf("expected hy2 body, got %q", rr.Body.String())
	}
}

func TestClientJSONEndpointRequiresChallenge(t *testing.T) {
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
	node, err := st.CreateNode(model.Node{Name: "FirstByte Hysteria2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := st.CreateConnection(model.Connection{AccountID: account.ID, ClientID: client.ID, NodeID: node.ID, Protocol: model.ProtocolHysteria2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateIssuedConfig(model.IssuedConfig{ConnectionID: connection.ID, Kind: model.ConfigHy2URI, Name: "FirstByte Hysteria2", Config: "hy2://secret@example.com:443"}); err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	clientToken := "client-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, ClientID: client.ID, Token: clientToken, TokenHash: commontokens.Hash(clientToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodPost, "/client/v1/configs", strings.NewReader(`{"token":"stable-token","challenge":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/client/v1/configs", strings.NewReader(`{"token":"stable-token","challenge":"qwty"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "hy2://secret@example.com:443") {
		t.Fatalf("did not expect raw hysteria config in response, got %q", rr.Body.String())
	}
	for _, forbidden := range []string{"config_template", "base_url", "ssh_host", "ssh_user", "remote_name", "remote_id", "last_error"} {
		if strings.Contains(rr.Body.String(), forbidden) {
			t.Fatalf("did not expect %q in client response: %s", forbidden, rr.Body.String())
		}
	}
	if !strings.Contains(rr.Body.String(), `"profiles"`) {
		t.Fatalf("expected subscription profiles in response, got %q", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/client/v1/configs", strings.NewReader(`{"token":"client-token","challenge":"qwty"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload struct {
		ClientID string       `json:"client_id"`
		Client   model.Client `json:"client"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ClientID != client.ID || payload.Client.Name != "Default" {
		t.Fatalf("expected client in client response, got %#v", payload)
	}
}

func TestClientRecoveryRequiresTokenChallenge(t *testing.T) {
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	account, err := st.CreateAccount(model.Account{Username: "qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	rawToken := "stable-token"
	if _, err := st.CreateAccessToken(model.AccessToken{AccountID: account.ID, Token: rawToken, TokenHash: commontokens.Hash(rawToken), Purpose: model.TokenPurposeClient}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodPost, "/client/v1/setup-pin", strings.NewReader(`{"token":"stable-token","challenge":"qwty","pin":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected setup pin 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var setup struct {
		RecoveryCode string `json:"recovery_code"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	if setup.RecoveryCode == "" {
		t.Fatal("expected recovery code")
	}

	body := `{"recovery_code":"` + setup.RecoveryCode + `","new_pin":"abcdef"}`
	req = httptest.NewRequest(http.MethodPost, "/client/v1/recover", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected recovery without token 401, got %d: %s", rr.Code, rr.Body.String())
	}

	body = `{"token":"stable-token","challenge":"bad","recovery_code":"` + setup.RecoveryCode + `","new_pin":"abcdef"}`
	req = httptest.NewRequest(http.MethodPost, "/client/v1/recover", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected recovery with bad challenge 403, got %d: %s", rr.Code, rr.Body.String())
	}

	body = `{"token":"stable-token","challenge":"qwty","recovery_code":"` + setup.RecoveryCode + `","new_pin":"abcdef"}`
	req = httptest.NewRequest(http.MethodPost, "/client/v1/recover", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected recovery with token and challenge 200, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/client/v1/login", strings.NewReader(`{"token":"stable-token","challenge":"qwty","pin":"abcdef"}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected token-bound login 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func postHPLink(t *testing.T, srv *Server, token, challenge, profile, clientID string) struct {
	URL string `json:"url"`
} {
	t.Helper()
	body := `{"token":"` + token + `","challenge":"` + challenge + `","profile":"` + profile + `","client_id":"` + clientID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/client/v1/hp-link", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
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

func decodeAnnounce(t *testing.T, value string) string {
	t.Helper()
	raw := strings.TrimPrefix(value, "base64:")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(decoded)
}

func TestSubscriptionLabelRemovesRepeatedProtocolAndCountry(t *testing.T) {
	got := subscriptionssvc.SubscriptionLabel("Netherlands Hysteria2", model.Node{Name: "Amsterdam Hysteria2", Region: "NL"})

	if got != "🇳🇱 Netherlands · Amsterdam" {
		t.Fatalf("unexpected label %q", got)
	}
}
