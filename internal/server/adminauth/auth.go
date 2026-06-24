package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"

	"golang.org/x/crypto/bcrypt"
)

type Role string
type Permission string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"

	PermissionAdminRead      Permission = "admin:read"
	PermissionUsersWrite     Permission = "accounts:write"
	PermissionNodesWrite     Permission = "nodes:write"
	PermissionAccessWrite    Permission = "connection:write"
	PermissionConfigsWrite   Permission = "configs:write"
	PermissionTokensWrite    Permission = "tokens:write"
	PermissionProvisionWrite Permission = "provision:write"
	PermissionAuditRead      Permission = "audit:read"
)

const (
	passwordCredentialType = model.AuthCredentialTypePassword
	passwordMinLength      = 6
	lockoutAttempts        = 10
	lockoutDuration        = 15 * time.Minute
	accessTTL              = 10 * time.Minute
	refreshTTL             = 40 * time.Minute
	refreshAbsoluteTTL     = 6 * time.Hour
	csrfTTL                = accessTTL
	refreshReuseGrace      = 10 * time.Second
)

var weakPasswords = map[string]bool{
	"admin":    true,
	"password": true,
	"123456":   true,
	"12345678": true,
	"qwerty":   true,
	"letmein":  true,
}

var ErrPasswordChangeRequired = errors.New("password change required")

type Principal struct {
	Name        string       `json:"name"`
	Role        Role         `json:"role"`
	Permissions []Permission `json:"permissions"`
	IdentityID  string       `json:"identity_id,omitempty"`
	SessionID   string       `json:"session_id,omitempty"`
}

type Store interface {
	CreateAuthIdentity(model.AuthIdentity) (model.AuthIdentity, error)
	GetAuthIdentity(id string) (model.AuthIdentity, error)
	UpdateAuthIdentity(model.AuthIdentity) (model.AuthIdentity, error)
	ListAdminIdentityProfiles() []model.AdminIdentityProfile
	FindActiveAdminIdentityByLogin(login string) (model.AuthIdentity, error)
	CreateAuthCredential(model.AuthCredential) (model.AuthCredential, error)
	UpdateAuthCredential(model.AuthCredential) (model.AuthCredential, error)
	GetAuthCredential(identityID string, credentialType model.AuthCredentialType) (model.AuthCredential, error)
	CreateAuthSession(model.AuthSession) (model.AuthSession, error)
	GetAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error)
	GetActiveAuthSessionByTokenHash(tokenHash string) (model.AuthSession, error)
	GetActiveAuthSessionByRefreshHash(refreshHash string) (model.AuthSession, error)
	GetAuthSessionByRefreshHashForRotation(refreshHash string, graceWindow time.Duration) (model.AuthSession, error)
	RotateAuthRefreshToken(oldRefreshHash string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time, graceWindow time.Duration) (model.RefreshTokenRotation, error)
	UpdateAuthSessionTokens(sessionID string, tokenHash string, expiresAt time.Time, refreshHash string, refreshExpiresAt time.Time) (model.AuthSession, error)
	UpdateAuthSessionPresence(sessionID string, lastSeenAt time.Time, onlineUntil time.Time) (model.AuthSession, error)
	RevokeAuthSessionsByIdentityID(identityID string) error
	CreateAdminProfile(model.AdminProfile) (model.AdminProfile, error)
	GetAdminProfile(identityID string) (model.AdminProfile, error)
	UpdateAdminProfile(model.AdminProfile) (model.AdminProfile, error)
}

type Config struct {
	Store      Store
	CookieName string
	CookiePath string
	SessionTTL time.Duration
	CSRFSecret string
}

type Authenticator struct {
	store      Store
	cookieName string
	cookiePath string
	sessionTTL time.Duration
	csrfSecret []byte
}

func New(config Config) *Authenticator {
	cookieName := strings.TrimSpace(config.CookieName)
	if cookieName == "" {
		cookieName = "laz_admin_session"
	}
	cookiePath := normalizeCookiePath(config.CookiePath)
	sessionTTL := config.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = accessTTL
	}
	csrfSecret := []byte(strings.TrimSpace(config.CSRFSecret))
	if len(csrfSecret) == 0 {
		csrfSecret = []byte("laz-admin-csrf-development-secret")
	}
	a := &Authenticator{
		store:      config.Store,
		cookieName: cookieName,
		cookiePath: cookiePath,
		sessionTTL: sessionTTL,
		csrfSecret: csrfSecret,
	}
	return a
}

