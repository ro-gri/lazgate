package admin

import (
	"context"
	"net/http"

	auditsvc "laz/internal/server/audit"
	adminview "laz/internal/server/transport/http/admin/view"
	"laz/internal/server/transport/http/httpx"
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

func (a *App) recordSystemAudit(event auditsvc.Event) {
	if a.audit == nil {
		return
	}
	_, _ = a.audit.Record(context.Background(), event)
}
