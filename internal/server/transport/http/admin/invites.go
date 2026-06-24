package admin

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	adminauthsvc "laz/internal/server/adminauth"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/transport/http/httpx"

	"golang.org/x/crypto/bcrypt"
)

const (
	adminInviteDefaultTTL = 24 * time.Hour
	adminInviteMaxTTL     = 7 * 24 * time.Hour
)

type adminInviteResponse struct {
	ID                     string                  `json:"id"`
	Role                   string                  `json:"role"`
	Status                 model.AdminInviteStatus `json:"status"`
	ExpiresAt              time.Time               `json:"expires_at"`
	InvitedByIdentityID    string                  `json:"invited_by_identity_id,omitempty"`
	InvitedByLoginSnapshot string                  `json:"invited_by_login_snapshot"`
	UsedByIdentityID       string                  `json:"used_by_identity_id,omitempty"`
	UsedByLogin            string                  `json:"used_by_login,omitempty"`
	UsedByDisplayName      string                  `json:"used_by_display_name,omitempty"`
	UsedAt                 time.Time               `json:"used_at,omitempty"`
	RevokedAt              time.Time               `json:"revoked_at,omitempty"`
	Note                   string                  `json:"note"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
}

func (a *App) adminInvitesHandler(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.listAdminInvites(w, r)
	case http.MethodPost:
		a.createAdminInvite(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) adminInviteSubroutes(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, a.webPrefix+"/api/v1/admin-invites/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[1] == "revoke" && r.Method == http.MethodPost {
		invite, err := a.store.RevokeAdminInvite(parts[0])
		if err != nil {
			httpx.Error(w, http.StatusNotFound, "invite not found or not active")
			return
		}
		a.recordAudit(r, auditsvc.Event{Action: "admin.invite_revoke", EntityType: "admin_invite", EntityID: invite.ID})
		httpx.JSON(w, http.StatusOK, map[string]any{"invite": adminInviteJSON(invite)})
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (a *App) listAdminInvites(w http.ResponseWriter, r *http.Request) {
	items := a.store.ListAdminInvites()
	out := make([]adminInviteResponse, 0, len(items))
	for _, item := range items {
		out = append(out, adminInviteJSON(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": out})
}

func (a *App) createAdminInvite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Role      string `json:"role"`
		ExpiresIn string `json:"expires_in"`
		TTL       string `json:"ttl"`
		Note      string `json:"note"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	role := model.AdminRole(strings.TrimSpace(input.Role))
	if !validAdminRole(role) {
		httpx.Error(w, http.StatusBadRequest, "valid role is required")
		return
	}
	ttl, err := parseInviteTTL(input.ExpiresIn, input.TTL)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	principal, _ := adminauthsvc.PrincipalFromContext(r.Context())
	result, err := a.createAdminInviteRecord(role, ttl, strings.TrimSpace(input.Note), principal.IdentityID, principal.Name)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create invite")
		return
	}
	a.recordAudit(r, auditsvc.Event{Action: "admin.invite_create", EntityType: "admin_invite", EntityID: result.Invite.ID, Details: map[string]any{"role": role}})
	httpx.JSON(w, http.StatusCreated, map[string]any{"invite": adminInviteJSON(result.Invite), "invite_url": result.URL, "invite_code": result.Code, "expires_at": result.Invite.ExpiresAt})
}

