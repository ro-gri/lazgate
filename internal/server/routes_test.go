package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"laz/internal/server/model"
	"laz/internal/server/storage"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminRoutePrefixServesLoginAndAssets(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	for _, path := range []string{"/admin/login", "/admin/assets/styles.css", "/admin/assets/app.js", "/admin/assets/install-progress.js", "/admin/assets/server-fields.js", "/admin/assets/ipaddr.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestAdminManagementPageRouteServesEmbeddedApp(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/administration/invites", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "__COUNTRY_CODES__") || !strings.Contains(body, `id="country-codes"`) || !strings.Contains(body, `"FI"`) {
		t.Fatal("admin page must include the country catalog")
	}
	if !strings.Contains(body, "Администрирование") || !strings.Contains(body, "/admin/administration/invites") || !strings.Contains(body, "/admin/assets/app.js") {
		t.Fatalf("expected administration navigation and app asset, got %q", body)
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/admin/admins", nil)
	legacyRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(legacyRR, legacyReq)
	if legacyRR.Code != http.StatusNotFound {
		t.Fatalf("expected legacy admins route 404, got %d: %s", legacyRR.Code, legacyRR.Body.String())
	}
}

func TestLegacyRootAdminAPIReturnsNotFound(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/account-1/summary", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPrefixedAdminAPIRequiresSessionAndLegacyRootAPIIsRemoved(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin-local"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin-local/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected prefixed API 401 without session, got %d: %s", rr.Code, rr.Body.String())
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	legacyRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(legacyRR, legacyReq)
	if legacyRR.Code != http.StatusNotFound {
		t.Fatalf("expected legacy root API 404, got %d: %s", legacyRR.Code, legacyRR.Body.String())
	}
}

func TestAdminFrontendUsesPrefixedAPI(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin-local"}, nil)

	for _, path := range []string{"/admin-local/login", "/admin-local/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", path, rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, `data-web-prefix="/admin-local"`) {
			t.Fatalf("expected web prefix in %s html, got %q", path, body)
		}
	}
}

