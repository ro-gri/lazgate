package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"laz/internal/common/httpx"
	"laz/internal/model"
	"laz/internal/services/accounts"
	adminauthsvc "laz/internal/services/adminauth"
	auditsvc "laz/internal/services/audit"
	"laz/internal/services/connections"
	"laz/internal/storage"
	adminview "laz/internal/transport/http/admin/view"
)

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
		Token string `json:"token"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	result, err := a.adminAuth.Login(input.Token)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	cookie := a.adminAuth.NewSessionCookie(result.SessionToken)
	cookie.Secure = requestIsHTTPS(r)
	http.SetCookie(w, cookie)
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), result.Principal)), auditsvc.Event{
		Action:     "admin.login",
		EntityType: "admin_session",
		EntityID:   result.Session.ID,
		Details:    map[string]any{"role": result.Principal.Role},
	})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"principal":  result.Principal,
		"csrf_token": result.CSRFToken,
	})
}

func (a *App) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	principal, _ := adminauthsvc.PrincipalFromContext(r.Context())
	_ = a.adminAuth.Logout(r)
	http.SetCookie(w, a.adminAuth.ExpiredSessionCookie())
	a.recordAudit(r.WithContext(adminauthsvc.WithPrincipal(r.Context(), principal)), auditsvc.Event{
		Action:     "admin.logout",
		EntityType: "admin_session",
		Details:    map[string]any{"role": principal.Role},
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"status": "ok"})
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
		status := model.Status(strings.TrimSpace(r.URL.Query().Get("status")))
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
			Note        string `json:"note"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		createInput := accounts.CreateAccountInput{
			Username:    input.Username,
			DisplayName: input.DisplayName,
			Note:        input.Note,
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
		httpx.JSON(w, http.StatusCreated, adminview.AccountItem(u))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) userSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "hold":
			a.setAccountRemoteStatus(w, r, parts[0], model.StatusHeld, "disabled")
			return
		case "resume":
			a.setAccountRemoteStatus(w, r, parts[0], model.StatusActive, "active")
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
	if len(parts) == 2 && parts[1] == "active-configs" && r.Method == http.MethodGet {
		summary, err := a.store.ClientSummary(parts[0], r.URL.Query().Get("client_id"))
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		token := model.AccessToken{
			AccountID: parts[0],
			ClientID:  r.URL.Query().Get("client_id"),
			Purpose:   model.TokenPurposeClient,
			Status:    model.StatusActive,
		}
		httpx.JSON(w, http.StatusOK, adminview.ActiveClientSummary(token, summary))
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
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Note        string `json:"note"`
		Client      struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"client"`
		Nodes          string   `json:"nodes"`
		NodeIDs        []string `json:"node_ids"`
		TrafficLimitGB int      `json:"traffic_limit_gb"`
		ExpirationDays int      `json:"expiration_days"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	enrollment, err := a.accounts.Enroll(r.Context(), accounts.EnrollmentInput{
		Username:       input.Username,
		DisplayName:    input.DisplayName,
		Note:           input.Note,
		ClientSlug:     input.Client.Slug,
		ClientName:     input.Client.Name,
		Nodes:          input.Nodes,
		NodeIDs:        input.NodeIDs,
		TrafficLimitGB: input.TrafficLimitGB,
		ExpirationDays: input.ExpirationDays,
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

	token, record, err := a.getOrCreateClientToken(enrollment.Account.ID, "", time.Time{})
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
			"client_id":          enrollment.Client.ID,
			"client_slug":        enrollment.Client.Slug,
			"requested_node_ids": input.NodeIDs,
			"result_count":       len(results),
			"partial":            enrollment.Partial,
		},
	})

	status := http.StatusCreated
	if enrollment.Partial {
		status = http.StatusMultiStatus
	}
	httpx.JSON(w, status, map[string]any{
		"account":      adminview.AccountItem(enrollment.Account),
		"client":       adminview.ClientItem(enrollment.Client),
		"results":      results,
		"token":        token,
		"token_record": adminview.AccessTokenItem(record),
		"config_page":  a.absoluteURL(r, "/connect/"+token),
		"summary":      adminview.Summary(enrollment.Summary),
	})
}

