package adminauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	commontokens "laz/internal/common/tokens"
	"laz/internal/model"
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

type Principal struct {
	Name        string       `json:"name"`
	Role        Role         `json:"role"`
	Permissions []Permission `json:"permissions"`
}

type Store interface {
	CreateAdminSession(model.AdminSession) (model.AdminSession, error)
	GetAdminSessionByHash(hash string) (model.AdminSession, error)
	TouchAdminSession(id string) error
	RevokeAdminSession(id string) error
}

type Config struct {
	Store       Store
	Token       string
	TokenSHA256 string
	CookieName  string
	SessionTTL  time.Duration
}

type Authenticator struct {
	store       Store
	token       string
	tokenSHA256 string
	cookieName  string
	sessionTTL  time.Duration
}

func New(config Config) *Authenticator {
	cookieName := strings.TrimSpace(config.CookieName)
	if cookieName == "" {
		cookieName = "laz_admin_session"
	}
	sessionTTL := config.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 12 * time.Hour
	}
	return &Authenticator{
		store:       config.Store,
		token:       strings.TrimSpace(config.Token),
		tokenSHA256: strings.ToLower(strings.TrimSpace(config.TokenSHA256)),
		cookieName:  cookieName,
		sessionTTL:  sessionTTL,
	}
}

func (a *Authenticator) AuthenticateRequest(r *http.Request) (Principal, bool) {
	if a == nil {
		return Principal{}, false
	}
	raw := bearerToken(r.Header.Get("Authorization"))
	if raw != "" {
		if !a.validToken(raw) {
			return Principal{}, false
		}
		return Principal{
			Name:        "admin",
			Role:        RoleOwner,
			Permissions: PermissionsForRole(RoleOwner),
		}, true
	}
	principal, _, ok := a.AuthenticateSessionRequest(r)
	return principal, ok
}

func (a *Authenticator) AuthenticateSessionRequest(r *http.Request) (Principal, model.AdminSession, bool) {
	if a == nil || a.store == nil {
		return Principal{}, model.AdminSession{}, false
	}
	cookie, err := r.Cookie(a.cookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Principal{}, model.AdminSession{}, false
	}
	session, err := a.store.GetAdminSessionByHash(commontokens.Hash(cookie.Value))
	if err != nil || session.Status != model.StatusActive {
		return Principal{}, model.AdminSession{}, false
	}
	if !session.ExpiresAt.IsZero() && time.Now().UTC().After(session.ExpiresAt) {
		_ = a.store.RevokeAdminSession(session.ID)
		return Principal{}, model.AdminSession{}, false
	}
	_ = a.store.TouchAdminSession(session.ID)
	role := Role(session.Role)
	return Principal{
		Name:        session.PrincipalName,
		Role:        role,
		Permissions: PermissionsForRole(role),
	}, session, true
}

type LoginResult struct {
	Principal    Principal
	Session      model.AdminSession
	SessionToken string
	CSRFToken    string
}

func (a *Authenticator) Login(rawToken string) (LoginResult, error) {
	if a == nil || a.store == nil || !a.validToken(rawToken) {
		return LoginResult{}, ErrInvalidCredentials
	}
	sessionToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, err := commontokens.New()
	if err != nil {
		return LoginResult{}, err
	}
	principal := Principal{Name: "admin", Role: RoleOwner, Permissions: PermissionsForRole(RoleOwner)}
	session, err := a.store.CreateAdminSession(model.AdminSession{
		TokenHash:     commontokens.Hash(sessionToken),
		CSRFTokenHash: commontokens.Hash(csrfToken),
		PrincipalName: principal.Name,
		Role:          string(principal.Role),
		ExpiresAt:     time.Now().UTC().Add(a.sessionTTL),
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Principal: principal, Session: session, SessionToken: sessionToken, CSRFToken: csrfToken}, nil
}

func (a *Authenticator) Logout(r *http.Request) error {
	_, session, ok := a.AuthenticateSessionRequest(r)
	if !ok {
		return ErrInvalidCredentials
	}
	return a.store.RevokeAdminSession(session.ID)
}

func (a *Authenticator) CookieName() string {
	if a == nil || a.cookieName == "" {
		return "laz_admin_session"
	}
	return a.cookieName
}

func (a *Authenticator) NewSessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     a.CookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
		MaxAge:   int(a.sessionTTL.Seconds()),
	}
}

func (a *Authenticator) ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     a.CookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

func (a *Authenticator) ValidateCSRF(session model.AdminSession, token string) bool {
	token = strings.TrimSpace(token)
	return token != "" && subtle.ConstantTimeCompare([]byte(commontokens.Hash(token)), []byte(session.CSRFTokenHash)) == 1
}

func (a *Authenticator) validToken(raw string) bool {
	if a.tokenSHA256 != "" {
		sum := sha256.Sum256([]byte(raw))
		got := hex.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(got), []byte(a.tokenSHA256)) == 1
	}
	if a.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(raw), []byte(a.token)) == 1
}

func bearerToken(header string) string {
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
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
		return []Permission{
			PermissionAdminRead,
			PermissionUsersWrite,
			PermissionNodesWrite,
			PermissionAccessWrite,
			PermissionConfigsWrite,
			PermissionTokensWrite,
			PermissionProvisionWrite,
			PermissionAuditRead,
		}
	case RoleAdmin:
		return []Permission{
			PermissionAdminRead,
			PermissionUsersWrite,
			PermissionNodesWrite,
			PermissionAccessWrite,
			PermissionConfigsWrite,
			PermissionTokensWrite,
			PermissionProvisionWrite,
			PermissionAuditRead,
		}
	case RoleViewer:
		return []Permission{
			PermissionAdminRead,
			PermissionAuditRead,
		}
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