func TestAdminAuthMeReturnsPrincipal(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	_, rawSession := createRouteTestAdminSession(t, srv, st, "owner", model.AdminRoleOwner)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Principal struct {
			Name        string   `json:"name"`
			Role        string   `json:"role"`
			Permissions []string `json:"permissions"`
		} `json:"principal"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Principal.Name != "owner" || body.Principal.Role != "owner" {
		t.Fatalf("unexpected principal: %+v", body.Principal)
	}
	if len(body.Principal.Permissions) == 0 {
		t.Fatalf("expected permissions in principal: %+v", body.Principal)
	}
}

func TestAdminAuthMeRejectsMissingToken(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminMutatingRoutesRejectCrossSiteOrigin(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	_, rawSession := createRouteTestAdminSession(t, srv, st, "owner", model.AdminRoleOwner)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/qr", bytes.NewBufferString(`{"value":"x"}`))
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: rawSession})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminLoginSessionAndCSRF(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	createRouteTestAdmin(t, st, "owner", model.AdminRoleOwner, "owner-secure-password1!")

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/login", strings.NewReader(`{"login":"owner","password":"owner-secure-password1!"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected normal login 200, got %d: %s", loginRR.Code, loginRR.Body.String())
	}
	var login struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(loginRR.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}
	if login.CSRFToken == "" {
		t.Fatal("expected csrf token")
	}
	cookies := loginRR.Result().Cookies()
	accessCookie := findRouteTestCookie(cookies, srv.adminAuth.CookieName())
	refreshCookie := findRouteTestCookie(cookies, srv.adminAuth.RefreshCookieName())
	if accessCookie == nil || refreshCookie == nil {
		t.Fatalf("expected login access and refresh cookies, got %+v", cookies)
	}
	if accessCookie.Path != "/admin" || accessCookie.MaxAge != 600 || refreshCookie.MaxAge < 2390 || refreshCookie.MaxAge > 2400 {
		t.Fatalf("unexpected login cookie settings access=%+v refresh=%+v", accessCookie, refreshCookie)
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/refresh", nil)
	refreshReq.Header.Set("X-CSRF-Token", login.CSRFToken)
	refreshReq.AddCookie(accessCookie)
	refreshReq.AddCookie(refreshCookie)
	refreshRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(refreshRR, refreshReq)
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("expected refresh 200, got %d: %s", refreshRR.Code, refreshRR.Body.String())
	}
	var refresh struct {
		CSRFToken        string `json:"csrf_token"`
		AccessExpiresAt  string `json:"access_expires_at"`
		RefreshExpiresAt string `json:"refresh_expires_at"`
		RefreshAfter     string `json:"refresh_after"`
		ServerTime       string `json:"server_time"`
	}
	if err := json.NewDecoder(refreshRR.Body).Decode(&refresh); err != nil {
		t.Fatal(err)
	}
	if refresh.CSRFToken == "" || refresh.AccessExpiresAt == "" || refresh.RefreshExpiresAt == "" || refresh.RefreshAfter == "" || refresh.ServerTime == "" {
		t.Fatalf("expected refresh timings and csrf, got %+v", refresh)
	}
	rotatedAccessCookie := findRouteTestCookie(refreshRR.Result().Cookies(), srv.adminAuth.CookieName())
	rotatedRefreshCookie := findRouteTestCookie(refreshRR.Result().Cookies(), srv.adminAuth.RefreshCookieName())
	if rotatedAccessCookie == nil || rotatedRefreshCookie == nil || rotatedRefreshCookie.Value == refreshCookie.Value {
		t.Fatalf("expected rotated admin cookies, got access=%+v refresh=%+v", rotatedAccessCookie, rotatedRefreshCookie)
	}
	invalidRefreshReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/refresh", nil)
	invalidRefreshReq.AddCookie(rotatedAccessCookie)
	invalidRefreshReq.AddCookie(rotatedRefreshCookie)
	invalidRefreshRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(invalidRefreshRR, invalidRefreshReq)
	if invalidRefreshRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing csrf refresh rejection, got %d: %s", invalidRefreshRR.Code, invalidRefreshRR.Body.String())
	}

	noCSRFReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/qr", strings.NewReader(`{"value":"x"}`))
	noCSRFReq.Header.Set("Content-Type", "application/json")
	noCSRFReq.AddCookie(rotatedAccessCookie)
	noCSRFRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(noCSRFRR, noCSRFReq)
	if noCSRFRR.Code != http.StatusForbidden {
		t.Fatalf("expected missing csrf 403, got %d: %s", noCSRFRR.Code, noCSRFRR.Body.String())
	}

	qrReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/qr", strings.NewReader(`{"value":"x"}`))
	qrReq.Header.Set("Content-Type", "application/json")
	qrReq.Header.Set("X-CSRF-Token", refresh.CSRFToken)
	qrReq.AddCookie(rotatedAccessCookie)
	qrRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(qrRR, qrReq)
	if qrRR.Code != http.StatusOK {
		t.Fatalf("expected csrf-protected qr 200, got %d: %s", qrRR.Code, qrRR.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/logout", nil)
	logoutReq.Header.Set("X-CSRF-Token", refresh.CSRFToken)
	logoutReq.AddCookie(rotatedAccessCookie)
	logoutReq.AddCookie(rotatedRefreshCookie)
	logoutRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(logoutRR, logoutReq)
	if logoutRR.Code != http.StatusOK {
		t.Fatalf("expected logout 200, got %d: %s", logoutRR.Code, logoutRR.Body.String())
	}
	logoutCookies := logoutRR.Result().Cookies()
	if len(logoutCookies) == 0 {
		t.Fatal("expected logout to clear session cookie")
	}
	clearedAccessCookie := findRouteTestCookie(logoutCookies, srv.adminAuth.CookieName())
	clearedRefreshCookie := findRouteTestCookie(logoutCookies, srv.adminAuth.RefreshCookieName())
	if clearedAccessCookie == nil || clearedRefreshCookie == nil || clearedAccessCookie.Path != rotatedAccessCookie.Path || clearedRefreshCookie.Path != rotatedRefreshCookie.Path || clearedAccessCookie.MaxAge != -1 || clearedRefreshCookie.MaxAge != -1 {
		t.Fatalf("expected logout to clear access and refresh cookies, got %+v", logoutCookies)
	}
}

func findRouteTestCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestAdminViewerCannotWrite(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	viewer, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "viewer", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: viewer.ID, Type: model.AuthCredentialTypePassword, SecretHash: "unused", Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: viewer.ID, Role: model.AdminRoleViewer, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	rawSession := "viewer-session"
	createdSession, err := st.CreateAuthSession(model.AuthSession{IdentityID: viewer.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	csrf := srv.adminAuth.NewCSRFToken(createdSession.ID)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/accounts", strings.NewReader(`{"username":"qwerty"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected viewer write 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminManagementOwnerOnlyAndInviteEnrollment(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "http://example.test", WebPrefix: "/admin"}, st)

	viewer := createRouteTestAdmin(t, st, "viewer", model.AdminRoleViewer, "viewer-secure-password1!")
	viewerRawSession := "viewer-admins-session"
	viewerSession, err := st.CreateAuthSession(model.AuthSession{IdentityID: viewer.ID, TokenHash: testHash(viewerRawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/admin/api/v1/admins", "/admin/api/v1/admin-invites"} {
		viewerReq := httptest.NewRequest(http.MethodGet, path, nil)
		viewerReq.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(viewerSession.ID))
		viewerReq.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: viewerRawSession})
		viewerRR := httptest.NewRecorder()
		srv.Routes().ServeHTTP(viewerRR, viewerReq)
		if viewerRR.Code != http.StatusForbidden {
			t.Fatalf("expected viewer %s 403, got %d: %s", path, viewerRR.Code, viewerRR.Body.String())
		}
	}

	ownerSession, ownerRawSession := createRouteTestAdminSession(t, srv, st, "owner", model.AdminRoleOwner)
	createAdmin := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admins", strings.NewReader(`{"login":"ops","display_name":"Ops Admin","role":"admin","password":"ops-secure-password1!"}`))
	createAdmin.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	createAdmin.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	createAdmin.Header.Set("Content-Type", "application/json")
	createAdminRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createAdminRR, createAdmin)
	if createAdminRR.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected direct create admin 405, got %d: %s", createAdminRR.Code, createAdminRR.Body.String())
	}

	createInvite := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admin-invites", strings.NewReader(`{"role":"viewer","ttl":"24h","note":"readonly"}`))
	createInvite.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	createInvite.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	createInvite.Header.Set("Content-Type", "application/json")
	createInviteRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(createInviteRR, createInvite)
	if createInviteRR.Code != http.StatusCreated {
		t.Fatalf("expected create invite 201, got %d: %s", createInviteRR.Code, createInviteRR.Body.String())
	}
	var inviteBody struct {
		InviteURL  string `json:"invite_url"`
		InviteCode string `json:"invite_code"`
		Invite     struct {
			ID string `json:"id"`
		} `json:"invite"`
	}
	if err := json.NewDecoder(createInviteRR.Body).Decode(&inviteBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inviteBody.InviteURL, "/admin/invite/") || inviteBody.InviteCode == "" {
		t.Fatalf("expected one-time invite url and code, got %+v", inviteBody)
	}
	inviteToken := inviteBody.InviteURL[strings.LastIndex(inviteBody.InviteURL, "/")+1:]
	enroll := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/enroll-admin", strings.NewReader(fmt.Sprintf(`{"invite_token":%q,"invite_code":%q,"login":"readonly","display_name":"Read Only","password":"viewer-secure-password1!"}`, inviteToken, inviteBody.InviteCode)))
	enroll.Header.Set("Content-Type", "application/json")
	enrollRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(enrollRR, enroll)
	if enrollRR.Code != http.StatusOK {
		t.Fatalf("expected enroll 200, got %d: %s", enrollRR.Code, enrollRR.Body.String())
	}
	readonly, err := st.FindActiveAdminIdentityByLogin("readonly")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := st.GetAdminProfile(readonly.ID)
	if err != nil || profile.Role != model.AdminRoleViewer {
		t.Fatalf("expected viewer profile, got profile=%+v err=%v", profile, err)
	}
	used, err := st.GetAdminInvite(inviteBody.Invite.ID)
	if err != nil || used.Status != model.AdminInviteStatusUsed {
		t.Fatalf("expected used invite, got invite=%+v err=%v", used, err)
	}
	listInvites := httptest.NewRequest(http.MethodGet, "/admin/api/v1/admin-invites", nil)
	listInvites.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	listInvites.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	listInvitesRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(listInvitesRR, listInvites)
	if listInvitesRR.Code != http.StatusOK {
		t.Fatalf("expected list invites 200, got %d: %s", listInvitesRR.Code, listInvitesRR.Body.String())
	}
	var listInvitesBody struct {
		Invites []struct {
			ID                string `json:"id"`
			UsedByLogin       string `json:"used_by_login"`
			UsedByDisplayName string `json:"used_by_display_name"`
		} `json:"invites"`
	}
	if err := json.NewDecoder(listInvitesRR.Body).Decode(&listInvitesBody); err != nil {
		t.Fatal(err)
	}
	foundUsedInvite := false
	for _, invite := range listInvitesBody.Invites {
		if invite.ID == inviteBody.Invite.ID {
			foundUsedInvite = invite.UsedByLogin == "readonly" && invite.UsedByDisplayName == "Read Only"
			break
		}
	}
	if !foundUsedInvite {
		t.Fatalf("expected used admin login/name in invite list, got %+v", listInvitesBody.Invites)
	}
	reuse := httptest.NewRequest(http.MethodPost, "/admin/api/v1/auth/enroll-admin", strings.NewReader(fmt.Sprintf(`{"invite_token":%q,"invite_code":%q,"login":"again","password":"again-secure-password1!"}`, inviteToken, inviteBody.InviteCode)))
	reuse.Header.Set("Content-Type", "application/json")
	reuseRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(reuseRR, reuse)
	if reuseRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected invite reuse 401, got %d: %s", reuseRR.Code, reuseRR.Body.String())
	}
}

