package client

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"laz/internal/common/httpx"
	"laz/internal/model"
	clientauthsvc "laz/internal/services/clientauth"
	"laz/internal/storage"
	clientview "laz/internal/transport/http/client/view"
)

func (a *App) setupPIN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
		PIN       string `json:"pin"`
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
	result, err := a.clientAuth.SetupPIN(clientauthsvc.SetupPINInput{AccountID: account.ID, PIN: input.PIN})
	if err != nil {
		writeClientAuthError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"account":         clientview.AccountItem(account),
		"recovery_code":   result.RecoveryCode,
		"recovery_method": result.RecoveryMethod,
	})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token     string `json:"token"`
		Challenge string `json:"challenge"`
		PIN       string `json:"pin"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Token) == "" {
		httpx.Error(w, http.StatusBadRequest, "token is required")
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
	result, err := a.clientAuth.Login(clientauthsvc.LoginInput{AccountID: account.ID, PIN: input.PIN})
	if err != nil {
		writeClientAuthError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"session_token": result.SessionToken,
		"session":       clientview.SessionItem(result.Session),
		"account":       clientview.AccountItem(result.Account),
	})
}

func (a *App) recover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Token        string `json:"token"`
		Challenge    string `json:"challenge"`
		RecoveryCode string `json:"recovery_code"`
		NewPIN       string `json:"new_pin"`
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
	result, err := a.clientAuth.Recover(clientauthsvc.RecoveryInput{
		AccountID:    account.ID,
		Username:     account.Username,
		RecoveryCode: input.RecoveryCode,
		NewPIN:       input.NewPIN,
	})
	if err != nil {
		writeClientAuthError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"recovery_code":    result.RecoveryCode,
		"recovery_method":  result.RecoveryMethod,
		"sessions_revoked": result.SessionsRevoked,
	})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := a.clientAuth.Logout(sessionTokenFromRequest(r)); err != nil {
		writeClientAuthError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (a *App) sessionConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	session, _, ok := a.authenticateSession(w, r)
	if !ok {
		return
	}
	summary, err := a.store.ClientSummary(session.AccountID, "")
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	token := model.AccessToken{
		AccountID: session.AccountID,
		Purpose:   model.TokenPurposeClient,
		Status:    model.StatusActive,
	}
	payload := clientview.Summary(token, summary)
	payload["policy"] = clientview.EffectivePolicyItem(a.clientAuth.EffectivePolicy(session.AccountID))
	payload["pin_enabled"] = a.pinEnabled(session.AccountID)
	httpx.JSON(w, http.StatusOK, payload)
}

func (a *App) sessionClients(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.createSessionClient(w, r)
	case http.MethodDelete:
		a.deleteSessionClient(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) createSessionClient(w http.ResponseWriter, r *http.Request) {
	session, _, ok := a.authenticateSession(w, r)
	if !ok {
		return
	}
	var input struct {
		ClientSlug string   `json:"client_slug"`
		ClientName string   `json:"client_name"`
		NodeIDs    []string `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.clientAuth.CreateClient(r.Context(), clientauthsvc.CreateClientInput{
		AccountID:  session.AccountID,
		ClientSlug: input.ClientSlug,
		ClientName: input.ClientName,
		NodeIDs:    input.NodeIDs,
	})
	if err != nil {
		writeClientAuthError(w, err)
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
	httpx.JSON(w, status, map[string]any{
		"client":        clientview.ClientItem(result.Client),
		"partial":       result.Partial,
		"result_count":  len(result.Results),
		"success_count": successes,
		"error_ids":     errorIDs,
	})
}

func (a *App) deleteSessionClient(w http.ResponseWriter, r *http.Request) {
	session, _, ok := a.authenticateSession(w, r)
	if !ok {
		return
	}
	var input struct {
		ClientID string `json:"client_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.clientAuth.DeleteClient(r.Context(), clientauthsvc.DeleteClientInput{
		AccountID: session.AccountID,
		ClientID:  input.ClientID,
	})
	if err != nil {
		writeClientAuthError(w, err)
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
	status := http.StatusOK
	if result.Partial {
		status = http.StatusMultiStatus
	}
	httpx.JSON(w, status, map[string]any{
		"client":        clientview.ClientItem(result.Client),
		"partial":       result.Partial,
		"result_count":  len(result.Results),
		"success_count": successes,
		"error_ids":     errorIDs,
	})
}

func (a *App) sessionRecoveryCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	session, _, ok := a.authenticateSession(w, r)
	if !ok {
		return
	}
	result, err := a.clientAuth.RotateRecoveryCode(session.AccountID)
	if err != nil {
		writeClientAuthError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"recovery_code":    result.RecoveryCode,
		"recovery_method":  result.RecoveryMethod,
		"sessions_revoked": result.SessionsRevoked,
	})
}

func (a *App) sessionHPLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	session, _, ok := a.authenticateSession(w, r)
	if !ok {
		return
	}
	var input struct {
		Profile  string `json:"profile"`
		ClientID string `json:"client_id"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	clientID := strings.TrimSpace(input.ClientID)
	if clientID == "" {
		httpx.Error(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if _, err := a.clientForAccount(session.AccountID, clientID); err != nil {
		httpx.StoreError(w, err)
		return
	}
	_, record, err := a.getOrCreateToken(session.AccountID, clientID, time.Time{})
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
	httpx.JSON(w, http.StatusOK, map[string]any{"url": link.EncryptedURL})
}

func (a *App) sessionQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, _, ok := a.authenticateSession(w, r); !ok {
		return
	}
	var input struct {
		Value string `json:"value"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	httpx.QRCodePNG(w, input.Value)
}

func (a *App) authenticateSession(w http.ResponseWriter, r *http.Request) (model.ClientSession, model.Account, bool) {
	session, account, err := a.clientAuth.AuthenticateSession(sessionTokenFromRequest(r))
	if err != nil {
		writeClientAuthError(w, err)
		return model.ClientSession{}, model.Account{}, false
	}
	return session, account, true
}

func sessionTokenFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func writeClientAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
	case errors.Is(err, clientauthsvc.ErrInvalidCredentials):
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, clientauthsvc.ErrLocked):
		httpx.Error(w, http.StatusTooManyRequests, "temporarily locked")
	case errors.Is(err, clientauthsvc.ErrForbidden), errors.Is(err, clientauthsvc.ErrClientLimitReached):
		httpx.Error(w, http.StatusForbidden, "forbidden")
	default:
		httpx.PrivateError(w, http.StatusBadRequest, "request failed", err)
	}
}
