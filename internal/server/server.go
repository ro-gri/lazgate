package server

import (
	"net/http"
	"strings"

	"laz/internal/server/accounts"
	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/agentcontrol"
	auditsvc "laz/internal/server/audit"
	clientauthsvc "laz/internal/server/clientauth"
	clienttokens "laz/internal/server/clienttokens"
	"laz/internal/server/connections"
	provisioningsvc "laz/internal/server/provisioning"
	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/storage"
	subscriptionssvc "laz/internal/server/subscriptions"
	"laz/internal/server/transport/http/httpx"
)

type Server struct {
	store         store.Store
	accounts      *accounts.Service
	connections   *connections.Service
	subscriptions *subscriptionssvc.Service
	audit         *auditsvc.Recorder
	clientAuth    *clientauthsvc.Service
	clientTokens  *clienttokens.Service
	nodeInstall   *provisioningsvc.Installer
	agentControl  *agentcontrol.Hub
	adminAuth     *adminauthsvc.Authenticator
	publicBaseURL string
	webPrefix     string
	appName       string
	blankPageHTML string
}

func NewServer(st store.Store, adminToken string, publicBaseURL string, webPrefix string) *Server {
	connectionService := connections.New(st, commontokens.New)
	agentControl := agentcontrol.NewHub(st)
	connectionService.SetAgentControl(agentControl)
	server := &Server{
		store:         st,
		accounts:      accounts.New(st, connectionService),
		connections:   connectionService,
		subscriptions: subscriptionssvc.New(st),
		audit:         auditsvc.New(st),
		clientAuth:    clientauthsvc.New(st, connectionService),
		clientTokens:  clienttokens.New(st),
		nodeInstall:   provisioningsvc.New(st),
		agentControl:  agentControl,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		webPrefix:     normalizeWebPrefix(webPrefix),
		appName:       "laz",
	}
	server.SetAdminAuth(adminToken, "")
	return server
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