func (a *App) nodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.Nodes(a.store.ListNodes())})
	case http.MethodPost:
		var input struct {
			Name       string         `json:"name"`
			Type       model.NodeType `json:"type"`
			BaseURL    string         `json:"base_url"`
			APIKey     string         `json:"api_key"`
			Region     string         `json:"region"`
			SSHHost    string         `json:"ssh_host"`
			SSHPort    int            `json:"ssh_port"`
			SSHUser    string         `json:"ssh_user"`
			SSHKeyPath string         `json:"ssh_key_path"`
			UseIPv6    bool           `json:"use_ipv6"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(string(input.Type)) == "" {
			httpx.Error(w, http.StatusBadRequest, "name and type are required")
			return
		}
		n, err := a.store.CreateNode(model.Node{
			Name:       input.Name,
			Type:       input.Type,
			BaseURL:    input.BaseURL,
			APIKey:     input.APIKey,
			Region:     input.Region,
			SSHHost:    input.SSHHost,
			SSHPort:    input.SSHPort,
			SSHUser:    input.SSHUser,
			SSHKeyPath: input.SSHKeyPath,
			UseIPv6:    input.UseIPv6,
		})
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
		httpx.JSON(w, http.StatusCreated, adminview.NodeItem(n))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) nodeSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
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

func (a *App) clientsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		AccountID string `json:"account_id"`
		Slug      string `json:"slug"`
		Name      string `json:"name"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	createInput := accounts.CreateClientInput{
		AccountID: input.AccountID,
		Slug:      input.Slug,
		Name:      input.Name,
	}
	if err := createInput.Validate(); err != nil {
		httpx.ValidationError(w, err)
		return
	}
	d, err := a.accounts.CreateClient(createInput)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "clients.create",
		EntityType: "client",
		EntityID:   d.ID,
		Details:    map[string]any{"account_id": d.AccountID, "slug": d.Slug, "name": d.Name},
	})
	httpx.JSON(w, http.StatusCreated, adminview.ClientItem(d))
}

func (a *App) connectionsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.Connections(a.store.ListConnections())})
	case http.MethodPost:
		var input struct {
			AccountID  string         `json:"account_id"`
			ClientID   string         `json:"client_id"`
			NodeID     string         `json:"node_id"`
			Protocol   model.Protocol `json:"protocol"`
			RemoteID   string         `json:"remote_id"`
			RemoteName string         `json:"remote_name"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		createInput := accounts.CreateConnectionInput{
			AccountID:  input.AccountID,
			ClientID:   input.ClientID,
			NodeID:     input.NodeID,
			Protocol:   input.Protocol,
			RemoteID:   input.RemoteID,
			RemoteName: input.RemoteName,
		}
		if err := createInput.Validate(); err != nil {
			httpx.ValidationError(w, err)
			return
		}
		connection, err := a.accounts.CreateConnection(createInput)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "connections.create",
			EntityType: "connection",
			EntityID:   connection.ID,
			Details:    map[string]any{"account_id": connection.AccountID, "client_id": connection.ClientID, "node_id": connection.NodeID, "protocol": connection.Protocol},
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
			Slug           string   `json:"slug"`
			Name           string   `json:"name"`
			AllowedNodeIDs []string `json:"allowed_node_ids"`
			ClientLimit    int      `json:"client_limit"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.Slug) == "" || strings.TrimSpace(input.Name) == "" {
			httpx.Error(w, http.StatusBadRequest, "slug and name are required")
			return
		}
		tag, err := a.store.CreatePolicyTag(model.PolicyTag{
			Slug:           input.Slug,
			Name:           input.Name,
			AllowedNodeIDs: input.AllowedNodeIDs,
			ClientLimit:    input.ClientLimit,
		})
		if err != nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "internal error", err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "policy_tags.create",
			EntityType: "policy_tag",
			EntityID:   tag.ID,
			Details:    map[string]any{"slug": tag.Slug, "client_limit": tag.ClientLimit, "allowed_node_ids": tag.AllowedNodeIDs},
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
		httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.AccessTokens(a.store.ListAccessTokens())})
	case http.MethodPost:
		var input struct {
			AccountID string    `json:"account_id"`
			ClientID  string    `json:"client_id"`
			Purpose   string    `json:"purpose"`
			ExpiresAt time.Time `json:"expires_at"`
		}
		if !httpx.Decode(w, r, &input) {
			return
		}
		if input.AccountID == "" {
			httpx.Error(w, http.StatusBadRequest, "account_id is required")
			return
		}
		if input.Purpose == "" {
			input.Purpose = model.TokenPurposeClient
		}
		if input.Purpose != model.TokenPurposeClient {
			httpx.Error(w, http.StatusBadRequest, "unsupported token purpose")
			return
		}
		token, record, err := a.getOrCreateClientToken(input.AccountID, input.ClientID, input.ExpiresAt)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		a.recordAudit(r, auditsvc.Event{
			Action:     "tokens.create",
			EntityType: "access_token",
			EntityID:   record.ID,
			Details:    map[string]any{"account_id": record.AccountID, "client_id": record.ClientID, "purpose": record.Purpose},
		})
		response := map[string]any{
			"token":       token,
			"record":      adminview.AccessTokenItem(record),
			"config_page": a.absoluteURL(r, "/connect/"+token),
		}
		if record.ClientID != "" {
			response["subscription"] = a.absoluteURL(r, "/sub/"+token)
		}
		httpx.JSON(w, http.StatusCreated, response)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) accessSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/connections/")
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
		a.setAccessRemoteStatus(w, r, connectionID, model.StatusHeld, "disabled")
	case "resume":
		a.setAccessRemoteStatus(w, r, connectionID, model.StatusActive, "active")
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
	clientByID := map[string]model.Client{}
	for _, client := range summary.Clients {
		clientByID[client.ID] = client
	}
	configsByAccess := map[string][]adminview.IssuedConfig{}
	for _, cfg := range a.store.ListIssuedConfigs() {
		if cfg.Status != model.StatusDeleted {
			continue
		}
		connection, err := a.store.GetConnection(cfg.ConnectionID)
		if err != nil || connection.AccountID != accountID {
			continue
		}
		configsByAccess[cfg.ConnectionID] = append(configsByAccess[cfg.ConnectionID], adminview.IssuedConfigItem(cfg))
	}

	rows := []map[string]any{}
	for _, connection := range a.store.ListConnections() {
		if connection.AccountID != accountID || connection.Status != model.StatusDeleted {
			continue
		}
		node, _ := a.store.GetNode(connection.NodeID)
		client := clientByID[connection.ClientID]
		if client.ID == "" {
			client = model.Client{ID: connection.ClientID}
		}
		rows = append(rows, map[string]any{
			"connection": adminview.ConnectionItem(connection),
			"account":    adminview.AccountItem(summary.Account),
			"client":     adminview.ClientItem(client),
			"node":       adminview.NodeItem(node),
			"configs":    configsByAccess[connection.ID],
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"account": adminview.AccountItem(summary.Account),
		"items":   rows,
	})
}

