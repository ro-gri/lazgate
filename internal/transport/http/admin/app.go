package admin

import (
	"net/http"
	"time"

	"laz/internal/model"
	"laz/internal/services/accounts"
	adminauthsvc "laz/internal/services/adminauth"
	auditsvc "laz/internal/services/audit"
	"laz/internal/services/connections"
	"laz/internal/storage"

	"github.com/go-chi/chi/v5"
)

type TokenIssuer func(accountID, clientID string, expiresAt time.Time) (string, model.AccessToken, error)
type AbsoluteURL func(r *http.Request, path string) string
type ConfigProfiles func() []model.ConfigProfile

type Config struct {
	Store                  store.Store
	Accounts               *accounts.Service
	Connections            *connections.Service
	Audit                  *auditsvc.Recorder
	AdminAuth              *adminauthsvc.Authenticator
	WebPrefix              string
	AppName                string
	WebAssets              http.Handler
	AuthMiddleware         func(http.Handler) http.Handler
	GetOrCreateClientToken TokenIssuer
	AbsoluteURL            AbsoluteURL
	ListConfigProfiles     ConfigProfiles
}

type App struct {
	store                  store.Store
	accounts               *accounts.Service
	connections            *connections.Service
	audit                  *auditsvc.Recorder
	adminAuth              *adminauthsvc.Authenticator
	webPrefix              string
	appName                string
	webAssets              http.Handler
	authMiddleware         func(http.Handler) http.Handler
	getOrCreateClientToken TokenIssuer
	absoluteURL            AbsoluteURL
	listConfigProfiles     ConfigProfiles
}

func New(config Config) *App {
	return &App{
		store:                  config.Store,
		accounts:               config.Accounts,
		connections:            config.Connections,
		audit:                  config.Audit,
		adminAuth:              config.AdminAuth,
		webPrefix:              config.WebPrefix,
		appName:                config.AppName,
		webAssets:              config.WebAssets,
		authMiddleware:         config.AuthMiddleware,
		getOrCreateClientToken: config.GetOrCreateClientToken,
		absoluteURL:            config.AbsoluteURL,
		listConfigProfiles:     config.ListConfigProfiles,
	}
}

func (a *App) Mount(router chi.Router) {
	router.HandleFunc(a.webPrefix, a.redirectToWebRoot)
	router.Handle(a.webPrefix+"/assets/*", a.webAssets)
	router.HandleFunc(a.webPrefix+"/*", a.webIndex)
	router.HandleFunc("/api/v1/auth/login", a.authLogin)

	router.Route("/api/v1", func(r chi.Router) {
		r.Use(a.sameOriginGuard)
		r.Use(a.authMiddleware)
		r.Use(a.permissionGuard)
		r.HandleFunc("/auth/me", a.authMe)
		r.HandleFunc("/auth/logout", a.authLogout)
		r.HandleFunc("/audit-logs", a.auditLogs)
		r.HandleFunc("/accounts", a.accountsHandler)
		r.HandleFunc("/accounts/*", a.userSubroutes)
		r.HandleFunc("/enrollments", a.enrollments)
		r.HandleFunc("/nodes", a.nodes)
		r.HandleFunc("/nodes/*", a.nodeSubroutes)
		r.HandleFunc("/clients", a.clientsHandler)
		r.HandleFunc("/connections", a.connectionsHandler)
		r.HandleFunc("/connections/*", a.accessSubroutes)
		r.HandleFunc("/configs", a.configs)
		r.HandleFunc("/config-profiles", a.configProfiles)
		r.HandleFunc("/policy-tags", a.policyTags)
		r.HandleFunc("/tokens", a.tokens)
		r.HandleFunc("/qr", a.adminQR)
	})
}
