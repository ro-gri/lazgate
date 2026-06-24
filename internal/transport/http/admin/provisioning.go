package admin

import (
	"net/http"
	"strings"

	"laz/internal/model"
	auditsvc "laz/internal/services/audit"
	adminview "laz/internal/transport/http/admin/view"
	"laz/internal/transport/http/httpx"
)

func (a *App) setAccessRemoteStatus(w http.ResponseWriter, r *http.Request, connectionID string, localStatus model.Status, remoteStatus string) {
	connection, node, ok := a.getAccessAndNode(w, connectionID)
	if !ok {
		return
	}
	updated, err := a.connections.SetConnectionStatus(r.Context(), connection, node, localStatus, remoteStatus)
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     accessStatusAction(localStatus),
		EntityType: "connection",
		EntityID:   updated.ID,
		Details:    map[string]any{"account_id": updated.AccountID, "client_id": updated.ClientID, "node_id": updated.NodeID, "status": updated.Status},
	})
	httpx.JSON(w, http.StatusOK, adminview.ConnectionItem(updated))
}

func (a *App) deleteRemoteAccess(w http.ResponseWriter, r *http.Request, connectionID string) {
	connection, node, ok := a.getAccessAndNode(w, connectionID)
	if !ok {
		return
	}
	updated, err := a.connections.DeleteConnection(r.Context(), connection, node)
	if err != nil {
		writeConnectionError(w, err)
		return
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "connections.delete",
		EntityType: "connection",
		EntityID:   updated.ID,
		Details:    map[string]any{"account_id": updated.AccountID, "client_id": updated.ClientID, "node_id": updated.NodeID},
	})
	httpx.JSON(w, http.StatusOK, adminview.ConnectionItem(updated))
}

func (a *App) setAccountRemoteStatus(w http.ResponseWriter, r *http.Request, accountID string, localStatus model.Status, remoteStatus string) {
	account, err := a.store.UpdateAccountStatus(accountID, localStatus)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	summary, err := a.store.Summary(accountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}

	results := []map[string]any{}
	successes := 0
	for _, item := range summary.Connections {
		connection := item.Connection
		if connection.Status == model.StatusDeleted {
			continue
		}
		result := map[string]any{
			"connection_id": connection.ID,
			"node_id":       item.Node.ID,
		}
		updated, err := a.connections.SetConnectionStatus(r.Context(), connection, item.Node, localStatus, remoteStatus)
		if err != nil {
			result["status"] = "error"
			result["error_id"] = httpx.LogError(http.StatusBadGateway, err)
			results = append(results, result)
			continue
		}
		successes++
		result["status"] = "updated"
		result["connection"] = adminview.ConnectionItem(updated)
		results = append(results, result)
	}

	status := http.StatusOK
	if successes != len(results) {
		status = http.StatusMultiStatus
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     userStatusAction(localStatus),
		EntityType: "account",
		EntityID:   account.ID,
		Details:    map[string]any{"status": account.Status, "result_count": len(results), "successes": successes},
	})
	httpx.JSON(w, status, map[string]any{
		"account": adminview.AccountItem(account),
		"results": results,
	})
}

func (a *App) deleteRemoteAccount(w http.ResponseWriter, r *http.Request, accountID string) {
	account, err := a.store.GetAccount(accountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	summary, err := a.store.Summary(accountID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}

	results := []map[string]any{}
	successes := 0
	for _, item := range summary.Connections {
		connection := item.Connection
		if connection.Status == model.StatusDeleted {
			continue
		}
		result := map[string]any{
			"connection_id": connection.ID,
			"node_id":       item.Node.ID,
		}
		updated, err := a.connections.DeleteConnection(r.Context(), connection, item.Node)
		if err != nil {
			result["status"] = "error"
			result["error_id"] = httpx.LogError(http.StatusBadGateway, err)
			results = append(results, result)
			continue
		}
		successes++
		result["status"] = "deleted"
		result["connection"] = adminview.ConnectionItem(updated)
		results = append(results, result)
	}

	status := http.StatusOK
	if successes != len(results) {
		status = http.StatusMultiStatus
	} else {
		account, err = a.store.UpdateAccountStatus(accountID, model.StatusDeleted)
		if err != nil {
			httpx.StoreError(w, err)
			return
		}
	}
	a.recordAudit(r, auditsvc.Event{
		Action:     "accounts.delete",
		EntityType: "account",
		EntityID:   account.ID,
		Details:    map[string]any{"result_count": len(results), "successes": successes, "status": account.Status},
	})
	httpx.JSON(w, status, map[string]any{
		"account": adminview.AccountItem(account),
		"results": results,
	})
}

func (a *App) getAccessAndNode(w http.ResponseWriter, connectionID string) (model.Connection, model.Node, bool) {
	connection, err := a.store.GetConnection(connectionID)
	if err != nil {
		httpx.StoreError(w, err)
		return model.Connection{}, model.Node{}, false
	}
	node, err := a.store.GetNode(connection.NodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return model.Connection{}, model.Node{}, false
	}
	return connection, node, true
}

func writeConnectionError(w http.ResponseWriter, err error) {
	if strings.HasPrefix(err.Error(), "unsupported") {
		httpx.Error(w, http.StatusBadRequest, "unsupported operation")
		return
	}
	httpx.PrivateError(w, http.StatusBadGateway, "remote operation failed", err)
}

func accessStatusAction(status model.Status) string {
	if status == model.StatusHeld {
		return "connections.hold"
	}
	return "connections.resume"
}

func userStatusAction(status model.Status) string {
	if status == model.StatusHeld {
		return "accounts.hold"
	}
	return "accounts.resume"
}
