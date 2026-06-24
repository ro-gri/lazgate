package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"laz/internal/server/amneziasettings"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
	adminview "laz/internal/server/transport/http/admin/view"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) amneziaSettings(w http.ResponseWriter, r *http.Request, nodeID string) {
	node, err := a.store.GetNode(nodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if node.Type != model.NodeTypeNativeAmneziaWG || node.Status == model.StatusDeleted {
		httpx.Error(w, http.StatusConflict, "Настройки недоступны для этого сервера")
		return
	}
	c, err := a.store.GetAmneziaWGNodeConfig(nodeID)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		httpx.JSON(w, http.StatusOK, adminview.AmneziaSettingsItem(c))
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input amneziasettings.Input
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 800000))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
		httpx.Error(w, 400, "Некорректные параметры настроек")
		return
	}
	if input.Version != c.ConfigVersionMS {
		httpx.Error(w, 409, "Настройки уже изменены. Откройте форму заново")
		return
	}
	c, err = input.Apply(c)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	c, err = amneziasettings.Save(a.store, c, input.Version)
	var validation amneziasettings.ValidationError
	if errors.As(err, &validation) {
		httpx.Error(w, 400, validation.Error())
		return
	}
	if errors.Is(err, store.ErrConflict) {
		httpx.Error(w, 409, "Настройки уже изменены. Откройте форму заново")
		return
	}
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	a.recordAudit(r, auditsvc.Event{Action: "nodes.settings.update", EntityType: "node", EntityID: nodeID, Details: map[string]any{"revision": c.ConfigVersionMS, "header_key_rotated": input.HeaderKey != nil}})
	httpx.JSON(w, http.StatusAccepted, adminview.AmneziaSettingsItem(c))
}