func (a *Authenticator) AuthenticateSessionRequest(r *http.Request) (Principal, model.AuthSession, bool) {
	if a == nil || a.store == nil {
		return Principal{}, model.AuthSession{}, false
	}
	cookie, err := r.Cookie(a.cookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Principal{}, model.AuthSession{}, false
	}
	session, err := a.store.GetActiveAuthSessionByTokenHash(commontokens.Hash(cookie.Value))
	if err != nil || session.Status != model.StatusActive {
		return Principal{}, model.AuthSession{}, false
	}
	if !session.ExpiresAt.IsZero() && time.Now().UTC().After(session.ExpiresAt) {
		_ = a.store.RevokeAuthSessionsByIdentityID(session.IdentityID)
		return Principal{}, model.AuthSession{}, false
	}
	identity, err := a.store.GetAuthIdentity(session.IdentityID)
	if err != nil || identity.Status != model.StatusActive || identity.MustChangePassword {
		return Principal{}, model.AuthSession{}, false
	}
	profile, err := a.store.GetAdminProfile(identity.ID)
	if err != nil || profile.Status != model.StatusActive {
		return Principal{}, model.AuthSession{}, false
	}
	role := Role(profile.Role)
	return Principal{Name: identity.Login, Role: role, Permissions: PermissionsForRole(role), IdentityID: identity.ID, SessionID: session.ID}, session, true
}

type LoginResult struct {
	Principal               Principal
	Session                 model.AuthSession
	SessionToken            string
	RefreshToken            string
	CSRFToken               string
	MustChangePassword      bool
	PasswordChangeToken     string
	PasswordChangeSessionID string
}

func (a *Authenticator) LoginWithPassword(login, password string) (LoginResult, error) {
	if a == nil || a.store == nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	identity, err := a.store.FindActiveAdminIdentityByLogin(strings.TrimSpace(login))
	if err != nil || identity.Status != model.StatusActive {
		return LoginResult{}, ErrInvalidCredentials
	}
	credential, err := a.store.GetAuthCredential(identity.ID, passwordCredentialType)
	if err != nil || credential.Status != model.StatusActive {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !credential.LockedUntil.IsZero() && time.Now().UTC().Before(credential.LockedUntil) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte(password)) != nil {
		_ = a.recordFailedLogin(credential)
		return LoginResult{}, ErrInvalidCredentials
	}
	if credential.FailedAttempts != 0 || !credential.LockedUntil.IsZero() {
		credential.FailedAttempts = 0
		credential.LockedUntil = time.Time{}
		_, _ = a.store.UpdateAuthCredential(credential)
	}
	if identity.MustChangePassword {
		changeToken, err := commontokens.New()
		if err != nil {
			return LoginResult{}, err
		}
		limited, err := a.store.CreateAuthSession(model.AuthSession{
			IdentityID: identity.ID,
			TokenHash:  commontokens.Hash(changeToken),
			Status:     model.StatusHeld,
			ExpiresAt:  time.Now().UTC().Add(15 * time.Minute),
		})
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{MustChangePassword: true, PasswordChangeToken: changeToken, PasswordChangeSessionID: limited.ID}, ErrPasswordChangeRequired
	}
	return a.issueNormalSession(identity)
}