func TestAdminManagementListIncludesPresence(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	owner, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "owner", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: owner.ID, Role: model.AdminRoleOwner, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateAuthSession(model.AuthSession{IdentityID: owner.ID, TokenHash: testHash("owner-presence-raw"), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	lastSeen := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	onlineUntil := time.Now().UTC().Add(time.Minute).Truncate(time.Millisecond)
	if _, err := st.UpdateAuthSessionPresence(session.ID, lastSeen, onlineUntil); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/admins", nil)
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: "owner-presence-raw"})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected admins 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Admins []struct {
			ID          string    `json:"id"`
			LastSeenAt  time.Time `json:"last_seen_at"`
			OnlineUntil time.Time `json:"online_until"`
			Online      bool      `json:"online"`
		} `json:"admins"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var got *struct {
		ID          string    `json:"id"`
		LastSeenAt  time.Time `json:"last_seen_at"`
		OnlineUntil time.Time `json:"online_until"`
		Online      bool      `json:"online"`
	}
	for i := range body.Admins {
		if body.Admins[i].ID == owner.ID {
			got = &body.Admins[i]
		}
	}
	if got == nil {
		t.Fatalf("expected owner in response, got %+v", body.Admins)
	}
	if !got.LastSeenAt.Equal(lastSeen) || !got.OnlineUntil.Equal(onlineUntil) || !got.Online {
		t.Fatalf("expected presence in response, got %+v", got)
	}
}

func TestAdminManagementRoleChangeRevokesSessions(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	admin, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "ops", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: admin.ID, Role: model.AdminRoleAdmin, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	rawSession := "ops-session"
	if _, err := st.CreateAuthSession(model.AuthSession{IdentityID: admin.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	ownerSession, ownerRawSession := createRouteTestAdminSession(t, srv, st, "role-owner", model.AdminRoleOwner)
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/v1/admins/"+admin.ID, strings.NewReader(`{"role":"viewer"}`))
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected role update 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(testHash(rawSession)); err == nil {
		t.Fatal("expected role change to revoke admin sessions")
	}
}

func TestAdminManagementSelfPasswordChangeSucceedsAndKeepsCurrentSession(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	hash, err := bcrypt.GenerateFromPassword([]byte("old-secure-password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "ops", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: admin.ID, Role: model.AdminRoleAdmin, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: admin.ID, Type: model.AuthCredentialTypePassword, SecretHash: string(hash), Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	currentRawSession := "self-current-session"
	currentSession, err := st.CreateAuthSession(model.AuthSession{IdentityID: admin.ID, TokenHash: testHash(currentRawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	otherRawSession := "self-other-session"
	if _, err := st.CreateAuthSession(model.AuthSession{IdentityID: admin.ID, TokenHash: testHash(otherRawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admins/"+admin.ID+"/reset-password", strings.NewReader(`{"current_password":"old-secure-password1!","new_password":"new-secure-password1!"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(currentSession.ID))
	req.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: currentRawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected self change password 200, got %d: %s", rr.Code, rr.Body.String())
	}
	updated, err := st.GetAuthIdentity(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MustChangePassword {
		t.Fatal("expected self password change to clear must_change_password")
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(testHash(currentRawSession)); err != nil {
		t.Fatalf("expected current session to remain active: %v", err)
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(testHash(otherRawSession)); err == nil {
		t.Fatal("expected self password change to revoke other sessions")
	}
	credential, err := st.GetAuthCredential(admin.ID, model.AuthCredentialTypePassword)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte("new-secure-password1!")) != nil {
		t.Fatal("expected credential to accept new password")
	}
}

func TestAdminManagementSelfPasswordChangeRejectsWrongCurrentPassword(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	hash, err := bcrypt.GenerateFromPassword([]byte("old-secure-password1!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "ops", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: admin.ID, Role: model.AdminRoleAdmin, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: admin.ID, Type: model.AuthCredentialTypePassword, SecretHash: string(hash), Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	rawSession := "wrong-current-session"
	session, err := st.CreateAuthSession(model.AuthSession{IdentityID: admin.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admins/"+admin.ID+"/reset-password", strings.NewReader(`{"current_password":"wrong-password","new_password":"new-secure-password1!"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(session.ID))
	req.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong current password 401, got %d: %s", rr.Code, rr.Body.String())
	}
	credential, err := st.GetAuthCredential(admin.ID, model.AuthCredentialTypePassword)
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(credential.SecretHash), []byte("old-secure-password1!")) != nil {
		t.Fatal("expected credential to keep old password")
	}
}

func TestAdminManagementResetPasswordForcesChangeAndRevokesSessions(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	admin, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "ops", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: admin.ID, Role: model.AdminRoleAdmin, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: admin.ID, Type: model.AuthCredentialTypePassword, SecretHash: "old", Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	rawSession := "reset-session"
	if _, err := st.CreateAuthSession(model.AuthSession{IdentityID: admin.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	ownerSession, ownerRawSession := createRouteTestAdminSession(t, srv, st, "reset-owner", model.AdminRoleOwner)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admins/"+admin.ID+"/reset-password", strings.NewReader(`{"password":"fresh-secure-password1!"}`))
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected reset password 200, got %d: %s", rr.Code, rr.Body.String())
	}
	updated, err := st.GetAuthIdentity(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.MustChangePassword {
		t.Fatal("expected password reset to set must_change_password")
	}
	if _, err := st.GetActiveAuthSessionByTokenHash(testHash(rawSession)); err == nil {
		t.Fatal("expected password reset to revoke admin sessions")
	}
}

func TestAdminManagementNonOwnerCannotResetOthers(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	viewer, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "viewer", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: viewer.ID, Role: model.AdminRoleViewer, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	target, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: "target", Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: target.ID, Role: model.AdminRoleAdmin, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	rawSession := "viewer-reset-session"
	session, err := st.CreateAuthSession(model.AuthSession{IdentityID: viewer.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/admins/"+target.ID+"/reset-password", strings.NewReader(`{"password":"fresh-secure-password1!"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(session.ID))
	req.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected non-owner reset other admin 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminManagementProtectsLastActiveOwner(t *testing.T) {
	st := newRouteTestStore(t)
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, st)
	ownerSession, ownerRawSession := createRouteTestAdminSession(t, srv, st, "owner", model.AdminRoleOwner)
	owner, err := st.FindActiveAdminIdentityByLogin("owner")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/admin/api/v1/admins/"+owner.ID, strings.NewReader(`{"role":"admin"}`))
	req.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	req.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected last owner demotion 400, got %d: %s", rr.Code, rr.Body.String())
	}

	disableReq := httptest.NewRequest(http.MethodPatch, "/admin/api/v1/admins/"+owner.ID, strings.NewReader(`{"status":"deleted"}`))
	disableReq.Header.Set("X-CSRF-Token", srv.adminAuth.NewCSRFToken(ownerSession.ID))
	disableReq.AddCookie(&http.Cookie{Name: srv.adminAuth.CookieName(), Value: ownerRawSession})
	disableReq.Header.Set("Content-Type", "application/json")
	disableRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(disableRR, disableReq)
	if disableRR.Code != http.StatusBadRequest {
		t.Fatalf("expected last owner disable 400, got %d: %s", disableRR.Code, disableRR.Body.String())
	}
}

func newRouteTestStore(t *testing.T) *store.SQLStore {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenSQLiteWithAuth(dir+"/state.db", dir+"/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func createRouteTestAdminSession(t *testing.T, srv *Server, st *store.SQLStore, login string, role model.AdminRole) (model.AuthSession, string) {
	t.Helper()
	if st == nil {
		st = newRouteTestStore(t)
		srv.store = st
		srv.SetAdminAuth(srv.secretKey)
	}
	identity := createRouteTestAdmin(t, st, login, role, login+"-secure-password1!")
	rawSession := login + "-session"
	session, err := st.CreateAuthSession(model.AuthSession{IdentityID: identity.ID, TokenHash: testHash(rawSession), Status: model.StatusActive, ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return session, rawSession
}

func createRouteTestAdmin(t *testing.T, st *store.SQLStore, login string, role model.AdminRole, password string) model.AuthIdentity {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := st.CreateAuthIdentity(model.AuthIdentity{Kind: model.AuthIdentityKindAdmin, Login: login, DisplayName: login, Status: model.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAuthCredential(model.AuthCredential{IdentityID: identity.ID, Type: model.AuthCredentialTypePassword, SecretHash: string(hash), Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAdminProfile(model.AdminProfile{IdentityID: identity.ID, Role: role, Status: model.StatusActive}); err != nil {
		t.Fatal(err)
	}
	return identity
}

func testHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func TestClientPageRouteServesConnectPage(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/client/test-token", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("expected html content type, got %q", got)
	}
	if body := rr.Body.String(); !strings.Contains(body, "test-token") {
		t.Fatalf("expected response body to contain token, got %q", body)
	}
}

func TestClientV1RoutesAreNotCapturedByClientPageRoute(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/client/v1/configs", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected API route 405 for GET /client/v1/configs, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); strings.Contains(got, "text/html") {
		t.Fatalf("expected API route, got page content type %q", got)
	}
}

func TestClientStaticAssetsUseDedicatedPrefix(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/connect-assets/connect.js", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected javascript content type, got %q", got)
	}
}

func TestFrontendAssetsContainTimerRefreshAndVisibilityHandlers(t *testing.T) {
	srv := newTestServer(t, Config{PublicBaseURL: "", WebPrefix: "/admin"}, nil)
	cases := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "admin app",
			path: "/admin/assets/app.js",
			want: []string{
				"/api/v1/auth/refresh",
				"setTimeout",
				"refresh_after",
				"visibilitychange",
				"pagehide",
				"pageshow",
				"stopEvents()",
			},
		},
		{
			name: "client app",
			path: "/connect-assets/connect.js",
			want: []string{
				"/client/v1/auth/refresh",
				"setTimeout",
				"refresh_after",
				"visibilitychange",
				"pagehide",
				"pageshow",
				"stopClientEvents()",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()
			srv.Routes().ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("expected %s to contain %q", tc.path, want)
				}
			}
		})
	}
}
