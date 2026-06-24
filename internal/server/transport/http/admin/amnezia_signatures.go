package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"laz/internal/server/integrations/nativeawg/cps"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) generateAWGSignatures(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Count      int         `json:"count"`
		Profile    cps.Profile `json:"profile"`
		Service    cps.Service `json:"service"`
		ServerName string      `json:"server_name"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Некорректные параметры генерации")
		return
	}
	if decoder.Decode(new(any)) != io.EOF || input.Count < 1 || input.Count > cps.PacketCount {
		httpx.Error(w, http.StatusBadRequest, "Количество должно быть от 1 до 5")
		return
	}
	chain, err := cps.Generate(r.Context(), cps.Options{Profile: input.Profile, Service: input.Service, ServerName: input.ServerName})
	if errors.Is(err, cps.ErrInvalidOptions) {
		httpx.Error(w, http.StatusBadRequest, "Некорректный профиль, сервис или домен")
		return
	}
	if err != nil {
		if r.Context().Err() == nil {
			httpx.PrivateError(w, http.StatusInternalServerError, "Не удалось сгенерировать сигнатуры", err)
		}
		return
	}
	signatures := make([]string, input.Count)
	for i := range signatures {
		signatures[i] = chain.Signatures[i].CPS
	}
	httpx.JSON(w, http.StatusOK, struct {
		Signatures []string `json:"signatures"`
	}{signatures})
}
