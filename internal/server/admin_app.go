package server

import (
	"net/http"

	"laz/internal/server/dashboard"
	adminapp "laz/internal/server/transport/http/admin"

	"github.com/go-chi/chi/v5"
)

type AdminApp struct {
	app *adminapp.App
}

func (s *Server) AdminApp() *AdminApp {
	return &AdminApp{app: adminapp.New(adminapp.Config{
		Store:       s.store,
		Accounts:    s.accounts,
		Connections: s.connections,
		NodeInstall: s.nodeInstall,
		Events:      s.events,
		Audit:       s.audit,
		Dashboard:   dashboard.New(s.store),
		AdminAuth:   s.adminAuth,
		WebPrefix:   s.webPrefix,
		AppName:     s.appName,
		WebAssets:   s.webAssets(),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				s.requireAdminAuth(w, r, next.ServeHTTP)
			})
		},
		GetOrCreateClientToken: s.clientTokens.GetOrCreate,
		AbsoluteURL:            s.absoluteURL,
		ListConfigProfiles:     s.listConfigProfiles,
	})}
}

func (a *AdminApp) Mount(router chi.Router) {
	a.app.Mount(router)
}
