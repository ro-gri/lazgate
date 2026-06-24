package admin

import (
	"net/http"

	"laz/internal/common/httpx"
	auditsvc "laz/internal/services/audit"
	adminview "laz/internal/transport/http/admin/view"
)

func (a *App) auditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": adminview.AuditLogs(a.store.ListAuditLogs())})
}

func (a *App) recordAudit(r *http.Request, event auditsvc.Event) {
	if a.audit == nil {
		return
	}
	_, _ = a.audit.Record(r.Context(), event)
}
