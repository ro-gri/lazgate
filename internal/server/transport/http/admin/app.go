package admin

import (
	"net/http"
	"strings"
	"time"

	transportstore "laz/internal/queue"
	"laz/internal/server/accounts"
	adminauthsvc "laz/internal/server/adminauth"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/connections"
	dashboardsvc "laz/internal/server/dashboard"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	provisioningsvc "laz/internal/server/provisioning"
	"laz/internal/server/ratelimit"
	"laz/internal/server/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
)

type PortalTokenIssuer func(accountID string, expiresAt time.Time) (string, model.AccountPortalToken, error)
type SubscriptionTokenIssuer func(connectionID string, expiresAt time.Time) (string, model.ConnectionSubscriptionToken, error)
type AbsoluteURL func(r *http.Request, path string) string
type ConfigProfiles func() []model.ConfigProfile

type Config struct {
	Store                        store.Store
	Transport                    transportstore.Store
	Accounts                     *accounts.Service
	Connections                  *connections.Service
	NodeInstall                  *provisioningsvc.Installer
	Events                       *eventsvc.Bus
	Dashboard                    *dashboardsvc.Service
	Audit                        *auditsvc.Recorder
	AdminAuth                    *adminauthsvc.Authenticator
	WebPrefix                    string
	AppName                      string
	WebAssets                    http.Handler
	AuthMiddleware               func(http.Handler) http.Handler
	GetOrCreatePortalToken       PortalTokenIssuer
	GetOrCreateSubscriptionToken SubscriptionTokenIssuer
	AbsoluteURL                  AbsoluteURL
	ListConfigProfiles           ConfigProfiles
	PublicBaseURL                string
	AgentGRPCTarget              string
	AgentDownloadBase            string
	RateLimits                   ratelimit.Config
}

type App struct {
	store                        store.Store
	transport                    transportstore.Store
	accounts                     *accounts.Service
	connections                  *connections.Service
	nodeInstall                  *provisioningsvc.Installer
	events                       *eventsvc.Bus
	dashboard                    *dashboardsvc.Service
	audit                        *auditsvc.Recorder
	adminAuth                    *adminauthsvc.Authenticator
	webPrefix                    string
	appName                      string
	webAssets                    http.Handler
	authMiddleware               func(http.Handler) http.Handler
	getOrCreatePortalToken       PortalTokenIssuer
	getOrCreateSubscriptionToken SubscriptionTokenIssuer
	absoluteURL                  AbsoluteURL
	listConfigProfiles           ConfigProfiles
	publicBaseURL                string
	agentGRPCTarget              string
	agentDownloadBase            string
	rateLimits                   ratelimit.Config
}

func New(config Config) *App {
	app := &App{
		store:                        config.Store,
		transport:                    config.Transport,
		accounts:                     config.Accounts,
		connections:                  config.Connections,
		nodeInstall:                  config.NodeInstall,
		events:                       config.Events,
		dashboard:                    config.Dashboard,
		audit:                        config.Audit,
		adminAuth:                    config.AdminAuth,
		webPrefix:                    config.WebPrefix,
		appName:                      config.AppName,
		webAssets:                    config.WebAssets,
		authMiddleware:               config.AuthMiddleware,
		getOrCreatePortalToken:       config.GetOrCreatePortalToken,
		getOrCreateSubscriptionToken: config.GetOrCreateSubscriptionToken,
		absoluteURL:                  config.AbsoluteURL,
		listConfigProfiles:           config.ListConfigProfiles,
		publicBaseURL:                strings.TrimRight(strings.TrimSpace(config.PublicBaseURL), "/"),
		agentGRPCTarget:              config.AgentGRPCTarget,
		agentDownloadBase:            config.AgentDownloadBase,
		rateLimits:                   config.RateLimits.Normalize(),
	}
	if invite, created, err := adminauthsvc.EnsureBootstrapOwnerInvite(app.store, func(role model.AdminRole, ttl time.Duration, note string, invitedByID string, invitedByLogin string) (adminauthsvc.BootstrapInvite, error) {
		createdInvite, err := app.createAdminInviteRecord(role, ttl, note, invitedByID, invitedByLogin)
		if err != nil {
			return adminauthsvc.BootstrapInvite{}, err
		}
		return createdInvite.BootstrapInvite(), nil
	}); err == nil && created {
		adminauthsvc.LogBootstrapOwnerInvite(invite, nil)
	}
	return app
}

func (a *App) Mount(router chi.Router) {
	router.HandleFunc(a.webPrefix, a.redirectToWebRoot)
	router.Handle(a.webPrefix+"/assets/*", a.webAssets)
	a.mountAPI(router, a.webPrefix+"/api/v1")
	router.HandleFunc(a.webPrefix+"/*", a.webIndex)
}

func (a *App) mountAPI(router chi.Router, prefix string) {
	router.With(a.limitByIP(a.rateLimits.AdminLoginPerMinute)).HandleFunc(prefix+"/auth/login", a.authLogin)
	router.With(a.limitByIP(a.rateLimits.AdminLoginPerMinute)).HandleFunc(prefix+"/auth/enroll-admin", a.enrollAdmin)
	router.HandleFunc(prefix+"/auth/change-password", a.authChangePassword)
	router.HandleFunc(prefix+"/auth/refresh", a.authRefresh)
	router.Route(prefix, func(r chi.Router) {
		r.Use(a.sameOriginGuard)
		r.Use(a.authMiddleware)
		r.Use(a.permissionGuard)
		r.HandleFunc("/auth/me", a.authMe)
		r.HandleFunc("/auth/logout", a.authLogout)
		r.HandleFunc("/admins", a.adminsHandler)
		r.HandleFunc("/admins/*", a.adminSubroutes)
		r.HandleFunc("/admin-invites", a.adminInvitesHandler)
		r.HandleFunc("/admin-invites/*", a.adminInviteSubroutes)
		r.HandleFunc("/events", a.eventsStream)
		r.HandleFunc("/audit-logs", a.auditLogs)
		r.HandleFunc("/dashboard", a.dashboardHandler)
		r.HandleFunc("/accounts", a.accountsHandler)
		r.HandleFunc("/accounts/*", a.userSubroutes)
		r.HandleFunc("/enrollments", a.enrollments)
		r.HandleFunc("/nodes", a.nodes)
		r.HandleFunc("/nodes/install-hysteria2", a.installHysteria2Node)
		r.HandleFunc("/nodes/install-amneziawg", a.installAWGNode)
		r.With(a.limitByIP(30)).Post("/nodes/amneziawg-signatures", a.generateAWGSignatures)
		r.With(a.limitByIP(10)).Post("/nodes/hysteria-files", a.hysteriaFiles)
		r.HandleFunc("/nodes/attach-hysteria2", a.attachHysteria2Node)
		r.HandleFunc("/nodes/*", a.nodeSubroutes)
		r.HandleFunc("/connections", a.connectionsHandler)
		r.HandleFunc("/connections/*", a.accessSubroutes)
		r.HandleFunc("/configs", a.configs)
		r.HandleFunc("/config-profiles", a.configProfiles)
		r.HandleFunc("/policy-tags", a.policyTags)
		r.HandleFunc("/tokens", a.tokens)
		r.HandleFunc("/qr", a.adminQR)
	})
}

func (a *App) limitByIP(limit int) func(http.Handler) http.Handler {
	if !a.rateLimits.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	return httprate.LimitByIP(limit, time.Minute)
}
