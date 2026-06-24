package client

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"errors"
	clientselfservice "laz/internal/server/clientselfservice"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	"laz/internal/server/ratelimit"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	subscriptionssvc "laz/internal/server/subscriptions"
	clientview "laz/internal/server/transport/http/client/view"
	"laz/internal/server/transport/http/httpx"
	webui "laz/internal/server/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
)

type SubscriptionTokenFunc func(connectionID string, expiresAt time.Time) (string, model.ConnectionSubscriptionToken, error)
type AbsoluteURLFunc func(r *http.Request, path string) string

type App struct {
	store                        store.Store
	clientSelfService            *clientselfservice.Service
	events                       *eventsvc.Bus
	subscriptions                *subscriptionssvc.Service
	getOrCreateSubscriptionToken SubscriptionTokenFunc
	absoluteURL                  AbsoluteURLFunc
	connectAssets                http.Handler
	secret                       string
	rateLimits                   ratelimit.Config
}

type Config struct {
	Store                        store.Store
	ClientSelfService            *clientselfservice.Service
	Events                       *eventsvc.Bus
	Subscriptions                *subscriptionssvc.Service
	GetOrCreateSubscriptionToken SubscriptionTokenFunc
	AbsoluteURL                  AbsoluteURLFunc
	ConnectAssets                http.Handler
	Secret                       string
	RateLimits                   ratelimit.Config
}

func New(config Config) *App {
	return &App{
		store:                        config.Store,
		clientSelfService:            config.ClientSelfService,
		events:                       config.Events,
		subscriptions:                config.Subscriptions,
		getOrCreateSubscriptionToken: config.GetOrCreateSubscriptionToken,
		absoluteURL:                  config.AbsoluteURL,
		connectAssets:                config.ConnectAssets,
		secret:                       config.Secret,
		rateLimits:                   config.RateLimits.Normalize(),
	}
}

func (a *App) Mount(router chi.Router) {
	router.Handle("/connect-assets/*", a.connectAssets)

	router.Route("/client/v1", func(r chi.Router) {
		r.With(a.limitByIP(a.rateLimits.VerifyPerMinute)).HandleFunc("/configs", a.configs)
		r.With(a.limitByIP(a.rateLimits.PasswordSetupPerMinute)).HandleFunc("/password/setup", a.setupPassword)
		r.With(a.limitByIP(a.rateLimits.UserLoginPerMinute)).HandleFunc("/login", a.login)
		r.With(a.limitByIP(a.rateLimits.UserLoginPerMinute)).HandleFunc("/auth/refresh", a.refresh)
		r.With(a.limitByIP(a.rateLimits.RecoveryPerMinute)).HandleFunc("/recover", a.recover)
		r.HandleFunc("/logout", a.logout)
		r.HandleFunc("/session/configs", a.sessionConfigs)
		r.HandleFunc("/session/connections", a.sessionConnections)
		r.HandleFunc("/session/recovery-code", a.sessionRecoveryCode)
		r.With(a.limitByIP(a.rateLimits.HPLinkPerMinute)).HandleFunc("/session/hp-link", a.sessionHPLink)
		r.With(a.limitByIP(a.rateLimits.QRPerMinute)).HandleFunc("/session/qr", a.sessionQR)
		r.HandleFunc("/events", a.eventsStream)
		r.With(a.limitByIP(a.rateLimits.HPLinkPerMinute)).HandleFunc("/hp-link", a.hpLink)
		r.With(a.limitByIP(a.rateLimits.QRPerMinute)).HandleFunc("/qr", a.qr)
	})
	router.With(a.limitByIP(a.rateLimits.ConnectPerMinute)).HandleFunc("/client/*", a.connectPage)
	router.HandleFunc("/c/*", a.shortSubscription)
	router.HandleFunc("/hp/*", a.hpSubscription)
	router.HandleFunc("/sub/*", a.subscription)
}

func (a *App) limitByIP(limit int) func(http.Handler) http.Handler {
	if !a.rateLimits.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return httprate.LimitByIP(limit, time.Minute)
}

