package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	subscriptionssvc "laz/internal/server/subscriptions"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) hpSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rawToken := strings.Trim(strings.TrimPrefix(r.URL.Path, "/hp/"), "/")
	token, ok := a.authenticateClientToken(w, r, rawToken)
	if !ok {
		return
	}
	if token.ClientID == "" {
		httpx.Error(w, http.StatusBadRequest, "client-bound token is required")
		return
	}
	summary, err := a.store.ClientSummary(token.AccountID, token.ClientID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	if profile == "" {
		profile = "all"
	}
	meta := a.subscriptions.ProfileMeta(profile)
	if !meta.Found {
		httpx.Error(w, http.StatusNotFound, "profile not found")
		return
	}
	setHPProfileHeaders(w, meta)
	body := a.subscriptions.HPSubscriptionBody(token, summary)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (a *App) shortSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/c/"), "/")
	if id == "" || strings.Contains(id, "/") {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	link, err := a.store.GetShortLink(id)
	if err != nil || link.Status != model.StatusActive {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	token, ok := a.clientTokenByID(link.TokenID)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "invalid token")
		return
	}
	summary, err := a.store.ClientSummary(token.AccountID, token.ClientID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	meta := a.subscriptions.ProfileMeta(link.Profile)
	if !meta.Found {
		httpx.Error(w, http.StatusNotFound, "profile not found")
		return
	}
	setHPProfileHeaders(w, meta)
	body := a.subscriptions.HPSubscriptionBody(token, summary)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (a *App) getOrCreateShortLink(r *http.Request, token model.AccessToken, profile string) (model.ShortLink, error) {
	if a.subscriptions == nil {
		return model.ShortLink{}, store.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	return a.subscriptions.GetOrCreateHPShortLink(
		ctx,
		token,
		profile,
		func(id string) string { return a.absoluteURL(r, "/c/"+id) },
		commontokens.NewShortID,
		HPLinkEncryptor,
	)
}

func encryptHPLink(ctx context.Context, rawURL string) (string, error) {
	body, err := json.Marshal(map[string]string{"url": rawURL})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://crypto.happ.su/api-v2.php", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("crypto link failed: HTTP %d", resp.StatusCode)
	}
	text := strings.TrimSpace(string(raw))
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) == nil {
		for _, key := range []string{"encrypted_link", "url", "link", "result", "encrypted"} {
			if value, ok := decoded[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), nil
			}
		}
	}
	if strings.HasPrefix(text, "happ://") {
		return text, nil
	}
	return "", fmt.Errorf("crypto link response has unknown format")
}

var HPLinkEncryptor = encryptHPLink

func (a *App) subscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rawToken := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sub/"), "/")
	token, ok := a.authenticateClientToken(w, r, rawToken)
	if !ok {
		return
	}
	if token.ClientID == "" {
		httpx.Error(w, http.StatusBadRequest, "client-bound token is required")
		return
	}
	summary, err := a.store.ClientSummary(token.AccountID, token.ClientID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(a.subscriptions.ClosedSubscriptionBody(token, summary)))
}

func setHPProfileHeaders(w http.ResponseWriter, meta subscriptionssvc.ProfileMeta) {
	if meta.Routing != "" {
		w.Header().Set("routing", meta.Routing)
	}
	if meta.Title != "" {
		w.Header().Set("profile-title", meta.Title)
	}
	if meta.Announce != "" {
		w.Header().Set("announce", "base64:"+base64.StdEncoding.EncodeToString([]byte(meta.Announce)))
	}
}
