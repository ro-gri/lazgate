package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	adminauthsvc "laz/internal/server/adminauth"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/model"
	"laz/internal/server/transport/http/httpx"

	"golang.org/x/crypto/bcrypt"
)

type adminIdentityResponse struct {
	ID                 string       `json:"id"`
	Login              string       `json:"login"`
	DisplayName        string       `json:"display_name"`
	Role               string       `json:"role"`
	Status             model.Status `json:"status"`
	MustChangePassword bool         `json:"must_change_password"`
	LastSeenAt         time.Time    `json:"last_seen_at"`
	OnlineUntil        time.Time    `json:"online_until"`
	Online             bool         `json:"online"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
}

func (a *App) adminsHandler(w http.ResponseWriter, r *http.Request) {
	if !a.requireOwner(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.listAdmins(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) adminSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, a.webPrefix+"/api/v1/admins/"), "/")
	if path == "" {
		httpx.Error(w, http.StatusNotFound, "admin not found")
		return
	}
	parts := strings.Split(path, "/")
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodPatch {
		if !a.requireOwner(w, r) {
			return
		}
		a.updateAdmin(w, r, id)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "reset-password":
			a.resetAdminPassword(w, r, id)
			return
		case "revoke-sessions":
			if !a.requireOwner(w, r) {
				return
			}
			a.revokeAdminSessions(w, r, id)
			return
		}
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (a *App) requireOwner(w http.ResponseWriter, r *http.Request) bool {
	principal, ok := adminauthsvc.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if principal.Role != adminauthsvc.RoleOwner {
		httpx.Error(w, http.StatusForbidden, "owner role required")
		return false
	}
	return true
}

func (a *App) listAdmins(w http.ResponseWriter, r *http.Request) {
	items := a.store.ListAdminIdentityProfiles()
	out := make([]adminIdentityResponse, 0, len(items))
	for _, item := range items {
		out = append(out, adminResponse(item))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"admins": out})
}

func (a *App) updateAdmin(w http.ResponseWriter, r *http.Request, id string) {
	identity, profile, ok := a.adminIdentityProfile(w, id)
	if !ok {
		return
	}
	var input struct {
		Role        *string `json:"role"`
		Status      *string `json:"status"`
		DisplayName *string `json:"display_name"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if input.Role != nil {
		role := model.AdminRole(strings.TrimSpace(*input.Role))
		if !validAdminRole(role) {
			httpx.Error(w, http.StatusBadRequest, "invalid role")
			return
		}
		profile.Role = role
	}
	if input.Status != nil {
		status := model.Status(strings.TrimSpace(*input.Status))
		if status != model.StatusActive && status != model.StatusDeleted {
			httpx.Error(w, http.StatusBadRequest, "invalid status")
			return
		}
		identity.Status = status
		profile.Status = status
	}
	if input.DisplayName != nil {
		identity.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if err := a.preventLastActiveOwner(identity.ID, profile.Role, identity.Status, profile.Status); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedIdentity, err := a.store.UpdateAuthIdentity(identity)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update admin identity")
		return
	}
	updatedProfile, err := a.store.UpdateAdminProfile(profile)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update admin profile")
		return
	}
	_ = a.store.RevokeAuthSessionsByIdentityID(identity.ID)
	a.recordAudit(r, auditsvc.Event{Action: "admin.identity_update", EntityType: "admin_identity", EntityID: identity.ID, Details: map[string]any{"role": updatedProfile.Role, "status": updatedIdentity.Status}})
	httpx.JSON(w, http.StatusOK, map[string]any{"admin": adminResponse(model.AdminIdentityProfile{Identity: updatedIdentity, Profile: updatedProfile})})
}

