package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	"laz/internal/server/transport/http/httpx"

	"golang.org/x/crypto/bcrypt"
)

const (
	strongCookiePurpose      = "strong-user-cookie-v1"
	strongRefreshPurpose     = "strong-user-refresh-v1"
	strongCSRFPurpose        = "strong-user-csrf-v1"
	strongCookieName         = "laz_user_session"
	strongRefreshCookieName  = "laz_user_refresh"
	strongSessionTTL         = 10 * time.Minute
	strongRefreshTTL         = 40 * time.Minute
	strongRefreshAbsoluteTTL = 6 * time.Hour
	userPasswordMinLen       = 6
	strongLockAttempts       = 10
	strongLockDuration       = 15 * time.Minute
)

var weakUserPasswords = map[string]bool{
	"admin":    true,
	"password": true,
	"123456":   true,
	"12345678": true,
	"qwerty":   true,
	"letmein":  true,
}

type strongAuthContext struct {
	Identity model.AuthIdentity
	Session  model.AuthSession
	Account  model.Account
}

func (a *App) validateUserPassword(login, password string) error {
	password = strings.TrimSpace(password)
	if len(password) < userPasswordMinLen {
		return fmt.Errorf("password must be at least %d characters", userPasswordMinLen)
	}
	if strings.EqualFold(strings.TrimSpace(login), password) || weakUserPasswords[strings.ToLower(password)] {
		return errors.New("password is too weak")
	}
	if !hasRequiredUserPasswordComposition(password) {
		return errors.New("password must include at least one letter, one digit, and one symbol")
	}
	return nil
}

func hasRequiredUserPasswordComposition(password string) bool {
	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	return hasLetter && hasDigit && hasSymbol
}

func (a *App) setupStrongUser(account model.Account, password string) (string, model.AuthIdentity, model.AuthSession, error) {
	if err := a.validateUserPassword(account.Username, password); err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	identity, err := a.store.FindActiveUserIdentityByAccountID(account.ID)
	if errors.Is(err, store.ErrNotFound) {
		identity, err = a.store.CreateAuthIdentity(model.AuthIdentity{
			Kind:      model.AuthIdentityKindUser,
			Login:     account.Username,
			AccountID: account.ID,
			Status:    model.StatusActive,
		})
	}
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	credential, err := a.store.GetAuthCredential(identity.ID, model.AuthCredentialTypePassword)
	if errors.Is(err, store.ErrNotFound) {
		_, err = a.store.CreateAuthCredential(model.AuthCredential{IdentityID: identity.ID, Type: model.AuthCredentialTypePassword, SecretHash: string(hash), Status: model.StatusActive})
	} else if err == nil {
		credential.SecretHash = string(hash)
		credential.FailedAttempts = 0
		credential.LockedUntil = time.Time{}
		credential.Status = model.StatusActive
		_, err = a.store.UpdateAuthCredential(credential)
	}
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	return a.createStrongSession(identity, account.ID)
}

