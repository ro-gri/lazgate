package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"laz/internal/server/accounts"
	adminauthsvc "laz/internal/server/adminauth"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/connections"
	dashboardsvc "laz/internal/server/dashboard"
	"laz/internal/server/hysteriasettings"
	"laz/internal/server/model"
	provisioningsvc "laz/internal/server/provisioning"
	"laz/internal/server/storage"
	adminview "laz/internal/server/transport/http/admin/view"
	"laz/internal/server/transport/http/httpx"
	"laz/internal/server/workqueue"
)

func (a *App) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.dashboard == nil {
		httpx.Error(w, http.StatusBadRequest, "dashboard is not configured")
		return
	}
	query := r.URL.Query()
	fromMS, _ := strconv.ParseInt(query.Get("from_ms"), 10, 64)
	toMS, _ := strconv.ParseInt(query.Get("to_ms"), 10, 64)
	limit, _ := strconv.Atoi(query.Get("limit"))
	httpx.JSON(w, http.StatusOK, a.dashboard.Build(dashboardsvc.Request{
		FromMS: fromMS,
		ToMS:   toMS,
		Bucket: query.Get("bucket"),
		Limit:  limit,
	}))
}

func (a *App) authMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, ok := adminauthsvc.PrincipalFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"principal": principal})
}

func (a *App) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
		httpx.Error(w, http.StatusForbidden, "cross-site admin request rejected")
		return
	}
	var input struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.adminAuth.LoginWithPassword(input.Login, input.Password)
	if errors.Is(err, adminauthsvc.ErrPasswordChangeRequired) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"must_change_password":  true,
			"password_change_token": result.PasswordChangeToken,
		})
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	setAdminAuthCookies(w, r, a.adminAuth.NewSessionCookie(result.SessionToken), a.adminAuth.NewRefreshCookie(result.RefreshToken, result.Session.RefreshExpiresAt))
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), result.Principal)), auditsvc.Event{
		Action:     "admin.login",
		EntityType: "admin_session",
		EntityID:   result.Session.ID,
		Details:    map[string]any{"role": result.Principal.Role},
	})
	httpx.JSON(w, http.StatusOK, adminAuthPayload(a.adminAuth, result))
}

func (a *App) authChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
		httpx.Error(w, http.StatusForbidden, "cross-site admin request rejected")
		return
	}
	var input struct {
		PasswordChangeToken string `json:"password_change_token"`
		NewPassword         string `json:"new_password"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.adminAuth.ChangePassword(input.PasswordChangeToken, input.NewPassword)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	setAdminAuthCookies(w, r, a.adminAuth.NewSessionCookie(result.SessionToken), a.adminAuth.NewRefreshCookie(result.RefreshToken, result.Session.RefreshExpiresAt))
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), result.Principal)), auditsvc.Event{
		Action:     "admin.password_change",
		EntityType: "admin_identity",
		EntityID:   result.Session.IdentityID,
		Details:    map[string]any{"role": result.Principal.Role},
	})
	httpx.JSON(w, http.StatusOK, adminAuthPayload(a.adminAuth, result))
}

func (a *App) authRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameHostHeader(r.Header.Get("Origin"), r.Host) || !sameHostHeader(r.Header.Get("Referer"), r.Host) {
		httpx.Error(w, http.StatusForbidden, "cross-site admin request rejected")
		return
	}
	cookie, err := r.Cookie(a.adminAuth.RefreshCookieName())
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		httpx.Error(w, http.StatusUnauthorized, "refresh token required")
		return
	}
	csrfToken := r.Header.Get("X-CSRF-Token")
	result, err := a.adminAuth.Refresh(cookie.Value, csrfToken)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	setAdminAuthCookies(w, r, a.adminAuth.NewSessionCookie(result.SessionToken), a.adminAuth.NewRefreshCookie(result.RefreshToken, result.Session.RefreshExpiresAt))
	httpx.JSON(w, http.StatusOK, adminAuthPayload(a.adminAuth, result))
}

func (a *App) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, _ := adminauthsvc.PrincipalFromContext(r.Context())
	_ = a.adminAuth.Logout(r)
	setAdminAuthCookies(w, r, a.adminAuth.ExpiredSessionCookie(), a.adminAuth.ExpiredRefreshCookie())
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), principal)), auditsvc.Event{
		Action:     "admin.logout",
		EntityType: "admin_session",
		Details:    map[string]any{"role": principal.Role},
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func setAdminAuthCookies(w http.ResponseWriter, r *http.Request, cookies ...*http.Cookie) {
	for _, cookie := range cookies {
		cookie.Secure = requestIsHTTPS(r)
		http.SetCookie(w, cookie)
	}
}

func adminAuthPayload(auth *adminauthsvc.Authenticator, result adminauthsvc.LoginResult) map[string]any {
	payload := auth.Timings(result.Session)
	payload["principal"] = result.Principal
	payload["csrf_token"] = result.CSRFToken
	return payload
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *App) adminQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Value string `json:"value"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	httpx.QRCodePNG(w, input.Value)
}

