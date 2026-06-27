package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	transportstore "laz/internal/nodeproto/transport"
	"laz/internal/server/accounts"
	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/agentcontrol"
	auditsvc "laz/internal/server/audit"
	clientauthsvc "laz/internal/server/clientauth"
	clienttokens "laz/internal/server/clienttokens"
	"laz/internal/server/connections"
	eventsvc "laz/internal/server/events"
	provisioningsvc "laz/internal/server/provisioning"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	subscriptionssvc "laz/internal/server/subscriptions"
	"laz/internal/server/transport/http/httpx"
	"laz/internal/server/workqueue"
)

type Server struct {
	store         store.Store
	transport     transportstore.Store
	accounts      *accounts.Service
	connections   *connections.Service
	subscriptions *subscriptionssvc.Service
	audit         *auditsvc.Recorder
	clientAuth    *clientauthsvc.Service
	clientTokens  *clienttokens.Service
	nodeInstall   *provisioningsvc.Installer
	events        *eventsvc.Bus
	agentControl  *agentcontrol.Hub
	worker        *workqueue.Worker
	adminAuth     *adminauthsvc.Authenticator
	publicBaseURL string
	webPrefix     string
	appName       string
	blankPageHTML string
}

func NewServer(st store.Store, adminToken string, publicBaseURL string, webPrefix string) *Server {
	return NewServerWithTransport(st, transportstore.NopStore{}, adminToken, publicBaseURL, webPrefix)
}

func NewServerWithTransport(st store.Store, transport transportstore.Store, adminToken string, publicBaseURL string, webPrefix string) *Server {
	connectionService := connections.New(st, commontokens.New)
	agentControl := agentcontrol.NewHub(st, transport)
	eventBus := eventsvc.New(st)
	nodeInstall := provisioningsvc.New(st)
	connectionService.SetTransport(transport)
	worker := workqueue.New(transport, eventBus, "server", slog.Default())
	worker.Register(workqueue.TypeNodeAuthRefresh, workqueue.AuthRefreshHandler{Store: st, Refresher: agentControl})
	hysteriaInstallHandler := workqueue.HysteriaInstallHandler{Installer: nodeInstall}
	for _, typ := range []string{
		workqueue.TypeHysteriaInstallConnect,
		workqueue.TypeHysteriaInstallCheckSystem,
		workqueue.TypeHysteriaInstallCreateUser,
		workqueue.TypeHysteriaInstallInstallDetect,
		workqueue.TypeHysteriaInstallWriteConfig,
		workqueue.TypeHysteriaInstallInstallAgent,
		workqueue.TypeHysteriaInstallStartService,
		workqueue.TypeHysteriaInstallVerify,
		workqueue.TypeHysteriaInstallRegisterNode,
		workqueue.TypeHysteriaInstallWaitAgent,
		workqueue.TypeHysteriaInstallDone,
	} {
		worker.Register(typ, hysteriaInstallHandler)
	}
	server := &Server{
		store:         st,
		transport:     transport,
		accounts:      accounts.New(st, connectionService),
		connections:   connectionService,
		subscriptions: subscriptionssvc.New(st),
		audit:         auditsvc.New(st),
		clientAuth:    clientauthsvc.New(st, connectionService),
		clientTokens:  clienttokens.New(st),
		nodeInstall:   nodeInstall,
		events:        eventBus,
		agentControl:  agentControl,
		worker:        worker,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		webPrefix:     normalizeWebPrefix(webPrefix),
		appName:       "laz",
	}
	server.SetAdminAuth(adminToken, "")
	return server
}

func (s *Server) RunWorkers(ctx context.Context) {
	if s.worker != nil {
		go s.worker.Run(ctx)
	}
}

func (s *Server) AgentControl() *agentcontrol.Hub {
	return s.agentControl
}

func (s *Server) SetProvisioningAgentServerCertPEM(pem string) {
	s.nodeInstall.SetAgentServerCertPEM(pem)
}

func (s *Server) SetAppName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "laz"
	}
	s.appName = name
}

func (s *Server) SetBlankPageHTML(html string) {
	s.blankPageHTML = html
}

func (s *Server) SetAdminAuth(token string, tokenSHA256 string) {
	s.adminAuth = adminauthsvc.New(adminauthsvc.Config{
		Store:       s.store,
		Token:       token,
		TokenSHA256: tokenSHA256,
	})
}

func (s *Server) requireAdminAuth(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if r.Header.Get("Authorization") != "" {
		principal, ok := s.adminAuth.AuthenticateRequest(r)
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r.WithContext(adminauthsvc.WithPrincipal(r.Context(), principal)))
		return
	}
	principal, session, ok := s.adminAuth.AuthenticateSessionRequest(r)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if isMutatingAdminMethod(r.Method) && !s.adminAuth.ValidateCSRF(session, r.Header.Get("X-CSRF-Token")) {
		httpx.Error(w, http.StatusForbidden, "csrf token required")
		return
	}
	next(w, r.WithContext(adminauthsvc.WithPrincipal(r.Context(), principal)))
}

func isMutatingAdminMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func normalizeWebPrefix(value string) string {
	value = "/" + strings.Trim(strings.TrimSpace(value), "/")
	if value == "/" {
		return "/admin"
	}
	if strings.HasPrefix(value, "/api/") || strings.HasPrefix(value, "/client/") || strings.HasPrefix(value, "/sub/") || value == "/healthz" || strings.HasPrefix(value, "/assets/") {
		return "/admin"
	}
	return value
}
