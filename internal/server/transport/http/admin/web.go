package admin

import (
	"encoding/json"
	"laz/internal/protocol/hy2config"
	"net/http"
	"strings"

	"laz/internal/server/provisioning"
	"laz/internal/server/transport/http/httpx"
	webui "laz/internal/server/web"
)

func (a *App) redirectToWebRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != a.webPrefix {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	http.Redirect(w, r, a.webPrefix+"/", http.StatusFound)
}

func (a *App) webIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	loginPath := a.webPrefix + "/login"
	if r.URL.Path == loginPath {
		a.serveWebFile(w, "admin/login.html")
		return
	}
	if strings.HasPrefix(r.URL.Path, a.webPrefix+"/invite/") {
		a.serveWebFile(w, "admin/invite.html")
		return
	}
	allowedPath := r.URL.Path == a.webPrefix+"/" ||
		r.URL.Path == a.webPrefix+"/deleted" ||
		r.URL.Path == a.webPrefix+"/accounts" ||
		r.URL.Path == a.webPrefix+"/accounts/" ||
		strings.HasPrefix(r.URL.Path, a.webPrefix+"/accounts/") ||
		r.URL.Path == a.webPrefix+"/nodes" ||
		r.URL.Path == a.webPrefix+"/nodes/" ||
		strings.HasPrefix(r.URL.Path, a.webPrefix+"/nodes/") ||
		r.URL.Path == a.webPrefix+"/administration" ||
		r.URL.Path == a.webPrefix+"/administration/" ||
		r.URL.Path == a.webPrefix+"/administration/invites" ||
		r.URL.Path == a.webPrefix+"/administration/invites/" ||
		r.URL.Path == a.webPrefix+"/administration/admins" ||
		r.URL.Path == a.webPrefix+"/administration/admins/"
	if !allowedPath {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	a.serveWebFile(w, "admin/index.html")
}

func (a *App) serveWebFile(w http.ResponseWriter, path string) {
	raw, err := webui.AssetsFS.ReadFile(path)
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	body := strings.ReplaceAll(string(raw), "__WEB_PREFIX__", a.webPrefix)
	if strings.Contains(body, "__HYSTERIA_DEFAULTS__") {
		defaults, _ := json.Marshal(hy2config.Defaults())
		body = strings.ReplaceAll(body, "__HYSTERIA_DEFAULTS__", string(defaults))
	}
	if strings.Contains(body, "__COUNTRY_CODES__") {
		codes, err := json.Marshal(provisioning.SupportedCountries())
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		body = strings.ReplaceAll(body, "__COUNTRY_CODES__", string(codes))
	}
	body = strings.ReplaceAll(body, "__APP_NAME__", a.appName)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
