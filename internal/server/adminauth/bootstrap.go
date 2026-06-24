package adminauth

import (
	"fmt"
	"log/slog"
	"time"

	"laz/internal/server/model"
)

type BootstrapInviteStore interface {
	ListAdminIdentityProfiles() []model.AdminIdentityProfile
	FindActiveSystemOwnerInvite(now time.Time) (model.AdminInvite, error)
	RevokeAdminInvite(inviteID string) (model.AdminInvite, error)
}

type BootstrapInviteCreator func(role model.AdminRole, ttl time.Duration, note string, invitedByID string, invitedByLogin string) (BootstrapInvite, error)

type BootstrapInvite struct {
	ID        string
	URL       string
	Code      string
	ExpiresAt time.Time
}

func EnsureBootstrapOwnerInvite(store BootstrapInviteStore, create BootstrapInviteCreator) (BootstrapInvite, bool, error) {
	if store == nil || create == nil || activeOwnerExists(store.ListAdminIdentityProfiles()) {
		return BootstrapInvite{}, false, nil
	}
	if existing, err := store.FindActiveSystemOwnerInvite(time.Now().UTC()); err == nil {
		_, _ = store.RevokeAdminInvite(existing.ID)
	}
	invite, err := create(model.AdminRoleOwner, 24*time.Hour, "bootstrap owner invite", "", "system")
	if err != nil {
		return BootstrapInvite{}, false, err
	}
	return invite, true, nil
}

func LogBootstrapOwnerInvite(invite BootstrapInvite, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("bootstrap owner invite created", "url", invite.URL, "code", invite.Code, "expires_at", invite.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("LazGate bootstrap owner invite: %s\nInvite code: %s\nExpires at: %s\n", invite.URL, invite.Code, invite.ExpiresAt.Format(time.RFC3339))
}

func activeOwnerExists(items []model.AdminIdentityProfile) bool {
	for _, item := range items {
		if item.Identity.Status == model.StatusActive && item.Profile.Status == model.StatusActive && item.Profile.Role == model.AdminRoleOwner {
			return true
		}
	}
	return false
}
