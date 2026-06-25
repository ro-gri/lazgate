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

	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/model"
	"laz/internal/server/storage"
)

func TestAdminRoutePrefixServesLoginAndAssets(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	for _, path := range []string{"/admin/login", "/admin/assets/styles.css"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d: %s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestAdminSubroutesRequireAuthWithWildcardRouter(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/account-1/summary", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminAuthMeReturnsPrincipal(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer secret")
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
	if body.Principal.Name != "admin" || body.Principal.Role != "owner" {
		t.Fatalf("unexpected principal: %+v", body.Principal)
	}
	if len(body.Principal.Permissions) == 0 {
		t.Fatalf("expected permissions in principal: %+v", body.Principal)
	}
}

func TestAdminAuthMeRejectsMissingToken(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminMutatingRoutesRejectCrossSiteOrigin(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/qr", bytes.NewBufferString(`{"value":"x"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAdminLoginSessionAndCSRF(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"token":"secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(loginRR, loginReq)
	if loginRR.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d: %s", loginRR.Code, loginRR.Body.String())
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
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	noCSRFReq := httptest.NewRequest(http.MethodPost, "/api/v1/qr", strings.NewReader(`{"value":"x"}`))
	noCSRFReq.Header.Set("Content-Type", "application/json")
	noCSRFReq.AddCookie(cookies[0])
	noCSRFRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(noCSRFRR, noCSRFReq)
	if noCSRFRR.Code != http.StatusForbidden {
		t.Fatalf("expected missing csrf 403, got %d: %s", noCSRFRR.Code, noCSRFRR.Body.String())
	}

	qrReq := httptest.NewRequest(http.MethodPost, "/api/v1/qr", strings.NewReader(`{"value":"x"}`))
	qrReq.Header.Set("Content-Type", "application/json")
	qrReq.Header.Set("X-CSRF-Token", login.CSRFToken)
	qrReq.AddCookie(cookies[0])
	qrRR := httptest.NewRecorder()
	srv.Routes().ServeHTTP(qrRR, qrReq)
	if qrRR.Code != http.StatusOK {
		t.Fatalf("expected csrf-protected qr 200, got %d: %s", qrRR.Code, qrRR.Body.String())
	}
}

func TestAdminViewerCannotWrite(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, "secret", "", "/admin")
	rawSession := "viewer-session"
	csrf := "viewer-csrf"
	if _, err := st.CreateAdminSession(model.AdminSession{
		Token:         rawSession,
		TokenHash:     testHash(rawSession),
		CSRFToken:     csrf,
		CSRFTokenHash: testHash(csrf),
		PrincipalName: "viewer",
		Role:          string(adminauthsvc.RoleViewer),
		ExpiresAt:     time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", strings.NewReader(`{"username":"qwerty"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(&http.Cookie{Name: "laz_admin_session", Value: rawSession})
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected viewer write 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func testHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func TestClientStaticAssetsUseDedicatedPrefix(t *testing.T) {
	srv := NewServer(nil, "secret", "", "/admin")

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