func (a *App) resetAdminPassword(w http.ResponseWriter, r *http.Request, id string) {
	principal, _ := adminauthsvc.PrincipalFromContext(r.Context())
	identity, _, ok := a.adminIdentityProfile(w, id)
	if !ok {
		return
	}
	var input struct {
		Password        string `json:"password"`
		NewPassword     string `json:"new_password"`
		CurrentPassword string `json:"current_password"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if identity.ID == principal.IdentityID {
		a.changeOwnAdminPassword(w, r, identity, input.CurrentPassword, input.NewPassword)
		return
	}
	if !a.requireOwner(w, r) {
		return
	}
	a.ownerResetAdminPassword(w, r, identity, input.Password)
}

func (a *App) changeOwnAdminPassword(w http.ResponseWriter, r *http.Request, identity model.AuthIdentity, currentPassword string, newPassword string) {
	credential, err := a.store.GetAuthCredential(identity.ID, model.AuthCredentialTypePassword)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "credential not found")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte(currentPassword)) != nil {
		httpx.Error(w, http.StatusUnauthorized, "current password is invalid")
		return
	}
	if err := adminauthsvc.ValidateAdminPassword(identity.Login, newPassword); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.updateAdminPasswordCredential(w, credential, newPassword) {
		return
	}
	identity.MustChangePassword = false
	if _, err := a.store.UpdateAuthIdentity(identity); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update admin identity")
		return
	}
	principal, _ := adminauthsvc.PrincipalFromContext(r.Context())
	if principal.SessionID == "" {
		_ = a.store.RevokeAuthSessionsByIdentityID(identity.ID)
		a.recordAudit(r, auditsvc.Event{Action: "admin.password_change", EntityType: "admin_identity", EntityID: identity.ID, Details: map[string]any{"sessions": "all_revoked"}})
		httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok", "session": "revoked"})
		return
	}
	_ = a.store.RevokeOtherAuthSessionsByIdentityID(identity.ID, principal.SessionID)
	a.recordAudit(r, auditsvc.Event{Action: "admin.password_change", EntityType: "admin_identity", EntityID: identity.ID, Details: map[string]any{"sessions": "other_revoked"}})
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok", "session": "current_kept"})
}

func (a *App) ownerResetAdminPassword(w http.ResponseWriter, r *http.Request, identity model.AuthIdentity, password string) {
	if err := adminauthsvc.ValidateAdminPassword(identity.Login, password); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	credential, err := a.store.GetAuthCredential(identity.ID, model.AuthCredentialTypePassword)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "credential not found")
		return
	}
	if !a.updateAdminPasswordCredential(w, credential, password) {
		return
	}
	identity.MustChangePassword = true
	if _, err := a.store.UpdateAuthIdentity(identity); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update admin identity")
		return
	}
	_ = a.store.RevokeAuthSessionsByIdentityID(identity.ID)
	a.recordAudit(r, auditsvc.Event{Action: "admin.password_reset", EntityType: "admin_identity", EntityID: identity.ID})
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok", "session": "target_revoked"})
}

func (a *App) updateAdminPasswordCredential(w http.ResponseWriter, credential model.AuthCredential, password string) bool {
	hash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimSpace(password)), bcrypt.DefaultCost)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to hash password")
		return false
	}
	credential.SecretHash = string(hash)
	credential.FailedAttempts = 0
	credential.LockedUntil = time.Time{}
	credential.Status = model.StatusActive
	if _, err := a.store.UpdateAuthCredential(credential); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to update credential")
		return false
	}
	return true
}

func (a *App) revokeAdminSessions(w http.ResponseWriter, r *http.Request, id string) {
	identity, _, ok := a.adminIdentityProfile(w, id)
	if !ok {
		return
	}
	if err := a.store.RevokeAuthSessionsByIdentityID(identity.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	a.recordAudit(r, auditsvc.Event{Action: "admin.sessions_revoke", EntityType: "admin_identity", EntityID: identity.ID})
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (a *App) adminIdentityProfile(w http.ResponseWriter, id string) (model.AuthIdentity, model.AdminProfile, bool) {
	identity, err := a.store.GetAuthIdentity(id)
	if err != nil || identity.Kind != model.AuthIdentityKindAdmin {
		httpx.Error(w, http.StatusNotFound, "admin not found")
		return model.AuthIdentity{}, model.AdminProfile{}, false
	}
	profile, err := a.store.GetAdminProfile(identity.ID)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "admin profile not found")
		return model.AuthIdentity{}, model.AdminProfile{}, false
	}
	return identity, profile, true
}

func adminResponse(item model.AdminIdentityProfile) adminIdentityResponse {
	identity := item.Identity
	profile := item.Profile
	return adminIdentityResponse{ID: identity.ID, Login: identity.Login, DisplayName: identity.DisplayName, Role: string(profile.Role), Status: identity.Status, MustChangePassword: identity.MustChangePassword, LastSeenAt: item.LastSeenAt, OnlineUntil: item.OnlineUntil, Online: item.Online, CreatedAt: identity.CreatedAt, UpdatedAt: identity.UpdatedAt}
}

func validAdminRole(role model.AdminRole) bool {
	return role == model.AdminRoleOwner || role == model.AdminRoleAdmin || role == model.AdminRoleViewer
}

func (a *App) preventLastActiveOwner(targetID string, targetRole model.AdminRole, identityStatus model.Status, profileStatus model.Status) error {
	if targetRole == model.AdminRoleOwner && identityStatus == model.StatusActive && profileStatus == model.StatusActive {
		return nil
	}
	activeOwners := 0
	for _, item := range a.store.ListAdminIdentityProfiles() {
		if item.Identity.ID == targetID {
			item.Profile.Role = targetRole
			item.Identity.Status = identityStatus
			item.Profile.Status = profileStatus
		}
		if item.Profile.Role == model.AdminRoleOwner && item.Identity.Status == model.StatusActive && item.Profile.Status == model.StatusActive {
			activeOwners++
		}
	}
	if activeOwners == 0 {
		return errors.New("cannot remove the last active owner")
	}
	return nil
}