func (a *Authenticator) ChangePassword(changeToken, newPassword string) (LoginResult, error) {
	if a == nil || a.store == nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	session, err := a.store.GetAuthSessionByTokenHash(commontokens.Hash(changeToken))
	if err != nil || session.Status != model.StatusHeld || (!session.ExpiresAt.IsZero() && time.Now().UTC().After(session.ExpiresAt)) {
		return LoginResult{}, ErrInvalidCredentials
	}
	identity, err := a.store.GetAuthIdentity(session.IdentityID)
	if err != nil || identity.Status != model.StatusActive || !identity.MustChangePassword {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := ValidateAdminPassword(identity.Login, newPassword); err != nil {
		return LoginResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	credential, err := a.store.GetAuthCredential(identity.ID, passwordCredentialType)
	if err != nil {
		return LoginResult{}, err
	}
	credential.SecretHash = string(hash)
	credential.FailedAttempts = 0
	credential.LockedUntil = time.Time{}
	credential.Status = model.StatusActive
	if _, err := a.store.UpdateAuthCredential(credential); err != nil {
		return LoginResult{}, err
	}
	identity.MustChangePassword = false
	if _, err := a.store.UpdateAuthIdentity(identity); err != nil {
		return LoginResult{}, err
	}
	_ = a.store.RevokeAuthSessionsByIdentityID(identity.ID)
	return a.issueNormalSession(identity)
}

func (a *Authenticator) Logout(r *http.Request) error {
	_, session, ok := a.AuthenticateSessionRequest(r)
	if !ok {
		return ErrInvalidCredentials
	}
	return a.store.RevokeAuthSessionsByIdentityID(session.IdentityID)
}

func (a *Authenticator) IssueSession(identity model.AuthIdentity) (LoginResult, error) {
	return a.issueNormalSession(identity)
}

func (a *Authenticator) issueNormalSession(identity model.AuthIdentity) (LoginResult, error) {
	profile, err := a.store.GetAdminProfile(identity.ID)
	if err != nil || profile.Status != model.StatusActive {
		return LoginResult{}, ErrInvalidCredentials
	}
	now := time.Now().UTC()
	sessionToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	refreshToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	session, err := a.store.CreateAuthSession(model.AuthSession{
		IdentityID:           identity.ID,
		AccountID:            identity.AccountID,
		TokenHash:            commontokens.Hash(sessionToken),
		RefreshTokenHash:     commontokens.Hash(refreshToken),
		RefreshExpiresAt:     now.Add(refreshTTL),
		RefreshAbsoluteUntil: now.Add(refreshAbsoluteTTL),
		Status:               model.StatusActive,
		ExpiresAt:            now.Add(a.sessionTTL),
	})
	if err != nil {
		return LoginResult{}, err
	}
	principal := Principal{Name: identity.Login, Role: Role(profile.Role), Permissions: PermissionsForRole(Role(profile.Role)), IdentityID: identity.ID, SessionID: session.ID}
	csrfToken := a.NewCSRFToken(session.ID)
	return LoginResult{Principal: principal, Session: session, SessionToken: sessionToken, RefreshToken: refreshToken, CSRFToken: csrfToken}, nil
}

func (a *Authenticator) Refresh(rawRefreshToken, csrfToken string) (LoginResult, error) {
	if a == nil || a.store == nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	now := time.Now().UTC()
	refreshHash := commontokens.Hash(rawRefreshToken)
	sessionToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	refreshToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	session, err := a.store.GetAuthSessionByRefreshHashForRotation(refreshHash, refreshReuseGrace)
	if err != nil || session.RefreshTokenHash == "" || now.After(session.RefreshExpiresAt) || now.After(session.RefreshAbsoluteUntil) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if !a.ValidateCSRF(session, csrfToken) {
		return LoginResult{}, ErrInvalidCredentials
	}
	identity, err := a.store.GetAuthIdentity(session.IdentityID)
	if err != nil || identity.Status != model.StatusActive || identity.MustChangePassword {
		return LoginResult{}, ErrInvalidCredentials
	}
	profile, err := a.store.GetAdminProfile(identity.ID)
	if err != nil || profile.Status != model.StatusActive {
		return LoginResult{}, ErrInvalidCredentials
	}
	rotation, err := a.store.RotateAuthRefreshToken(refreshHash, commontokens.Hash(sessionToken), now.Add(a.sessionTTL), commontokens.Hash(refreshToken), now.Add(refreshTTL), refreshReuseGrace)
	if err != nil {
		return LoginResult{}, err
	}
	session = rotation.Session
	if rotation.Grace && rotation.GraceRefreshToken.TokenHash != "" {
		session.RefreshTokenHash = rotation.GraceRefreshToken.TokenHash
	}
	role := Role(profile.Role)
	principal := Principal{Name: identity.Login, Role: role, Permissions: PermissionsForRole(role), IdentityID: identity.ID, SessionID: session.ID}
	return LoginResult{Principal: principal, Session: session, SessionToken: sessionToken, RefreshToken: refreshToken, CSRFToken: a.NewCSRFToken(session.ID)}, nil
}

func (a *Authenticator) recordFailedLogin(credential model.AuthCredential) error {
	credential.FailedAttempts++
	if credential.FailedAttempts >= lockoutAttempts {
		credential.LockedUntil = time.Now().UTC().Add(lockoutDuration)
	}
	_, err := a.store.UpdateAuthCredential(credential)
	return err
}

func (a *Authenticator) CookieName() string {
	if a == nil || a.cookieName == "" {
		return "laz_admin_session"
	}
	return a.cookieName
}

func (a *Authenticator) CookiePath() string {
	if a == nil || a.cookiePath == "" {
		return "/"
	}
	return a.cookiePath
}

func (a *Authenticator) NewSessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: a.CookieName(), Value: token, Path: a.CookiePath(), HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: false, MaxAge: int(a.sessionTTL.Seconds())}
}

