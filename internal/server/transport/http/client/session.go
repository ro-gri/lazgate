package client

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	clientselfservice "laz/internal/server/clientselfservice"
	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	clientview "laz/internal/server/transport/http/client/view"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) setupPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
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
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if a.hasStrongPasswordCredential(account.ID) {
		httpx.Error(w, http.StatusConflict, "password is already set; use login or recovery")
		return
	}
	rawSession, _, session, err := a.setupStrongUser(account, input.Password)
	if err != nil {
		writeClientSessionError(w, err)
		return
	}
	recoveryCode, recoveryHash, err := newStrongRecoveryCode()
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	if _, err := a.store.CreateAuthRecoveryCode(model.AuthRecoveryCode{IdentityID: session.IdentityID, CodeHash: recoveryHash, Status: model.StatusActive}); err != nil {
		httpx.StoreError(w, err)
		return
	}
	csrf := a.issueStrongCookie(w, r, rawSession)
	payload := strongAuthTimings(session)
	payload["account"] = clientview.AccountItem(account)
	payload["recovery_code"] = recoveryCode
	payload["recovery_method"] = "local_code"
	payload["csrf_token"] = csrf
	httpx.JSON(w, http.StatusCreated, payload)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Token) == "" {
		httpx.Error(w, http.StatusBadRequest, "token is required")
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
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	rawSession, _, session, err := a.loginStrongUser(account, input.Password)
	if err != nil {
		writeClientSessionError(w, err)
		return
	}
	csrf := a.issueStrongCookie(w, r, rawSession)
	sessionToken, _ := splitStrongRawTokens(rawSession)
	payload := strongAuthTimings(session)
	payload["session_token"] = sessionToken
	payload["session"] = clientview.SessionItem(model.SessionView{ID: session.ID, AccountID: account.ID, Status: session.Status, ExpiresAt: session.ExpiresAt, CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt})
	payload["account"] = clientview.AccountItem(account)
	payload["csrf_token"] = csrf
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) recover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token        string `json:"token"`
		RecoveryCode string `json:"recovery_code"`
		NewPassword  string `json:"new_password"`
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
	account, err := a.store.GetAccount(token.AccountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	identity, err := a.store.FindActiveUserIdentityByAccountID(account.ID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	matched := false
	for _, code := range a.store.ListAuthRecoveryCodes(identity.ID) {
		if code.Status == model.StatusActive && code.CodeHash == recoveryCodeHash(input.RecoveryCode) {
			matched = true
			_, _ = a.store.MarkAuthRecoveryCodeUsed(code.ID)
			break
		}
	}
	if !matched {
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	_ = a.store.RevokeAuthSessionsByIdentityID(identity.ID)
	rawSession, _, session, err := a.setupStrongUser(account, input.NewPassword)
	if err != nil {
		writeClientSessionError(w, err)
		return
	}
	recoveryCode, recoveryHash, err := newStrongRecoveryCode()
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	if _, err := a.store.CreateAuthRecoveryCode(model.AuthRecoveryCode{IdentityID: session.IdentityID, CodeHash: recoveryHash, Status: model.StatusActive}); err != nil {
		httpx.StoreError(w, err)
		return
	}
	csrf := a.issueStrongCookie(w, r, rawSession)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"recovery_code":    recoveryCode,
		"recovery_method":  "local_code",
		"sessions_revoked": true,
		"csrf_token":       csrf,
	})
}

func sameHostHeader(value string, requestHost string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	return hostWithoutPort(parsed.Host) == hostWithoutPort(requestHost)
}