func (a *App) enrollAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
		httpx.Error(w, http.StatusForbidden, "cross-site admin request rejected")
		return
	}
	var input struct {
		InviteToken string `json:"invite_token"`
		InviteCode  string `json:"invite_code"`
		Login       string `json:"login"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	invite, err := a.store.GetAdminInviteByTokenHash(commontokens.Hash(strings.TrimSpace(input.InviteToken)))
	if err != nil || invite.Status != model.AdminInviteStatusActive || !time.Now().UTC().Before(invite.ExpiresAt) {
		httpx.Error(w, http.StatusUnauthorized, "invite is invalid or expired")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(invite.CodeHash), []byte(normalizeInviteCode(input.InviteCode))) != nil {
		httpx.Error(w, http.StatusUnauthorized, "invite code is invalid")
		return
	}
	login := strings.TrimSpace(input.Login)
	if login == "" {
		httpx.Error(w, http.StatusBadRequest, "login is required")
		return
	}
	if _, err := a.store.FindActiveAdminIdentityByLogin(login); err == nil {
		httpx.Error(w, http.StatusConflict, "admin login already exists")
		return
	}
	if err := adminauthsvc.ValidateAdminPassword(login, input.Password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = login
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(input.Password)), bcrypt.DefaultCost)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	identity, err := a.store.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: login, DisplayName: displayName, Status: model.StatusActive, MustChangePassword: false, VerifiedAt: time.Now().UTC()})
	if err != nil {
		httpx.Error(w, http.StatusConflict, "admin login already exists")
		return
	}
	if _, err := a.store.CreateAuthCredential(model.AuthCredential{IdentityID: identity.ID, Type: model.AuthCredentialTypePassword, SecretHash: string(hash), Status: model.StatusActive}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create credential")
		return
	}
	if _, err := a.store.CreateAdminProfile(model.AdminProfile{IdentityID: identity.ID, Role: invite.Role, Status: model.StatusActive}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to create admin profile")
		return
	}
	if _, err := a.store.MarkAdminInviteUsed(invite.ID, identity.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to mark invite used")
		return
	}
	result, err := a.adminAuth.IssueSession(identity)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to issue session")
		return
	}
	setAdminAuthCookies(w, r, a.adminAuth.NewSessionCookie(result.SessionToken), a.adminAuth.NewRefreshCookie(result.RefreshToken, result.Session.RefreshExpiresAt))
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), result.Principal)), auditsvc.Event{Action: "admin.enroll", EntityType: "admin_identity", EntityID: identity.ID, Details: map[string]any{"role": invite.Role}})
	httpx.JSON(w, http.StatusOK, adminAuthPayload(a.adminAuth, result))
}

type createdAdminInvite struct {
	Invite model.AdminInvite
	Token  string
	Code   string
	URL    string
}

func (c createdAdminInvite) BootstrapInvite() adminauthsvc.BootstrapInvite {
	return adminauthsvc.BootstrapInvite{ID: c.Invite.ID, URL: c.URL, Code: c.Code, ExpiresAt: c.Invite.ExpiresAt}
}

func (a *App) createAdminInviteRecord(role model.AdminRole, ttl time.Duration, note string, invitedByID string, invitedByLogin string) (createdAdminInvite, error) {
	token, err := commontokens.New()
	if err != nil {
		return createdAdminInvite{}, err
	}
	code, err := newInviteCode()
	if err != nil {
		return createdAdminInvite{}, err
	}
	codeHash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return createdAdminInvite{}, err
	}
	invite, err := a.store.CreateAdminInvite(model.AdminInvite{TokenHash: commontokens.Hash(token), CodeHash: string(codeHash), Role: role, Status: model.AdminInviteStatusActive, ExpiresAt: time.Now().UTC().Add(ttl), InvitedByIdentityID: invitedByID, InvitedByLoginSnapshot: invitedByLogin, Note: note})
	if err != nil {
		return createdAdminInvite{}, err
	}
	return createdAdminInvite{Invite: invite, Token: token, Code: code, URL: a.adminInviteURL(token)}, nil
}

func (a *App) adminInviteURL(token string) string {
	path := a.webPrefix + "/invite/" + token
	if a.publicBaseURL != "" {
		return a.publicBaseURL + path
	}
	return path
}

func parseInviteTTL(values ...string) (time.Duration, error) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		ttl, err := time.ParseDuration(value)
		if err != nil || ttl <= 0 {
			return 0, fmt.Errorf("ttl must be a positive duration")
		}
		if ttl > adminInviteMaxTTL {
			return 0, fmt.Errorf("ttl must not exceed 7d")
		}
		return ttl, nil
	}
	return adminInviteDefaultTTL, nil
}

func newInviteCode() (string, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	value := uint64(0)
	for _, b := range raw {
		value = (value << 8) | uint64(b)
	}
	value %= 1000000000000
	return fmt.Sprintf("%04d-%04d-%04d", value/100000000, (value/10000)%10000, value%10000), nil
}

func normalizeInviteCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func adminInviteJSON(invite model.AdminInvite) adminInviteResponse {
	return adminInviteResponse{
		ID:                     invite.ID,
		Role:                   string(invite.Role),
		Status:                 invite.Status,
		ExpiresAt:              invite.ExpiresAt,
		InvitedByIdentityID:    invite.InvitedByIdentityID,
		InvitedByLoginSnapshot: invite.InvitedByLoginSnapshot,
		UsedByIdentityID:       invite.UsedByIdentityID,
		UsedByLogin:            invite.UsedByLogin,
		UsedByDisplayName:      invite.UsedByDisplayName,
		UsedAt:                 invite.UsedAt,
		RevokedAt:              invite.RevokedAt,
		Note:                   invite.Note,
		CreatedAt:              invite.CreatedAt,
		UpdatedAt:              invite.UpdatedAt,
	}
}