func (a *Authenticator) RefreshCookieName() string {
	return a.CookieName() + "_refresh"
}

func (a *Authenticator) NewRefreshCookie(token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	return &http.Cookie{Name: a.RefreshCookieName(), Value: token, Path: a.CookiePath(), HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: false, MaxAge: maxAge}
}

func (a *Authenticator) ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: a.CookieName(), Value: "", Path: a.CookiePath(), HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1}
}

func (a *Authenticator) ExpiredRefreshCookie() *http.Cookie {
	return &http.Cookie{Name: a.RefreshCookieName(), Value: "", Path: a.CookiePath(), HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1}
}

func (a *Authenticator) Timings(session model.AuthSession) map[string]any {
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

func normalizeCookiePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "/"
	}
	return "/" + strings.Trim(value, "/")
}

func (a *Authenticator) ValidateCSRF(session model.AuthSession, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || a == nil {
		return false
	}
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return false
	}
	sessionID, expText, sig := parts[0], parts[1], parts[2]
	if sessionID != session.ID {
		return false
	}
	var exp int64
	if _, err := fmt.Sscanf(expText, "%d", &exp); err != nil || time.Now().UTC().Unix() > exp {
		return false
	}
	expected := a.csrfSignature(sessionID + ":" + expText)
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

func (a *Authenticator) NewCSRFToken(sessionID string) string {
	exp := time.Now().UTC().Add(csrfTTL).Unix()
	payload := fmt.Sprintf("%s:%d", sessionID, exp)
	return payload + ":" + a.csrfSignature(payload)
}

func (a *Authenticator) csrfSignature(payload string) string {
	mac := hmac.New(sha256.New, a.csrfSecret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func ValidateAdminPassword(login, password string) error {
	password = strings.TrimSpace(password)
	if len(password) < passwordMinLength {
		return fmt.Errorf("password must be at least %d characters", passwordMinLength)
	}
	lower := strings.ToLower(password)
	if weakPasswords[lower] || strings.EqualFold(strings.TrimSpace(login), password) {
		return errors.New("password is too weak")
	}
	if !hasRequiredPasswordComposition(password) {
		return errors.New("password must include at least one letter, one digit, and one symbol")
	}
	return nil
}

func hasRequiredPasswordComposition(password string) bool {
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

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func PermissionsForRole(role Role) []Permission {
	switch role {
	case RoleOwner:
		return []Permission{PermissionAdminRead, PermissionUsersWrite, PermissionNodesWrite, PermissionAccessWrite, PermissionConfigsWrite, PermissionTokensWrite, PermissionProvisionWrite, PermissionAuditRead}
	case RoleAdmin:
		return []Permission{PermissionAdminRead, PermissionUsersWrite, PermissionNodesWrite, PermissionAccessWrite, PermissionConfigsWrite, PermissionTokensWrite, PermissionProvisionWrite, PermissionAuditRead}
	case RoleViewer:
		return []Permission{PermissionAdminRead, PermissionAuditRead}
	default:
		return nil
	}
}

func HasPermission(principal Principal, permission Permission) bool {
	for _, item := range principal.Permissions {
		if item == permission {
			return true
		}
	}
	return false
}

func Authorize(ctx context.Context, permission Permission) (Principal, bool) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, false
	}
	if !HasPermission(principal, permission) {
		return Principal{}, false
	}
	return principal, true
}