func (a *App) provisionAccess(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID      string         `json:"account_id"`
		ClientID       string         `json:"client_id"`
		NodeID         string         `json:"node_id"`
		Protocol       model.Protocol `json:"protocol"`
		RemoteName     string         `json:"remote_name"`
		ConfigName     string         `json:"config_name"`
		Password       string         `json:"password"`
		TrafficLimitGB int            `json:"traffic_limit_gb"`
		ExpirationDays int            `json:"expiration_days"`
		Unlimited      bool           `json:"unlimited"`
	}
	if !httpx.Decode(w, r, &input) {
		return
	}
	if input.AccountID == "" || input.ClientID == "" || input.NodeID == "" || input.Protocol == "" {
		httpx.Error(w, http.StatusBadRequest, "account_id, client_id, node_id and protocol are required")
		return
	}

	node, err := a.store.GetNode(input.NodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if input.RemoteName == "" {
		account, err := a.store.GetAccount(input.AccountID)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		client, err := a.connections.GetClient(input.AccountID, input.ClientID)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
		input.RemoteName = connections.RemoteNameFor(account.Username, client.Slug, account.ID)
	}

	result, err := a.connections.CreateConnection(r.Context(), connections.ConnectionInput{
		AccountID:      input.AccountID,
		ClientID:       input.ClientID,
		Node:           node,
		Protocol:       input.Protocol,
		RemoteName:     input.RemoteName,
		ConfigName:     input.ConfigName,
		Password:       input.Password,
		TrafficLimitGB: input.TrafficLimitGB,
		ExpirationDays: input.ExpirationDays,
		Unlimited:      input.Unlimited,
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
	if input.Protocol == model.ProtocolAmneziaWG && len(result.Configs) == 1 {
		a.recordAudit(r, auditsvc.Event{
			Action:     "connections.provision",
			EntityType: "connection",
			EntityID:   result.Connection.ID,
			Details:    map[string]any{"account_id": result.Connection.AccountID, "client_id": result.Connection.ClientID, "node_id": result.Connection.NodeID, "protocol": result.Connection.Protocol, "config_count": len(result.Configs)},
		})
		httpx.JSON(w, http.StatusCreated, map[string]any{
			"connection": adminview.ConnectionItem(result.Connection),
			"config":     adminview.IssuedConfigItem(result.Configs[0]),
		})
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "connections.provision",
		EntityType: "connection",
		EntityID:   result.Connection.ID,
		Details:    map[string]any{"account_id": result.Connection.AccountID, "client_id": result.Connection.ClientID, "node_id": result.Connection.NodeID, "protocol": result.Connection.Protocol, "config_count": len(result.Configs)},
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"connection": adminview.ConnectionItem(result.Connection),
		"configs":    adminview.IssuedConfigs(result.Configs),
	})
}