func hostWithoutPort(value string) string {
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(value)
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
		httpx.Error(w, http.StatusForbidden, "cross-site client request rejected")
		return
	}
	cookie, err := r.Cookie(strongRefreshCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		httpx.Error(w, http.StatusUnauthorized, "refresh token required")
		return
	}
	refreshHash := a.validateStrongRefreshCookie(cookie.Value)
	if refreshHash == "" {
		httpx.Error(w, http.StatusUnauthorized, "refresh token required")
		return
	}
	now := time.Now().UTC()
	accessCookie, err := r.Cookie(strongCookieName)
	if err != nil || !a.validateStrongCSRF(accessCookie.Value, r.Header.Get("X-CSRF-Token")) {
		httpx.Error(w, http.StatusForbidden, "strong session and csrf required")
		return
	}
	session, err := a.store.GetAuthSessionByRefreshHashForRotation(refreshHash, 10*time.Second)
	if err != nil || session.RefreshTokenHash == "" || now.After(session.RefreshExpiresAt) || now.After(session.RefreshAbsoluteUntil) {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rawSession, err := commontokens.New()
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	rawRefresh, err := commontokens.New()
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	rotation, err := a.store.RotateAuthRefreshToken(refreshHash, commontokens.Hash(rawSession), now.Add(strongSessionTTL), commontokens.Hash(rawRefresh), now.Add(strongRefreshTTL), 10*time.Second)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	session = rotation.Session
	if session.RefreshTokenHash == "" || now.After(session.RefreshExpiresAt) || now.After(session.RefreshAbsoluteUntil) {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx, ok := a.strongContextFromSessionIgnoringAccessExpiry(session)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if rotation.Grace {
		rawRefresh = ""
	}
	csrf := a.issueStrongCookie(w, r, rawSession+":"+rawRefresh)
	payload := strongAuthTimings(session)
	payload["account"] = clientview.AccountItem(ctx.Account)
	payload["csrf_token"] = csrf
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.clearLiteCookie(w)
	a.clearStrongCookie(w)
	if ctx, ok := a.authenticateStrong(httptestResponseWriter{}, r, false); ok && ctx.Identity.ID != "" {
		_ = a.store.RevokeAuthSessionsByIdentityID(ctx.Identity.ID)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (a *App) sessionConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, ok := a.authenticateStrong(w, r, false)
	if !ok {
		return
	}
	summary, err := a.store.Summary(ctx.Account.ID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	payload := clientview.Summary(summary)
	policy := a.clientSelfService.EffectivePolicy(ctx.Account.ID)
	payload["policy"] = clientview.EffectivePolicyItem(policy)
	payload["nodes"] = a.allowedNodeViews(policy)
	payload["pin_enabled"] = true
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) sessionConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.createSessionConnections(w, r)
	case http.MethodDelete:
		a.deleteSessionConnection(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) createSessionConnections(w http.ResponseWriter, r *http.Request) {
	ctx, ok := a.authenticateStrong(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ConnectionName string   `json:"connection_name"`
		NodeIDs        []string `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.clientSelfService.CreateConnections(r.Context(), clientselfservice.CreateConnectionInput{AccountID: ctx.Account.ID, ConnectionName: input.ConnectionName, NodeIDs: input.NodeIDs})
	if err != nil {
		writeClientSessionError(w, err)
		return
	}
	successes := 0
	errorIDs := []string{}
	for _, item := range result.Results {
		if item.Err != nil {
			errorIDs = append(errorIDs, httpx.LogError(http.StatusBadGateway, item.Err))
			continue
		}
		successes++
	}
	status := http.StatusCreated
	if result.Partial {
		status = http.StatusMultiStatus
	}
	httpx.JSON(w, status, map[string]any{"connections": connectionResultViews(result.Results), "partial": result.Partial, "result_count": len(result.Results), "success_count": successes, "error_ids": errorIDs})
}

func (a *App) deleteSessionConnection(w http.ResponseWriter, r *http.Request) {
	ctx, ok := a.authenticateStrong(w, r, true)
	if !ok {
		return
	}
	var input struct {
		ConnectionID string `json:"connection_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.clientSelfService.DeleteConnection(r.Context(), ctx.Account.ID, input.ConnectionID)
	if err != nil {
		writeClientSessionError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"connection": clientview.ConnectionItem(result.Connection)})
}

func (a *App) sessionRecoveryCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, ok := a.authenticateStrong(w, r, true)
	if !ok {
		return
	}
	code, hash, err := newStrongRecoveryCode()
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	if _, err := a.store.CreateAuthRecoveryCode(model.AuthRecoveryCode{IdentityID: ctx.Identity.ID, CodeHash: hash, Status: model.StatusActive}); err != nil {
		httpx.StoreError(w, err)
		return
	}
	_ = a.store.RevokeAuthSessionsByIdentityID(ctx.Identity.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"recovery_code": code, "recovery_method": "local_code", "sessions_revoked": true})
}

func (a *App) sessionHPLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, ok := a.authenticateStrong(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Profile      string `json:"profile"`
		ConnectionID string `json:"connection_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	connectionID := strings.TrimSpace(input.ConnectionID)
	if connectionID == "" {
		httpx.Error(w, http.StatusBadRequest, "connection_id is required")
		return
	}
	connection, err := a.store.GetConnection(connectionID)
	if err != nil || connection.AccountID != ctx.Account.ID || connection.DesiredStatus == model.ConnectionStatusDeleted {
		httpx.Error(w, http.StatusForbidden, "connection is unavailable")
		return
	}
	_, record, err := a.getOrCreateSubscriptionToken(connectionID, time.Time{})
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	profile := strings.TrimSpace(input.Profile)
	if profile == "" {
		profile = "all"
	}
	link, err := a.getOrCreateShortLink(r, record, profile)
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

func (a *App) sessionQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, ok := a.authenticateStrong(w, r, true)
	if !ok {
		return
	}
	var input qrReference
	if !httpx.Decode(w, r, &input) {
		return
	}
	a.writeReferencedQRCode(w, input, ctx.Account.ID)
}

type httptestResponseWriter struct{}

func (httptestResponseWriter) Header() http.Header       { return http.Header{} }
func (httptestResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (httptestResponseWriter) WriteHeader(int)           {}

func writeClientSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
	case errors.Is(err, clientselfservice.ErrForbidden), errors.Is(err, clientselfservice.ErrConnectionLimitReached):
		httpx.Error(w, http.StatusForbidden, "forbidden")
	default:
		httpx.PrivateError(w, http.StatusBadRequest, "request failed", err)
	}
}

func connectionResultViews(items []clientselfservice.ConnectionResult) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"node": clientview.NodeItem(item.Node), "connection": clientview.ConnectionItem(item.Connection), "status": item.Status, "config_count": item.ConfigCount})
	}
	return out
}