func (a *App) connectPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rawToken := strings.Trim(strings.TrimPrefix(r.URL.Path, "/client/"), "/")
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
		Token          string        `json:"token"`
		Challenge      string        `json:"challenge"`
		Answer         string        `json:"answer"`
		PoWNonce       string        `json:"pow_nonce"`
		ChallengeState liteChallenge `json:"challenge_state"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	token, ok := a.authenticatePortalToken(w, input.Token)
	if !ok {
		return
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if input.ChallengeState.Signature == "" && input.Challenge == "" {
		challenge, err := a.newLiteChallenge(input.Token, account.Username)
		if err != nil {
			httpx.Error(w, http.StatusForbidden, "account username is too short")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"challenge": challenge, "pow_difficulty": liteChallengePoWBits})
		return
	}
	if input.Challenge != "" {
		if !challengeMatches(account.Username, input.Challenge) {
			httpx.Error(w, http.StatusForbidden, "invalid challenge")
			return
		}
	} else if !a.verifyLiteChallenge(input.Token, account.Username, input.Answer, input.PoWNonce, input.ChallengeState) {
		httpx.Error(w, http.StatusForbidden, "invalid challenge")
		return
	}
	csrfToken := a.issueLiteCookie(w, r, input.Token)
	summary, err := a.store.Summary(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	payload := clientview.Summary(summary)
	policy := a.clientSelfService.EffectivePolicy(token.AccountID)
	payload["policy"] = clientview.EffectivePolicyItem(policy)
	payload["nodes"] = a.allowedNodeViews(policy)
	payload["pin_enabled"] = a.pinEnabled(token.AccountID)
	payload["csrf_token"] = csrfToken
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) allowedNodeViews(policy model.EffectiveConnectionPolicy) []clientview.Node {
	allowed := make(map[string]struct{}, len(policy.AllowedNodeIDs))
	for _, id := range policy.AllowedNodeIDs {
		allowed[id] = struct{}{}
	}
	items := make([]clientview.Node, 0, len(allowed))
	for _, node := range a.store.ListNodes() {
		if _, ok := allowed[node.ID]; ok && node.Status == model.StatusActive {
			items = append(items, clientview.NodeItem(node))
		}
	}
	return items
}

func (a *App) hpLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token        string `json:"token"`
		Challenge    string `json:"challenge"`
		Profile      string `json:"profile"`
		ConnectionID string `json:"connection_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	token, ok := a.authenticatePortalToken(w, input.Token)
	if !ok {
		return
	}
	if tokenHash, ok := a.authenticateLite(w, r); !ok {
		return
	} else if tokenHash != token.TokenHash {
		httpx.Error(w, http.StatusForbidden, "lite cookie token mismatch")
		return
	}
	profile := strings.TrimSpace(input.Profile)
	if profile == "" {
		profile = "all"
	}
	connectionID := strings.TrimSpace(input.ConnectionID)
	if connectionID == "" {
		httpx.Error(w, http.StatusBadRequest, "connection_id is required")
		return
	}
	connection, err := a.store.GetConnection(connectionID)
	if err != nil || connection.AccountID != token.AccountID || connection.DesiredStatus == model.ConnectionStatusDeleted {
		httpx.Error(w, http.StatusForbidden, "connection is unavailable")
		return
	}
	_, linkToken, err := a.getOrCreateSubscriptionToken(connection.ID, time.Time{})
	if err != nil {
		httpx.StoreError(w, err)
		return
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
	httpx.JSON(w, http.StatusOK, map[string]any{"id": link.ID, "url": link.EncryptedURL})
}

func (a *App) qr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token string `json:"token"`
		qrReference
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	token, ok := a.authenticatePortalToken(w, input.Token)
	if !ok {
		return
	}
	if tokenHash, ok := a.authenticateLite(w, r); !ok {
		return
	} else if tokenHash != token.TokenHash {
		httpx.Error(w, http.StatusForbidden, "lite cookie token mismatch")
		return
	}
	a.writeReferencedQRCode(w, input.qrReference, token.AccountID)
}

