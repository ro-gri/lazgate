package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	transportstore "laz/internal/queue"
	"laz/internal/server/accounts"
	adminauthsvc "laz/internal/server/adminauth"
	"laz/internal/server/amneziaagent"
	auditsvc "laz/internal/server/audit"
	clientselfservice "laz/internal/server/clientselfservice"
	clienttokens "laz/internal/server/clienttokens"
	"laz/internal/server/connections"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/hysteriaagent"
	"laz/internal/server/model"
	provisioningsvc "laz/internal/server/provisioning"
	"laz/internal/server/ratelimit"
	commontokens "laz/internal/server/security/tokens"
	store "laz/internal/server/storage"
	subscriptionssvc "laz/internal/server/subscriptions"
	"laz/internal/server/transport/http/httpx"
	"laz/internal/server/workqueue"
)

type Server struct {
	store             store.Store
	transport         transportstore.Store
	accounts          *accounts.Service
	connections       *connections.Service
	subscriptions     *subscriptionssvc.Service
	audit             *auditsvc.Recorder
	clientSelfService *clientselfservice.Service
	clientTokens      *clienttokens.Service
	nodeInstall       *provisioningsvc.Installer
	events            *eventsvc.Bus
	hysteriaAgent     *hysteriaagent.Hub
	amneziaAgent      *amneziaagent.Hub
	worker            *workqueue.Worker
	adminAuth         *adminauthsvc.Authenticator
	publicBaseURL     string
	agentGRPCTarget   string
	agentDownloadBase string
	webPrefix         string
	appName           string
	blankPageHTML     string
	secretKey         string
	rateLimits        ratelimit.Config
}

type Config struct {
	PublicBaseURL     string
	WebPrefix         string
	AgentGRPCTarget   string
	AgentDownloadBase string
	SecretKey         string
}
type Dependencies struct {
	Store     store.Store
	Transport transportstore.Store
	Events    eventsvc.Store
	Completer workqueue.Completer
}

func New(cfg Config, deps Dependencies) (*Server, error) {
	st, transport, eventStore, completer := deps.Store, deps.Transport, deps.Events, deps.Completer
	if transport == nil {
		return nil, errors.New("server transport store is required")
	}
	if eventStore == nil {
		return nil, errors.New("server event store is required")
	}
	if completer == nil {
		return nil, errors.New("server worker completer is required")
	}
	connectionService := connections.New(st, commontokens.New)
	hysteriaAgent := hysteriaagent.NewHub(st, "hysteria2", transport)
	amneziaAgent := amneziaagent.NewHub(st, "amneziawg", transport)
	eventBus := eventsvc.New(eventStore)
	nodeInstall := provisioningsvc.New(st)
	connectionService.SetTransport(transport)
	worker, err := workqueue.New(transport, completer, "server", slog.Default())
	if err != nil {
		return nil, err
	}
	worker.Register(workqueue.TypeNodeAuthRefresh, workqueue.AuthRefreshHandler{Store: st, Refresher: workqueue.AuthRefresherFunc(func(ctx context.Context, nodeID, accountID string, version int64) (int64, error) {
		node, err := st.GetNode(nodeID)
		if err != nil {
			return 0, err
		}
		switch node.Type {
		case model.NodeTypeNativeHy2:
			return hysteriaAgent.RefreshAuthApplied(ctx, nodeID, accountID, version)
		case model.NodeTypeNativeAmneziaWG:
			return amneziaAgent.RefreshAuthApplied(ctx, nodeID, accountID, version)
		default:
			return 0, connections.ErrUnsupportedNodeType
		}
	})})
	hysteriaInstallHandler := workqueue.HysteriaInstallHandler{Installer: nodeInstall}
	for _, typ := range provisioningsvc.AWGSteps {
		worker.Register(typ, workqueue.AWGInstallHandler{Installer: nodeInstall})
	}
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
	hysteriaUninstallHandler := workqueue.HysteriaUninstallHandler{Uninstaller: nodeInstall}
	worker.Register(workqueue.TypeHysteriaUninstallCleanup, hysteriaUninstallHandler)
	worker.Register(workqueue.TypeHysteriaDetachCleanup, hysteriaUninstallHandler)
	server := &Server{
		store:             st,
		transport:         transport,
		accounts:          accounts.New(st, connectionService),
		connections:       connectionService,
		subscriptions:     subscriptionssvc.New(st),
		audit:             auditsvc.New(st),
		clientSelfService: clientselfservice.New(st, connectionService),
		clientTokens:      clienttokens.New(st),
		nodeInstall:       nodeInstall,
		events:            eventBus,
		hysteriaAgent:     hysteriaAgent,
		amneziaAgent:      amneziaAgent,
		worker:            worker,
		publicBaseURL:     strings.TrimRight(cfg.PublicBaseURL, "/"),
		agentGRPCTarget:   strings.TrimSpace(cfg.AgentGRPCTarget),
		agentDownloadBase: strings.TrimRight(strings.TrimSpace(cfg.AgentDownloadBase), "/"),
		webPrefix:         normalizeWebPrefix(cfg.WebPrefix),
		appName:           "laz",
		secretKey:         strings.TrimSpace(cfg.SecretKey),
		rateLimits:        ratelimit.DefaultConfig(),
	}
	server.SetAdminAuth(csrfSecretOrDefault(cfg.SecretKey))
	return server, nil
}

func (s *Server) RunWorkers(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if s.worker != nil {
			s.worker.Run(ctx)
		}
	}()
	return done
}

func (s *Server) SetRateLimits(config ratelimit.Config) {
	s.rateLimits = config.Normalize()
}

func (s *Server) HysteriaAgent() *hysteriaagent.Hub {
	return s.hysteriaAgent
}

func (s *Server) AmneziaAgent() *amneziaagent.Hub {
	return s.amneziaAgent
}

func (s *Server) AgentInstallationStore() store.Store {
	return s.store
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

func (s *Server) SetAdminAuth(csrfSecret string) {
	s.adminAuth = adminauthsvc.New(adminauthsvc.Config{
		Store:      s.store,
		CookiePath: adminCookiePath(s.webPrefix),
		CSRFSecret: csrfSecretOrDefault(csrfSecret),
	})
}

func (s *Server) requireAdminAuth(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
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

func adminCookiePath(webPrefix string) string {
	webPrefix = strings.TrimSpace(webPrefix)
	if webPrefix == "" || webPrefix == "/" {
		return "/"
	}
	return "/" + strings.Trim(webPrefix, "/")
}

func csrfSecretOrDefault(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "laz-admin-csrf-development-secret"
	}
	return secret
}
