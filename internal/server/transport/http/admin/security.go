package admin

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) sameOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
			httpx.Error(w, http.StatusForbidden, "cross-site admin request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func sameHostHeader(value string, requestHost string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	return hostWithoutPort(parsed.Host) == hostWithoutPort(requestHost)
}

func hostWithoutPort(value string) string {
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(value)
}

func (a *App) permissionGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission := permissionForRequest(r)
		if permission == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := adminauthsvc.Authorize(r.Context(), permission); !ok {
			httpx.Error(w, http.StatusForbidden, "permission denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func permissionForRequest(r *http.Request) adminauthsvc.Permission {
	path := r.URL.Path
	if r.Method == http.MethodGet {
		if path == "/api/v1/audit-logs" {
			return adminauthsvc.PermissionAuditRead
		}
		return adminauthsvc.PermissionAdminRead
	}
	if strings.HasPrefix(path, "/api/v1/accounts/") {
		return adminauthsvc.PermissionUsersWrite
	}
	if path == "/api/v1/accounts" || path == "/api/v1/enrollments" || path == "/api/v1/clients" {
		return adminauthsvc.PermissionUsersWrite
	}
	if strings.HasPrefix(path, "/api/v1/nodes") {
		return adminauthsvc.PermissionNodesWrite
	}
	if strings.HasPrefix(path, "/api/v1/connections") {
		return adminauthsvc.PermissionAccessWrite
	}
	if path == "/api/v1/configs" || path == "/api/v1/config-profiles" {
		return adminauthsvc.PermissionConfigsWrite
	}
	if path == "/api/v1/policy-tags" {
		return adminauthsvc.PermissionUsersWrite
	}
	if path == "/api/v1/tokens" {
		return adminauthsvc.PermissionTokensWrite
	}
	if path == "/api/v1/qr" || strings.HasPrefix(path, "/api/v1/auth/") {
		return adminauthsvc.PermissionAdminRead
	}
	return adminauthsvc.PermissionAdminRead
}