func (a *App) accountsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := model.AccountStatus(strings.TrimSpace(r.URL.Query().Get("status")))
		accounts := adminview.FilterAccounts(a.store.ListAccounts(), status)
		if r.URL.Query().Get("include") == "summary" {
			summaries := []adminview.AccountSummary{}
			for _, account := range accounts {
				summary, err := a.store.Summary(account.ID)
				if err != nil {
					httpx.StoreError(w, err)
					return
				}
				summaries = append(summaries, adminview.Summary(summary))
			}
			httpx.JSON(w, http.StatusOK, map[string]any{"items": summaries})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.Accounts(accounts)})
	case http.MethodPost:
		var input struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		createInput := accounts.CreateAccountInput{
			Username:    input.Username,
			DisplayName: input.DisplayName,
		}
		if err := createInput.Validate(); err != nil {
			httpx.ValidationError(w, err)
			return
		}
		u, err := a.accounts.CreateAccount(createInput)
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "accounts.create",
			EntityType: "account",
			EntityID:   u.ID,
			Details:    map[string]any{"username": u.Username, "display_name": u.DisplayName},
		})
		a.publish(r.Context(), model.Event{
			Type:        "account.created",
			EntityType:  "account",
			EntityID:    u.ID,
			Actor:       "admin",
			Message:     "Аккаунт " + accountLabel(u, u.ID) + " создан.",
			PayloadJSON: jsonString(map[string]any{"account": adminview.AccountItem(u)}),
		})
		httpx.JSON(w, http.StatusCreated, adminview.AccountItem(u))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) userSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, a.webPrefix+"/api/v1/accounts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "hold":
			a.setAccountRemoteStatus(w, r, parts[0], model.AccountStatusHeld, "disabled")
			return
		case "resume":
			a.setAccountRemoteStatus(w, r, parts[0], model.AccountStatusActive, "active")
			return
		case "delete":
			a.deleteRemoteAccount(w, r, parts[0])
			return
		}
	}
	if len(parts) == 2 && parts[1] == "summary" && r.Method == http.MethodGet {
		summary, err := a.store.Summary(parts[0])
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, adminview.Summary(summary))
		return
	}
	if len(parts) == 2 && parts[1] == "deleted-connections" && r.Method == http.MethodGet {
		a.deletedConnectionsForAccount(w, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "policy-tags" {
		a.accountPolicyTags(w, r, parts[0])
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (a *App) enrollments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Username       string   `json:"username"`
		DisplayName    string   `json:"display_name"`
		ConnectionName string   `json:"connection_name"`
		NodeIDs        []string `json:"node_ids"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	enrollment, err := a.accounts.Enroll(r.Context(), accounts.EnrollmentInput{
		Username:       input.Username,
		DisplayName:    input.DisplayName,
		ConnectionName: input.ConnectionName,
		NodeIDs:        input.NodeIDs,
	})
	if err != nil {
		httpx.ServiceError(w, err)
		return
	}
	results := []map[string]any{}
	for _, item := range enrollment.Results {
		result := map[string]any{"node_id": item.Node.ID, "status": item.Status}
		if item.Err != nil {
			result["status"] = "error"
			result["error_id"] = httpx.LogError(http.StatusBadGateway, item.Err)
			results = append(results, result)
			continue
		}
		result["connection"] = adminview.ConnectionItem(item.Connection)
		result["config_count"] = item.ConfigCount
		results = append(results, result)
	}

	token, record, err := a.getOrCreatePortalToken(enrollment.Account.ID, time.Time{})
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	if enrollment.SummaryErr != nil {
		httpx.StoreError(w, enrollment.SummaryErr)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "accounts.enroll",
		EntityType: "account",
		EntityID:   enrollment.Account.ID,
		Details: map[string]any{
			"username":           enrollment.Account.Username,
			"connection_name":    input.ConnectionName,
			"requested_node_ids": input.NodeIDs,
			"result_count":       len(results),
			"partial":            enrollment.Partial,
		},
	})
	a.publish(r.Context(), model.Event{
		Type:        "account.enrolled",
		EntityType:  "account",
		EntityID:    enrollment.Account.ID,
		Actor:       "admin",
		Message:     enrollmentEventMessage(enrollment.Account, enrollment.Results, enrollment.Partial),
		PayloadJSON: jsonString(map[string]any{"account": adminview.AccountItem(enrollment.Account), "partial": enrollment.Partial}),
	})

	status := http.StatusCreated
	if enrollment.Partial {
		status = http.StatusMultiStatus
	}
	httpx.JSON(w, status, map[string]any{
		"account":      adminview.AccountItem(enrollment.Account),
		"results":      results,
		"token":        token,
		"token_record": adminview.AccountPortalTokens([]model.AccountPortalToken{record})[0],
		"config_page":  a.absoluteURL(r, "/client/"+token),
		"summary":      adminview.Summary(enrollment.Summary),
	})
}

func (a *App) nodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items := make([]map[string]any, 0)
		for _, node := range a.store.ListNodes() {
			items = append(items, a.nodeListItem(node))
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var input createNodeRequest
		if !httpx.Decode(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(string(input.Type)) == "" {
			httpx.Error(w, http.StatusBadRequest, "name and type are required")
			return
		}
		var n model.Node
		var err error
		switch input.Type {
		case model.NodeTypeNativeAmneziaWG:
			httpx.Error(w, http.StatusBadRequest, "native AmneziaWG nodes must be installed through the provisioning endpoint")
			return
		case model.NodeTypeAmneziaAPI:
			if strings.TrimSpace(input.AmneziaAPIBaseURL) == "" {
				httpx.Error(w, http.StatusBadRequest, "amnezia_api_base_url is required")
				return
			}
			n, _, err = a.store.CreateAmneziaAPINode(model.Node{Name: input.Name, Type: input.Type, Region: input.Region}, model.AmneziaAPINodeConfig{BaseURL: strings.TrimSpace(input.AmneziaAPIBaseURL), APIKey: input.AmneziaAPIKey})
		case model.NodeTypeNativeHy2:
			httpx.Error(w, http.StatusBadRequest, "native Hysteria2 nodes must be installed or attached through the provisioning endpoint")
			return
		default:
			httpx.Error(w, http.StatusBadRequest, "unsupported node type")
			return
		}
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		instance, err := a.createRequestedNodeInstance(n.ID, input)
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "nodes.create",
			EntityType: "node",
			EntityID:   n.ID,
			Details:    map[string]any{"name": n.Name, "type": n.Type, "region": n.Region},
		})
		response := map[string]any{"node": adminview.NodeItem(n)}
		if instance.ID != "" {
			response["instance"] = nodeInstanceView(instance)
		}
		httpx.JSON(w, http.StatusCreated, response)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) nodeListItem(node model.Node) map[string]any {
	item := map[string]any{"id": node.ID, "name": node.Name, "type": node.Type, "region": node.Region, "status": node.Status, "created_at": node.CreatedAt, "updated_at": node.UpdatedAt}
	instance, err := a.store.GetActiveNodeInstance(node.ID)
	if err != nil {
		return item
	}
	item["instance"] = map[string]any{"id": instance.ID, "host": instance.Host, "ssh_port": instance.SSHPort, "ssh_user": instance.SSHUser, "status": instance.Status}
	for _, installation := range a.store.ListAgentInstallations() {
		if installation.NodeInstanceID != instance.ID || installation.Status != model.StatusActive {
			continue
		}
		runtime, _ := a.store.GetNodeRuntime(installation.ID)
		online := a.store.ListNodeOnlineConnections(instance.ID)
		item["agent_installation_id"], item["agent_status"], item["service_status"] = installation.ID, runtime.AgentStatus, runtime.ServiceStatus
		item["last_heartbeat_at"], item["online_count"] = jsonTime(runtime.LastHeartbeatAt), onlineCount(online)
		break
	}
	return item
}

type createNodeRequest struct {
	Name    string         `json:"name"`
	Type    model.NodeType `json:"type"`
	Region  string         `json:"region"`
	SSHHost string         `json:"ssh_host"`
	SSHPort int            `json:"ssh_port"`
	SSHUser string         `json:"ssh_user"`
	SSHKey  string         `json:"ssh_key"`

	AmneziaAPIBaseURL string `json:"amnezia_api_base_url"`
	AmneziaAPIKey     string `json:"amnezia_api_key"`
}

func (a *App) createRequestedNodeInstance(nodeID string, input createNodeRequest) (model.NodeInstance, error) {
	host := strings.TrimSpace(input.SSHHost)
	if host == "" {
		return model.NodeInstance{}, nil
	}
	return a.store.CreateNodeInstance(model.NodeInstance{NodeID: nodeID, Host: host, SSHPort: input.SSHPort, SSHUser: strings.TrimSpace(input.SSHUser), SSHKey: strings.TrimSpace(input.SSHKey)})
}

func nodeInstanceView(instance model.NodeInstance) map[string]any {
	return map[string]any{"id": instance.ID, "node_id": instance.NodeID, "host": instance.Host, "ssh_port": instance.SSHPort, "ssh_user": instance.SSHUser, "status": instance.Status, "created_at": instance.CreatedAt, "activated_at": instance.ActivatedAt}
}

func (a *App) installHysteria2Node(w http.ResponseWriter, r *http.Request) {
	a.hysteria2NodeProvision(w, r, "install")
}

func (a *App) attachHysteria2Node(w http.ResponseWriter, r *http.Request) {
	a.hysteria2NodeProvision(w, r, "attach")
}

func (a *App) hysteria2NodeProvision(w http.ResponseWriter, r *http.Request, mode string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input provisioningsvc.Hysteria2Request
	if !httpx.Decode(w, r, &input) {
		return
	}
	if input.FileSetID != "" {
		if input.Settings == nil {
			httpx.Error(w, 400, "Укажите настройки Hysteria")
			return
		}
		if err := hysteriasettings.PrepareFiles(a.store, input.FileSetID, input.Settings); err != nil {
			httpx.Error(w, 400, err.Error())
			return
		}
	}
	if input.Settings != nil && input.Settings.Masquerade.Type == "file" && input.FileSetID == "" {
		httpx.Error(w, 400, "Загрузите файлы или ZIP для маскировки")
		return
	}
	if a.nodeInstall == nil {
		httpx.Error(w, http.StatusBadRequest, "установка сервера не настроена")
		return
	}
	operationID := store.NewID("op")
	command := provisioningsvc.Hysteria2Command{
		Request:           input,
		ServerURL:         a.publicBaseURL,
		AgentGRPCTarget:   a.agentGRPCTarget,
		AgentDownloadBase: a.agentDownloadBase,
	}
	state, err := a.nodeInstall.BootstrapHysteriaInstall(r.Context(), mode, command)
	if err != nil {
		httpx.ValidationError(w, err)
		return
	}
	msg := workqueue.NewHysteriaInstallMessage(operationID, state)
	if err := a.transport.Enqueue(r.Context(), msg); err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	a.publish(r.Context(), model.Event{
		Type:        "hysteria.install.scheduled",
		EntityType:  "hysteria_install",
		EntityID:    operationID,
		Message:     "Установка Hysteria2 VPS запланирована.",
		PayloadJSON: jsonString(map[string]any{"operation_id": operationID, "status": "scheduled", "steps": hysteriaStepItems(state.Steps)}),
	})
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"operation":    map[string]any{"id": operationID, "type": "hysteria2." + mode, "status": "scheduled"},
		"operation_id": operationID,
		"steps":        hysteriaStepItems(state.Steps),
		"logs":         append([]string{"Bootstrap завершён; запрос на установку принят."}, state.Logs...),
	})
}

func hysteriaStepItems(steps []provisioningsvc.Step) []map[string]any {
	out := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		out = append(out, map[string]any{"type": step.Type, "name": step.Name, "status": step.Status, "message": step.Message})
	}
	return out
}

func (a *App) detachHysteria2Node(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.nodeInstall == nil {
		httpx.Error(w, http.StatusBadRequest, "установка сервера не настроена")
		return
	}
	operationID := store.NewID("op")
	state, err := a.nodeInstall.PrepareHysteria2Detach(nodeID)
	if err != nil {
		httpx.ValidationError(w, err)
		return
	}
	msg := workqueue.NewHysteriaDetachMessage(operationID, state)
	if err := a.transport.Enqueue(r.Context(), msg); err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "nodes.hysteria_detach",
		EntityType: "node",
		EntityID:   nodeID,
		Details:    map[string]any{"operation_id": operationID, "name": state.Node.Name},
	})
	a.publish(r.Context(), model.Event{
		Type:        "hysteria.detach.scheduled",
		EntityType:  "node",
		EntityID:    nodeID,
		Message:     "Удаление подключения Hysteria2 VPS запланировано.",
		PayloadJSON: jsonString(map[string]any{"operation_id": operationID, "status": "scheduled", "node_id": nodeID, "node_name": state.Node.Name}),
	})
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"operation":    map[string]any{"id": operationID, "type": "hysteria2.detach", "status": "scheduled"},
		"operation_id": operationID,
		"node":         adminview.NodeItem(state.Node),
	})
}

func (a *App) uninstallHysteria2Node(w http.ResponseWriter, r *http.Request, nodeID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.nodeInstall == nil {
		httpx.Error(w, http.StatusBadRequest, "установка сервера не настроена")
		return
	}
	operationID := store.NewID("op")
	state, err := a.nodeInstall.PrepareHysteria2Uninstall(nodeID)
	if err != nil {
		httpx.ValidationError(w, err)
		return
	}
	msg := workqueue.NewHysteriaUninstallMessage(operationID, state)
	if err := a.transport.Enqueue(r.Context(), msg); err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "nodes.hysteria_uninstall",
		EntityType: "node",
		EntityID:   nodeID,
		Details:    map[string]any{"operation_id": operationID, "name": state.Node.Name},
	})
	a.publish(r.Context(), model.Event{
		Type:        "hysteria.uninstall.scheduled",
		EntityType:  "node",
		EntityID:    nodeID,
		Message:     "Удаление Hysteria2 VPS запланировано.",
		PayloadJSON: jsonString(map[string]any{"operation_id": operationID, "status": "scheduled", "node_id": nodeID, "node_name": state.Node.Name}),
	})
	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"operation":    map[string]any{"id": operationID, "type": "hysteria2.uninstall", "status": "scheduled"},
		"operation_id": operationID,
		"node":         adminview.NodeItem(state.Node),
	})
}

func (a *App) nodeSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, a.webPrefix+"/api/v1/nodes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && parts[1] == "amnezia-settings" {
		a.amneziaSettings(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "hysteria-settings" {
		a.hysteriaSettings(w, r, parts[0])
		return
	}

	if len(parts) == 1 && r.Method == http.MethodGet {
		a.nodeDetails(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "commands" && r.Method == http.MethodPost {
		a.createNodeCommand(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "uninstall-hysteria2" {
		a.uninstallHysteria2Node(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "detach-hysteria2" {
		a.detachHysteria2Node(w, r, parts[0])
		return
	}
	if len(parts) != 2 || parts[1] != "remote-accounts" || r.Method != http.MethodGet {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	node, err := a.store.GetNode(parts[0])
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	result, err := a.connections.ListRemoteConnections(r.Context(), node)
	if err != nil {
		if errors.Is(err, connections.ErrUnsupportedNodeType) {
			httpx.Error(w, http.StatusBadRequest, "remote-accounts is not implemented for this node type")
			return
		}
		httpx.PrivateError(w, http.StatusBadGateway, "remote operation failed", err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (a *App) nodeDetails(w http.ResponseWriter, r *http.Request, nodeID string) {
	node, err := a.store.GetNode(nodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	instance, _ := a.store.GetActiveNodeInstance(nodeID)
	installation := a.activeInstallation(instance.ID)
	runtime, _ := a.store.GetNodeRuntime(installation.ID)
	online := a.store.ListNodeOnlineConnections(instance.ID)
	to := time.Now().UTC()
	usage, err := a.store.NodeUsageSummary(instance.ID, to.Add(-24*time.Hour).UnixMilli(), to.UnixMilli(), 10)
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	usageViews := make([]map[string]any, 0, len(usage.Items))
	for _, item := range usage.Items {
		usageViews = append(usageViews, map[string]any{"connection_id": item.ConnectionID, "account_name": item.AccountName, "connection_name": item.ConnectionName, "from_ms": item.FromMS, "to_ms": item.ToMS, "tx_bytes": item.TXBytes, "rx_bytes": item.RXBytes, "total_bytes": item.TXBytes + item.RXBytes})
	}
	response := map[string]any{
		"node":         adminview.NodeItem(node),
		"runtime":      nodeRuntimeView(runtime),
		"online":       nodeOnlineViews(online),
		"usage":        usageViews,
		"instance":     instance,
		"installation": installation,
		"commands":     a.store.ListRuntimeCommands(installation.ID, 20),
		"totals":       map[string]any{"tx_bytes": usage.TXBytes, "rx_bytes": usage.RXBytes, "total_bytes": usage.TXBytes + usage.RXBytes, "online_count": onlineCount(online)},
		"generated":    time.Now().UTC(),
	}
	if node.Type == model.NodeTypeNativeAmneziaWG {
		config, err := a.store.GetAmneziaWGNodeConfig(nodeID)
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		response["amnezia_settings"] = adminview.AmneziaSettingsItem(config)
	}
	httpx.JSON(w, http.StatusOK, response)
}

func (a *App) createNodeCommand(w http.ResponseWriter, r *http.Request, nodeID string) {
	if _, err := a.store.GetNode(nodeID); err != nil {
		httpx.StoreError(w, err)
		return
	}
	instance, err := a.store.GetActiveNodeInstance(nodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	installation := a.activeInstallation(instance.ID)
	if installation.ID == "" {
		httpx.Error(w, http.StatusConflict, "active agent installation not found")
		return
	}
	var input struct {
		Type      string            `json:"type"`
		Payload   map[string]string `json:"payload"`
		ExpiresIn int               `json:"expires_in_seconds"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if !allowedRuntimeCommand(input.Type) {
		httpx.Error(w, http.StatusBadRequest, "unsupported command")
		return
	}
	rawPayload, _ := json.Marshal(input.Payload)
	expiresAt := time.Now().UTC().Add(60 * time.Second)
	if input.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(input.ExpiresIn) * time.Second)
	}
	command, err := a.store.CreateRuntimeCommand(model.RuntimeCommand{
		AgentInstallationID: installation.ID,
		Type:                input.Type,
		Payload:             string(rawPayload),
		ExpiresAt:           expiresAt,
	})
	if err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "nodes.command",
		EntityType: "node",
		EntityID:   nodeID,
		Details:    map[string]any{"command_id": command.ID, "type": command.Type},
	})
	httpx.JSON(w, http.StatusCreated, command)
}

func jsonTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nodeRuntimeView(runtime model.NodeRuntime) map[string]any {
	return map[string]any{
		"agent_installation_id": runtime.AgentInstallationID,
		"node_instance_id":      runtime.NodeInstanceID,
		"agent_status":          runtime.AgentStatus,
		"last_heartbeat_at":     jsonTime(runtime.LastHeartbeatAt),
		"agent_version":         runtime.AgentVersion,
		"protocol_version":      runtime.ProtocolVersion,
		"service_status":        runtime.ServiceStatus,
		"recent_message":        runtime.RecentMessage,
		"updated_at":            runtime.UpdatedAt,
	}
}

func nodeOnlineViews(items []model.NodeOnlineConnection) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"node_instance_id": item.NodeInstanceID,
			"connection_id":    item.ConnectionID,
			"count":            item.Count,
			"first_seen_at":    item.FirstSeenAt,
			"last_seen_at":     item.LastSeenAt,
		})
	}
	return out
}

func allowedRuntimeCommand(command string) bool {
	switch command {
	case "GetStatus", "RestartHysteria", "StartHysteria", "StopHysteria", "CollectLogs", "DumpStreams", "ApplyHysteriaConfig",
		"GetAmneziaWGStatus", "GetAWGStatus", "CollectAmneziaWGLogs", "CollectAWGLogs", "RestartAmneziaWG", "RestartAWG":
		return true
	default:
		return false
	}
}

