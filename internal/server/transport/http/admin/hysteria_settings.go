package admin

import (
	"encoding/json"
	"errors"
	"io"
	"laz/internal/protocol/hy2config"
	"net/http"

	auditsvc "laz/internal/server/audit"
	"laz/internal/server/hysteriasettings"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) hysteriaSettings(w http.ResponseWriter, r *http.Request, id string) {
	node, err := a.store.GetNode(id)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if node.Type != model.NodeTypeNativeHy2 || node.Status == model.StatusDeleted {
		httpx.Error(w, 409, "Настройки недоступны для этого сервера")
		return
	}
	c, err := a.store.GetHysteria2NodeConfig(id)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	if c.SettingsJSON == "" {
		httpx.Error(w, 409, "Снимок настроек отсутствует. Нельзя безопасно редактировать сервер без его фактической конфигурации")
		return
	}
	if r.Method == http.MethodGet {
		v, e := hysteriasettings.Public(a.store, c)
		if e != nil {
			httpx.StoreError(w, e)
			return
		}
		httpx.JSON(w, 200, v)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(405)
		return
	}
	var in hysteriasettings.Input
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512000))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		httpx.Error(w, 400, "Некорректные параметры настроек")
		return
	}
	if in.Version != c.ConfigVersionMS {
		httpx.Error(w, 409, "Настройки уже изменены. Откройте форму заново")
		return
	}
	if err := hysteriasettings.PrepareFiles(a.store, in.FileSetID, &in.Settings); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	// Validate user input before persistence, so storage failures never leak internals.
	candidate := in.Settings
	if candidate.Obfs.Password == "" {
		candidate.Obfs.Password = c.ObfsPassword
	}
	if err := validateHysteriaInput(in, candidate); err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	c, err = hysteriasettings.Save(a.store, c, in)
	var validation hysteriasettings.ValidationError
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
	a.recordAudit(r, auditsvc.Event{Action: "nodes.settings.update", EntityType: "node", EntityID: id, Details: map[string]any{"revision": c.ConfigVersionMS}})
	v, err := hysteriasettings.Public(a.store, c)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	httpx.JSON(w, 202, v)
}

func validateHysteriaInput(in hysteriasettings.Input, c hy2config.Config) error {
	if !hy2config.Domain(in.EndpointHost) || in.EndpointPort < 1 || in.EndpointPort > 65535 {
		return errors.New("Укажите корректный публичный домен и порт 1–65535")
	}
	return hy2config.Validate(c)
}