func (a *App) loginStrongUser(account model.Account, password string) (string, model.AuthIdentity, model.AuthSession, error) {
	identity, err := a.store.FindActiveUserIdentityByAccountID(account.ID)
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	credential, err := a.store.GetAuthCredential(identity.ID, model.AuthCredentialTypePassword)
	if err != nil || credential.Status != model.StatusActive {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	if !credential.LockedUntil.IsZero() && time.Now().UTC().Before(credential.LockedUntil) {
		return "", model.AuthIdentity{}, model.AuthSession{}, clientAuthErrLocked{}
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte(password)) != nil {
		credential.FailedAttempts++
		if credential.FailedAttempts >= strongLockAttempts {
			credential.LockedUntil = time.Now().UTC().Add(strongLockDuration)
		}
		_, _ = a.store.UpdateAuthCredential(credential)
		return "", model.AuthIdentity{}, model.AuthSession{}, clientAuthErrInvalid{}
	}
	credential.FailedAttempts = 0
	credential.LockedUntil = time.Time{}
	_, _ = a.store.UpdateAuthCredential(credential)
	return a.createStrongSession(identity, account.ID)
}

func (a *App) createStrongSession(identity model.AuthIdentity, accountID string) (string, model.AuthIdentity, model.AuthSession, error) {
	raw, err := commontokens.New()
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	refresh, err := commontokens.New()
	if err != nil {
		return "", model.AuthIdentity{}, model.AuthSession{}, err
	}
	now := time.Now().UTC()
	session, err := a.store.CreateAuthSession(model.AuthSession{IdentityID: identity.ID, AccountID: accountID, TokenHash: commontokens.Hash(raw), RefreshTokenHash: commontokens.Hash(refresh), RefreshExpiresAt: now.Add(strongRefreshTTL), RefreshAbsoluteUntil: now.Add(strongRefreshAbsoluteTTL), Status: model.StatusActive, ExpiresAt: now.Add(strongSessionTTL)})
	return raw + ":" + refresh, identity, session, err
}

func splitStrongRawTokens(rawTokens string) (string, string) {
	parts := strings.SplitN(rawTokens, ":", 2)
	if len(parts) != 2 {
		return rawTokens, ""
	}
	return parts[0], parts[1]
}

func (a *App) issueStrongCookie(w http.ResponseWriter, r *http.Request, rawTokens string) string {
	rawSession, rawRefresh := splitStrongRawTokens(rawTokens)
	expiresAt := time.Now().UTC().Add(strongSessionTTL).Unix()
	value := a.signedValue(strongCookiePurpose, commontokens.Hash(rawSession), expiresAt)
	http.SetCookie(w, &http.Cookie{Name: strongCookieName, Value: value, Path: "/client/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: requestIsHTTPS(r), MaxAge: int(strongSessionTTL.Seconds())})
	if rawRefresh != "" {
		a.issueStrongRefreshCookie(w, r, rawRefresh, time.Now().UTC().Add(strongRefreshTTL))
	}
	return a.newStrongCSRF(value)
}

func (a *App) issueStrongRefreshCookie(w http.ResponseWriter, r *http.Request, rawRefresh string, expiresAt time.Time) {
	value := a.signedValue(strongRefreshPurpose, commontokens.Hash(rawRefresh), expiresAt.Unix())
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{Name: strongRefreshCookieName, Value: value, Path: "/client/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: requestIsHTTPS(r), MaxAge: maxAge})
}

func (a *App) clearStrongCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: strongCookieName, Value: "", Path: "/client/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: strongRefreshCookieName, Value: "", Path: "/client/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (a *App) authenticateStrong(w http.ResponseWriter, r *http.Request, requireCSRF bool) (strongAuthContext, bool) {
	cookie, err := r.Cookie(strongCookieName)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		parts := strings.Split(cookie.Value, ":")
		if len(parts) == 3 {
			session, err := a.store.GetActiveAuthSessionByTokenHash(parts[0])
			if err == nil && a.validateSignedCookie(strongCookiePurpose, cookie.Value) && (!requireCSRF || a.validateStrongCSRF(cookie.Value, r.Header.Get("X-CSRF-Token"))) {
				ctx, ok := a.strongContextFromSession(session)
				if ok {
					return ctx, true
				}
			}
		}
	}

	if requireCSRF {
		httpx.Error(w, http.StatusForbidden, "strong session and csrf required")
	} else {
		httpx.Error(w, http.StatusUnauthorized, "strong session required")
	}
	return strongAuthContext{}, false
}

func (a *App) strongContextFromSession(session model.AuthSession) (strongAuthContext, bool) {
	if session.Status != model.StatusActive || (!session.ExpiresAt.IsZero() && time.Now().UTC().After(session.ExpiresAt)) {
		if session.IdentityID != "" {
			_ = a.store.RevokeAuthSessionsByIdentityID(session.IdentityID)
		}
		return strongAuthContext{}, false
	}
	return a.strongContextFromSessionIgnoringAccessExpiry(session)
}

func (a *App) strongContextFromSessionIgnoringAccessExpiry(session model.AuthSession) (strongAuthContext, bool) {
	if session.Status != model.StatusActive {
		return strongAuthContext{}, false
	}
	identity, err := a.store.GetAuthIdentity(session.IdentityID)
	if err != nil || identity.Kind != model.AuthIdentityKindUser || identity.Status != model.StatusActive {
		return strongAuthContext{}, false
	}
	account, err := a.store.GetAccount(identity.AccountID)
	if err != nil || account.Status != model.AccountStatusActive {
		return strongAuthContext{}, false
	}
	return strongAuthContext{Identity: identity, Session: session, Account: account}, true
}

func (a *App) validateSignedCookie(purpose, value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return false
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().UTC().Unix() > expires {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(a.sign(purpose, parts[0]+":"+parts[1])))
}

func (a *App) newStrongCSRF(cookieValue string) string {
	expiresAt := time.Now().UTC().Add(strongSessionTTL).Unix()
	return a.signedValue(strongCSRFPurpose, commontokens.Hash(cookieValue), expiresAt)
}

func (a *App) validateStrongRefreshCookie(value string) string {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || !a.validateSignedCookie(strongRefreshPurpose, value) {
		return ""
	}
	return parts[0]
}

func (a *App) validateStrongCSRF(cookieValue, csrf string) bool {
	parts := strings.Split(strings.TrimSpace(csrf), ":")
	if len(parts) != 3 || parts[0] != commontokens.Hash(cookieValue) {
		return false
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().UTC().Unix() > expiresAt {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(a.sign(strongCSRFPurpose, parts[0]+":"+parts[1])))
}

func newStrongRecoveryCode() (string, string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	encoded := strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), "=")
	code := encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12]
	sum := sha256.Sum256([]byte(code))
	return code, fmt.Sprintf("%x", sum[:]), nil
}

func recoveryCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return fmt.Sprintf("%x", sum[:])
}

func strongAuthTimings(session model.AuthSession) map[string]any {
	now := time.Now().UTC()
	refreshAfter := session.ExpiresAt.Add(-2 * time.Minute)
	if refreshAfter.Before(now) {
		refreshAfter = now
	}
	return map[string]any{
		"access_expires_at":  session.ExpiresAt,
		"refresh_expires_at": session.RefreshExpiresAt,
		"refresh_after":      refreshAfter,
		"server_time":        now,
	}
}

type clientAuthErrInvalid struct{}

func (clientAuthErrInvalid) Error() string { return "invalid credentials" }

type clientAuthErrLocked struct{}

func (clientAuthErrLocked) Error() string { return "temporarily locked" }
