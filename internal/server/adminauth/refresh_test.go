package adminauth

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"laz/internal/server/model"
	commontokens "laz/internal/server/security/tokens"
	store "laz/internal/server/storage"
)

func newRefreshTestAuth(t *testing.T) (*Authenticator, *store.SQLStore) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenSQLiteWithAuth(filepath.Join(dir, "state.db"), filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	auth := New(Config{Store: st, CSRFSecret: "test-csrf"})
	identity, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "admin", DisplayName: "admin", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: identity.ID, Type: model.AuthCredentialTypePassword, SecretHash: mustHashRefreshTestPassword(t, "strong-admin-pass-2"), Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: identity.ID, Role: model.AdminRoleOwner, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	return auth, st
}

func TestAdminRefreshRotatesAndGraceReuseDoesNotRevoke(t *testing.T) {
	auth, st := newRefreshTestAuth(t)
	login, err := auth.LoginWithPassword("admin", "strong-admin-pass-2")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := auth.Refresh(login.RefreshToken, login.CSRFToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.RefreshToken == "" || refreshed.RefreshToken == login.RefreshToken {
		t.Fatal("expected refresh token rotation")
	}
	if _, err := st.GetActiveAuthSessionByRefreshHash(login.Session.RefreshTokenHash); err == nil {
		t.Fatal("old refresh token should not remain active")
	}
	duplicate, err := auth.Refresh(login.RefreshToken, login.CSRFToken)
	if err != nil {
		t.Fatalf("expected duplicate refresh inside grace to be tolerated: %v", err)
	}
	if duplicate.RefreshToken == "" || duplicate.Session.ID != refreshed.Session.ID {
		t.Fatal("expected duplicate refresh inside grace to keep the current session usable")
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(refreshed.Session.TokenHash); err != nil {
		t.Fatal("grace reuse should not revoke session family")
	}
}

func TestAdminRefreshReuseAfterGraceRevokesSession(t *testing.T) {
	auth, st := newRefreshTestAuth(t)
	login, err := auth.LoginWithPassword("admin", "strong-admin-pass-2")
	if err != nil {
		t.Fatal(err)
	}
	newAccess, err := commontokens.New()
	if err != nil {
		t.Fatal(err)
	}
	newRefresh, err := commontokens.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RotateAuthRefreshToken(login.Session.RefreshTokenHash, commontokens.Hash(newAccess), time.Now().UTC().Add(accessTTL), commontokens.Hash(newRefresh), time.Now().UTC().Add(refreshTTL), refreshReuseGrace); err != nil {
		t.Fatal(err)
	}
	time.Sleep(refreshReuseGrace + 100*time.Millisecond)
	if _, err := auth.Refresh(login.RefreshToken, login.CSRFToken); err == nil {
		t.Fatal("expected reuse after grace to fail")
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(login.Session.TokenHash); err == nil {
		t.Fatal("expected refresh token reuse after grace to revoke session")
	}
}

func mustHashRefreshTestPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

func TestAdminLogoutRevokesRefreshTokens(t *testing.T) {
	auth, st := newRefreshTestAuth(t)
	login, err := auth.LoginWithPassword("admin", "strong-admin-pass-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeAuthSessionsByIdentityID(login.Session.IdentityID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetActiveAuthSessionByRefreshHash(login.Session.RefreshTokenHash); err == nil {
		t.Fatal("expected revoked admin session refresh token to be inactive")
	}
}

func TestAdminRefreshTokenHashesAreStored(t *testing.T) {
	auth, _ := newRefreshTestAuth(t)
	login, err := auth.LoginWithPassword("admin", "strong-admin-pass-2")
	if err != nil {
		t.Fatal(err)
	}
	if login.Session.RefreshTokenHash == "" || login.Session.RefreshTokenHash == login.RefreshToken || login.Session.Status != model.StatusActive {
		t.Fatalf("expected hashed active refresh token, got session=%+v raw=%q", login.Session, login.RefreshToken)
	}
}
