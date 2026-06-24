package server

import (
	"net/http"

	adminapp "laz/internal/transport/http/admin"

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
		Audit:       s.audit,
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