func (a *App) authenticatePortalToken(w http.ResponseWriter, raw string) (model.AccountPortalToken, bool) {
	if raw == "" {
		httpx.Error(w, http.StatusUnauthorized, "token is required")
		return model.AccountPortalToken{}, false
	}
	hash := commontokens.Hash(raw)
	token, err := a.store.GetAccountPortalTokenByHash(hash)
	if err != nil || subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(hash)) != 1 {
		httpx.Error(w, http.StatusUnauthorized, "invalid token")
		return model.AccountPortalToken{}, false
	}
	if token.Status != model.StatusActive {
		httpx.Error(w, http.StatusForbidden, "token is not active")
		return model.AccountPortalToken{}, false
	}
	if !token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt) {
		httpx.Error(w, http.StatusForbidden, "token is expired")
		return model.AccountPortalToken{}, false
	}
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return model.AccountPortalToken{}, false
	}
	if account.Status != model.AccountStatusActive {
		httpx.Error(w, http.StatusForbidden, "account is not active")
		return model.AccountPortalToken{}, false
	}
	_ = a.store.TouchAccountPortalToken(token.ID)
	return token, true
}

func (a *App) pinEnabled(accountID string) bool {
	return a.hasStrongPasswordCredential(accountID)
}

func (a *App) hasStrongPasswordCredential(accountID string) bool {
	identity, err := a.store.FindActiveUserIdentityByAccountID(accountID)
	if err != nil {
		return false
	}
	credential, err := a.store.GetAuthCredential(identity.ID, model.AuthCredentialTypePassword)
	return err == nil && credential.Status == model.StatusActive && strings.TrimSpace(credential.SecretHash) != ""
}

func (a *App) subscriptionTokenByID(id string) (model.ConnectionSubscriptionToken, bool) {
	for _, token := range a.store.ListConnectionSubscriptionTokens() {
		if token.ID != id {
			continue
		}
		if token.Status != model.StatusActive {
			return model.ConnectionSubscriptionToken{}, false
		}
		if !token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt) {
			return model.ConnectionSubscriptionToken{}, false
		}
		connection, err := a.store.GetConnection(token.ConnectionID)
		if err != nil || connection.DesiredStatus == model.ConnectionStatusDeleted {
			return model.ConnectionSubscriptionToken{}, false
		}
		account, err := a.store.GetAccount(connection.AccountID)
		if err != nil || account.Status != model.AccountStatusActive {
			return model.ConnectionSubscriptionToken{}, false
		}
		_ = a.store.TouchConnectionSubscriptionToken(token.ID)
		return token, true
	}
	return model.ConnectionSubscriptionToken{}, false
}

func (a *App) authenticateSubscriptionToken(w http.ResponseWriter, raw string) (model.ConnectionSubscriptionToken, bool) {
	if raw == "" {
		httpx.Error(w, http.StatusUnauthorized, "token is required")
		return model.ConnectionSubscriptionToken{}, false
	}
	hash := commontokens.Hash(raw)
	token, err := a.store.GetConnectionSubscriptionTokenByHash(hash)
	if err != nil || subtle.ConstantTimeCompare([]byte(token.TokenHash), []byte(hash)) != 1 {
		httpx.Error(w, http.StatusUnauthorized, "invalid token")
		return model.ConnectionSubscriptionToken{}, false
	}
	if token.Status != model.StatusActive || (!token.ExpiresAt.IsZero() && time.Now().UTC().After(token.ExpiresAt)) {
		httpx.Error(w, http.StatusForbidden, "token is not active")
		return model.ConnectionSubscriptionToken{}, false
	}
	connection, err := a.store.GetConnection(token.ConnectionID)
	if err != nil || connection.DesiredStatus == model.ConnectionStatusDeleted {
		httpx.Error(w, http.StatusForbidden, "connection is unavailable")
		return model.ConnectionSubscriptionToken{}, false
	}
	account, err := a.store.GetAccount(connection.AccountID)
	if err != nil || account.Status != model.AccountStatusActive {
		httpx.Error(w, http.StatusForbidden, "account is not active")
		return model.ConnectionSubscriptionToken{}, false
	}
	_ = a.store.TouchConnectionSubscriptionToken(token.ID)
	return token, true
}
