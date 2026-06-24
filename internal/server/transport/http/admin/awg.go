package admin

import (
	"net/http"

	"laz/internal/server/adminauth"
	eventsvc "laz/internal/server/events"
	"laz/internal/server/model"
	"laz/internal/server/provisioning"
	store "laz/internal/server/storage"
	"laz/internal/server/transport/http/httpx"
	"laz/internal/server/workqueue"
)

func (a *App) installAWGNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input provisioning.AWGRequest
	if !httpx.Decode(w, r, &input) {
		return
	}
	if a.nodeInstall == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "provisioning unavailable")
		return
	}
	state, err := a.nodeInstall.BootstrapAWG(r.Context(), input, a.publicBaseURL, a.agentGRPCTarget, a.agentDownloadBase)
	if err != nil {
		httpx.ValidationError(w, err)
		return
	}
	id := store.NewID("op")
	principal, _ := adminauth.PrincipalFromContext(r.Context())
	state.EventPrincipal = principal.IdentityID
	if err = a.transport.Enqueue(r.Context(), workqueue.NewAWGInstallMessage(id, state)); err != nil {
		httpx.PrivateError(w, http.StatusInternalServerError, "unable to persist AWG installation", err)
		return
	}
	a.publish(r.Context(), model.Event{Topic: eventsvc.AdminTopic(state.EventPrincipal), Type: "awg.install.scheduled", EntityType: "awg_install", EntityID: id, Message: "AWG installation scheduled", PayloadJSON: jsonString(map[string]any{"operation_id": id, "status": "scheduled"})})
	httpx.JSON(w, http.StatusAccepted, map[string]any{"operation_id": id, "status": "scheduled", "steps": provisioning.AWGSteps})
}