func onlineCount(items []model.NodeOnlineConnection) int {
	var total int
	for _, item := range items {
		total += item.Count
	}
	return total
}

func (a *App) activeInstallation(instanceID string) model.AgentInstallation {
	for _, installation := range a.store.ListAgentInstallations() {
		if installation.NodeInstanceID == instanceID && installation.Status == model.StatusActive {
			return installation
		}
	}
	return model.AgentInstallation{}
}

func (a *App) connectionsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.Connections(a.store.ListConnections())})
	case http.MethodPost:
		var input struct {
			AccountID string         `json:"account_id"`
			NodeID    string         `json:"node_id"`
			Name      string         `json:"name"`
			Protocol  model.Protocol `json:"protocol"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		node, err := a.store.GetNode(input.NodeID)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		created, err := a.connections.CreateConnection(r.Context(), connections.ConnectionInput{AccountID: input.AccountID, Node: node, Protocol: input.Protocol, Name: input.Name})
		if err != nil {
			httpx.ServiceError(w, err)
			return
		}
		connection := created.Connection
		a.recordAudit(r, auditsvc.Event{
			Action:     "connections.create",
			EntityType: "connection",
			EntityID:   connection.ID,
			Details:    map[string]any{"account_id": connection.AccountID, "node_id": connection.NodeID, "name": connection.Name, "protocol": connection.Protocol},
		})
		accountName := a.accountEventLabel(connection.AccountID)
		nodeName := a.nodeEventLabel(connection.NodeID)
		a.publish(r.Context(), model.Event{
			Type:        "connection.created",
			EntityType:  "connection",
			EntityID:    connection.ID,
			Actor:       "admin",
			Message:     "Подключение " + connection.Name + " для " + accountName + " запланировано на " + nodeName + ".",
			PayloadJSON: jsonString(map[string]any{"account_id": connection.AccountID, "account_name": accountName, "connection_id": connection.ID, "connection_name": connection.Name, "node_id": connection.NodeID, "node_name": nodeName}),
		})
		httpx.JSON(w, http.StatusCreated, adminview.ConnectionItem(connection))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) configs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.IssuedConfigs(a.store.ListIssuedConfigs())})
	case http.MethodPost:
		var input struct {
			ConnectionID string           `json:"connection_id"`
			Kind         model.ConfigKind `json:"kind"`
			Slug         string           `json:"slug"`
			Name         string           `json:"name"`
			Client       string           `json:"client"`
			ContentType  string           `json:"content_type"`
			Config       string           `json:"config"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		createInput := accounts.CreateIssuedConfigInput{
			ConnectionID: input.ConnectionID,
			Kind:         input.Kind,
			Slug:         input.Slug,
			Name:         input.Name,
			Client:       input.Client,
			ContentType:  input.ContentType,
			Config:       input.Config,
		}
		if err := createInput.Validate(); err != nil {
			httpx.ValidationError(w, err)
			return
		}
		c, err := a.accounts.CreateIssuedConfig(createInput)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "configs.create",
			EntityType: "issued_config",
			EntityID:   c.ID,
			Details:    map[string]any{"connection_id": c.ConnectionID, "kind": c.Kind, "slug": c.Slug, "client": c.Client},
		})
		httpx.JSON(w, http.StatusCreated, adminview.IssuedConfigItem(c))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) configProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.ConfigProfiles(a.listConfigProfiles())})
	case http.MethodPost:
		var input struct {
			Protocol       model.Protocol   `json:"protocol"`
			Kind           model.ConfigKind `json:"kind"`
			Slug           string           `json:"slug"`
			Name           string           `json:"name"`
			Client         string           `json:"client"`
			ContentType    string           `json:"content_type"`
			ConfigTemplate string           `json:"config_template"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		createInput := accounts.CreateConfigProfileInput{
			Protocol:       input.Protocol,
			Kind:           input.Kind,
			Slug:           input.Slug,
			Name:           input.Name,
			Client:         input.Client,
			ContentType:    input.ContentType,
			ConfigTemplate: input.ConfigTemplate,
		}
		if err := createInput.Validate(); err != nil {
			httpx.ValidationError(w, err)
			return
		}
		p, err := a.accounts.CreateConfigProfile(createInput)
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "config_profiles.create",
			EntityType: "config_profile",
			EntityID:   p.ID,
			Details:    map[string]any{"protocol": p.Protocol, "kind": p.Kind, "slug": p.Slug, "client": p.Client},
		})
		httpx.JSON(w, http.StatusCreated, adminview.ConfigProfileItem(p))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) policyTags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.PolicyTags(a.store.ListPolicyTags())})
	case http.MethodPost:
		var input struct {
			Slug            string   `json:"slug"`
			Name            string   `json:"name"`
			AllowedNodeIDs  []string `json:"allowed_node_ids"`
			ConnectionLimit int      `json:"connection_limit"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.Name) == "" {
			httpx.Error(w, http.StatusBadRequest, "slug and name are required")
			return
		}
		tag, err := a.store.CreatePolicyTag(model.PolicyTag{
			Slug:            input.Slug,
			Name:            input.Name,
			AllowedNodeIDs:  input.AllowedNodeIDs,
			ConnectionLimit: input.ConnectionLimit,
		})
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "policy_tags.create",
			EntityType: "policy_tag",
			EntityID:   tag.ID,
			Details:    map[string]any{"slug": tag.Slug, "connection_limit": tag.ConnectionLimit, "allowed_node_ids": tag.AllowedNodeIDs},
		})
		httpx.JSON(w, http.StatusCreated, adminview.PolicyTagItem(tag))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) accountPolicyTags(w http.ResponseWriter, r *http.Request, accountID string) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.AccountPolicyTags(a.store.ListAccountPolicyTags(accountID))})
	case http.MethodPost:
		var input struct {
			TagID string `json:"tag_id"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		assigned, err := a.store.AssignPolicyTag(model.AccountPolicyTag{AccountID: accountID, TagID: input.TagID})
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "accounts.policy_tags.assign",
			EntityType: "account",
			EntityID:   accountID,
			Details:    map[string]any{"tag_id": input.TagID},
		})
		httpx.JSON(w, http.StatusCreated, adminview.AccountPolicyTagItem(assigned))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) tokens(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"portal_tokens": adminview.AccountPortalTokens(a.store.ListAccountPortalTokens()), "subscription_tokens": adminview.ConnectionSubscriptionTokens(a.store.ListConnectionSubscriptionTokens())})
	case http.MethodPost:
		var input struct {
			Scope        string    `json:"scope"`
			AccountID    string    `json:"account_id"`
			ConnectionID string    `json:"connection_id"`
			ExpiresAt    time.Time `json:"expires_at"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		switch input.Scope {
		case "portal":
			if input.AccountID == "" {
				httpx.Error(w, http.StatusBadRequest, "account_id is required")
				return
			}
			raw, record, err := a.getOrCreatePortalToken(input.AccountID, input.ExpiresAt)
			if err != nil {
				httpx.StoreError(w, err)
				return
			}
			a.recordAudit(r, auditsvc.Event{Action: "tokens.portal.create", EntityType: "account_portal_token", EntityID: record.ID, Details: map[string]any{"account_id": record.AccountID}})
			httpx.JSON(w, http.StatusCreated, map[string]any{"token": raw, "record": adminview.AccountPortalTokens([]model.AccountPortalToken{record})[0], "config_page": a.absoluteURL(r, "/client/"+raw)})
		case "subscription":
			if input.ConnectionID == "" {
				httpx.Error(w, http.StatusBadRequest, "connection_id is required")
				return
			}
			raw, record, err := a.getOrCreateSubscriptionToken(input.ConnectionID, input.ExpiresAt)
			if err != nil {
				httpx.StoreError(w, err)
				return
			}
			a.recordAudit(r, auditsvc.Event{Action: "tokens.subscription.create", EntityType: "connection_subscription_token", EntityID: record.ID, Details: map[string]any{"connection_id": record.ConnectionID}})
			httpx.JSON(w, http.StatusCreated, map[string]any{"token": raw, "record": adminview.ConnectionSubscriptionTokens([]model.ConnectionSubscriptionToken{record})[0], "subscription": a.absoluteURL(r, "/sub/"+raw)})
		default:
			httpx.Error(w, http.StatusBadRequest, "scope must be portal or subscription")
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) accessSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, a.webPrefix+"/api/v1/connections/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 1 && parts[0] == "provision" && r.Method == http.MethodPost {
		a.provisionAccess(w, r)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	connectionID, action := parts[0], parts[1]
	switch action {
	case "hold":
		a.setAccessRemoteStatus(w, r, connectionID, model.ConnectionStatusHeld, "disabled")
	case "resume":
		a.setAccessRemoteStatus(w, r, connectionID, model.ConnectionStatusActive, "active")
	case "delete":
		a.deleteRemoteAccess(w, r, connectionID)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (a *App) deletedConnectionsForAccount(w http.ResponseWriter, accountID string) {
	summary, err := a.store.Summary(accountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	rows := []map[string]any{}
	for _, connection := range a.store.ListConnections() {
		if connection.AccountID != accountID || connection.DesiredStatus != model.ConnectionStatusDeleted {
			continue
		}
		node, _ := a.store.GetNode(connection.NodeID)
		rows = append(rows, map[string]any{
			"connection": adminview.ConnectionItem(connection),
			"account":    adminview.AccountItem(summary.Account),
			"node":       adminview.NodeItem(node),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"account": adminview.AccountItem(summary.Account),
		"items":   rows,
	})
}

func accountLabel(account model.Account, fallback string) string {
	if label := strings.TrimSpace(account.DisplayName); label != "" {
		return label
	}
	if label := strings.TrimSpace(account.Username); label != "" {
		return label
	}
	return fallback
}

func nodeLabel(node model.Node, fallback string) string {
	if label := strings.TrimSpace(node.Name); label != "" {
		return label
	}
	return fallback
}

func (a *App) accountEventLabel(accountID string) string {
	account, err := a.store.GetAccount(accountID)
	if err != nil {
		return accountID
	}
	return accountLabel(account, accountID)
}

func (a *App) nodeEventLabel(nodeID string) string {
	node, err := a.store.GetNode(nodeID)
	if err != nil {
		return nodeID
	}
	return nodeLabel(node, nodeID)
}

func enrollmentEventMessage(account model.Account, results []accounts.EnrollmentNodeResult, partial bool) string {
	accountName := accountLabel(account, account.ID)
	nodeNames := make([]string, 0, len(results))
	for _, item := range results {
		if item.Err != nil {
			continue
		}
		nodeNames = append(nodeNames, nodeLabel(item.Node, item.Node.ID))
	}
	if len(nodeNames) == 0 {
		if partial {
			return "Аккаунт " + accountName + " создан, но подключения созданы частично."
		}
		return "Аккаунт " + accountName + " создан."
	}
	prefix := "Подключения для " + accountName + " запланированы на "
	if len(nodeNames) == 1 {
		prefix = "Подключение для " + accountName + " запланировано на "
	}
	message := prefix + strings.Join(nodeNames, ", ") + "."
	if partial {
		message = "Частично: " + message
	}
	return message
}

func (a *App) provisionAccess(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID string         `json:"account_id"`
		NodeID    string         `json:"node_id"`
		Name      string         `json:"name"`
		Protocol  model.Protocol `json:"protocol"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if input.AccountID == "" || input.NodeID == "" || strings.TrimSpace(input.Name) == "" || input.Protocol == "" {
		httpx.Error(w, http.StatusBadRequest, "account_id, node_id, name and protocol are required")
		return
	}

	node, err := a.store.GetNode(input.NodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	result, err := a.connections.CreateConnection(r.Context(), connections.ConnectionInput{
		AccountID: input.AccountID,
		Node:      node,
		Protocol:  input.Protocol,
		Name:      input.Name,
	})
	if err != nil {
		if strings.HasPrefix(err.Error(), "unsupported") {
			httpx.Error(w, http.StatusBadRequest, "unsupported operation")
			return
		}
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrDuplicateConnection) {
			httpx.StoreError(w, err)
			return
		}
		httpx.PrivateError(w, http.StatusBadGateway, "remote operation failed", err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "connections.provision",
		EntityType: "connection",
		EntityID:   result.Connection.ID,
		Details:    map[string]any{"account_id": result.Connection.AccountID, "node_id": result.Connection.NodeID, "name": result.Connection.Name, "protocol": result.Connection.Protocol, "config_count": len(result.Configs)},
	})
	accountName := a.accountEventLabel(result.Connection.AccountID)
	nodeName := a.nodeEventLabel(result.Connection.NodeID)
	a.publish(r.Context(), model.Event{
		Type:        "connection.created",
		EntityType:  "connection",
		EntityID:    result.Connection.ID,
		Actor:       "admin",
		Message:     "Подключение " + result.Connection.Name + " для " + accountName + " запланировано на " + nodeName + ".",
		PayloadJSON: jsonString(map[string]any{"account_id": result.Connection.AccountID, "account_name": accountName, "connection_id": result.Connection.ID, "connection_name": result.Connection.Name, "node_id": result.Connection.NodeID, "node_name": nodeName}),
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"connection":   adminview.ConnectionItem(result.Connection),
		"config_count": len(result.Configs),
	})
}
