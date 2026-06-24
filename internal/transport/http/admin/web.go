package admin

import (
	"net/http"
	"strings"

	"laz/internal/transport/http/httpx"
	webui "laz/internal/web"
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
	if r.URL.Path != a.webPrefix+"/" && r.URL.Path != a.webPrefix+"/deleted" && !strings.HasPrefix(r.URL.Path, a.webPrefix+"/accounts/") {
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
	body = strings.ReplaceAll(body, "__APP_NAME__", a.appName)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
