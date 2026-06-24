package client

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"errors"
	"laz/internal/common/httpx"
	commontokens "laz/internal/common/tokens"
	webui "laz/internal/common/web"
	"laz/internal/model"
	clientauthsvc "laz/internal/services/clientauth"
	subscriptionssvc "laz/internal/services/subscriptions"
	"laz/internal/storage"
	clientview "laz/internal/transport/http/client/view"

	"github.com/go-chi/chi/v5"
)

type ClientTokenFunc func(accountID, clientID string, expiresAt time.Time) (string, model.AccessToken, error)
type AbsoluteURLFunc func(r *http.Request, path string) string

type App struct {
	store            store.Store
	clientAuth       *clientauthsvc.Service
	subscriptions    *subscriptionssvc.Service
	getOrCreateToken ClientTokenFunc
	absoluteURL      AbsoluteURLFunc
	connectAssets    http.Handler
}

type Config struct {
	Store            store.Store
	ClientAuth       *clientauthsvc.Service
	Subscriptions    *subscriptionssvc.Service
	GetOrCreateToken ClientTokenFunc
	AbsoluteURL      AbsoluteURLFunc
	ConnectAssets    http.Handler
}

func New(config Config) *App {
	return &App{
		store:            config.Store,
		clientAuth:       config.ClientAuth,
		subscriptions:    config.Subscriptions,
		getOrCreateToken: config.GetOrCreateToken,
		absoluteURL:      config.AbsoluteURL,
		connectAssets:    config.ConnectAssets,
	}
}

func (a *App) Mount(router chi.Router) {
	router.HandleFunc("/connect/*", a.connectPage)
	router.Handle("/connect-assets/*", a.connectAssets)

	router.Route("/client/v1", func(r chi.Router) {
		r.HandleFunc("/configs", a.configs)
		r.HandleFunc("/setup-pin", a.setupPIN)
		r.HandleFunc("/login", a.login)
		r.HandleFunc("/recover", a.recover)
		r.HandleFunc("/logout", a.logout)
		r.HandleFunc("/session/configs", a.sessionConfigs)
		r.HandleFunc("/session/clients", a.sessionClients)
		r.HandleFunc("/session/recovery-code", a.sessionRecoveryCode)
		r.HandleFunc("/session/hp-link", a.sessionHPLink)
		r.HandleFunc("/session/qr", a.sessionQR)
		r.HandleFunc("/hp-link", a.hpLink)
		r.HandleFunc("/qr", a.qr)
	})
	router.HandleFunc("/c/*", a.shortSubscription)
	router.HandleFunc("/hp/*", a.hpSubscription)
	router.HandleFunc("/sub/*", a.subscription)
}

func (a *App) connectPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rawToken := strings.Trim(strings.TrimPrefix(r.URL.Path, "/connect/"), "/")
	if rawToken == "" || strings.Contains(rawToken, "/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, err := webui.AssetsFS.ReadFile("client/connect.html")
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	body := strings.ReplaceAll(string(raw), "__CONNECT_DATA__", httpx.HTMLJSON(map[string]any{
		"token": rawToken,
	}))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (a *App) configs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	token, ok := a.authenticateClientToken(w, r, input.Token)
	if !ok {
		return
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if !challengeMatches(account.Username, input.Challenge) {
		httpx.Error(w, http.StatusForbidden, "invalid challenge")
		return
	}
	summary, err := a.store.ClientSummary(token.AccountID, token.ClientID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	payload := clientview.Summary(token, summary)
	payload["pin_enabled"] = a.pinEnabled(token.AccountID)
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) hpLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
		Profile   string `json:"profile"`
		ClientID  string `json:"client_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	token, ok := a.authenticateClientToken(w, r, input.Token)
	if !ok {
		return
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if !challengeMatches(account.Username, input.Challenge) {
		httpx.Error(w, http.StatusForbidden, "invalid challenge")
		return
	}
	profile := strings.TrimSpace(input.Profile)
	if profile == "" {
		profile = "all"
	}
	linkToken := token
	clientID := strings.TrimSpace(input.ClientID)
	if token.ClientID != "" {
		if clientID != "" && clientID != token.ClientID {
			httpx.Error(w, http.StatusForbidden, "token is bound to another client")
			return
		}
		clientID = token.ClientID
	}
	if clientID == "" {
		httpx.Error(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if _, err := a.clientForAccount(token.AccountID, clientID); err != nil {
		httpx.StoreError(w, err)
		return
	}
	if token.ClientID == "" {
		_, record, err := a.getOrCreateToken(token.AccountID, clientID, time.Time{})
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		linkToken = record
	}
	link, err := a.getOrCreateShortLink(r, linkToken, profile)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.StoreError(w, err)
			return
		}
		httpx.PrivateError(w, http.StatusBadGateway, "subscription link is unavailable", err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": link.EncryptedURL})
}

func (a *App) qr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token string `json:"token"`
		Value string `json:"value"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if _, ok := a.authenticateClientToken(w, r, input.Token); !ok {
		return
	}
	httpx.QRCodePNG(w, input.Value)
}

func (a *App) authenticateClientToken(w http.ResponseWriter, _ *http.Request, raw string) (model.AccessToken, bool) {
	if raw == "" {
		httpx.Error(w, http.StatusUnauthorized, "token is required")
		return model.AccessToken{}, false
	}
	hash := commontokens.Hash(raw)
	token, err := a.store.GetAccessTokenByHash(hash)
	if err != nil || subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(hash)) != 1 {
		httpx.Error(w, http.StatusUnauthorized, "invalid token")
		return model.AccessToken{}, false
	}
	if token.Status != model.StatusActive || token.Purpose != model.TokenPurposeClient {
		httpx.Error(w, http.StatusForbidden, "token is not active")
		return model.AccessToken{}, false
	}
	if !token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt) {
		httpx.Error(w, http.StatusForbidden, "token is expired")
		return model.AccessToken{}, false
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return model.AccessToken{}, false
	}
	if account.Status != model.StatusActive {
		httpx.Error(w, http.StatusForbidden, "account is not active")
		return model.AccessToken{}, false
	}
	_ = a.store.TouchAccessToken(token.ID)
	return token, true
}

func (a *App) pinEnabled(accountID string) bool {
	credential, err := a.store.GetClientCredential(accountID)
	return err == nil && credential.PINHash != ""
}

func (a *App) clientTokenByID(id string) (model.AccessToken, bool) {
	for _, token := range a.store.ListAccessTokens() {
		if token.ID != id {
			continue
		}
		if token.Status != model.StatusActive || token.Purpose != model.TokenPurposeClient {
			return model.AccessToken{}, false
		}
		if !token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt) {
			return model.AccessToken{}, false
		}
		account, err := a.store.GetAccount(token.AccountID)
		if err != nil || account.Status != model.StatusActive {
			return model.AccessToken{}, false
		}
		_ = a.store.TouchAccessToken(token.ID)
		return token, true
	}
	return model.AccessToken{}, false
}

func (a *App) clientForAccount(accountID, clientID string) (model.Client, error) {
	return a.store.GetClientForAccount(accountID, clientID)
}
