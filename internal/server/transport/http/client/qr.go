package client

import (
	"errors"
	"net/http"
	"strings"

	"laz/internal/server/model"
	"laz/internal/server/storage"
	"laz/internal/server/transport/http/httpx"
)

const (
	qrKindConfig = "config"
	qrKindHPLink = "hp_link"
)

type qrReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (a *App) writeReferencedQRCode(w http.ResponseWriter, ref qrReference, accountID string) {
	value, err := a.resolveQRCodeValue(ref, accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "qr reference not found")
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.QRCodePNG(w, value)
}

func (a *App) resolveQRCodeValue(ref qrReference, accountID string) (string, error) {
	kind := strings.TrimSpace(ref.Kind)
	id := strings.TrimSpace(ref.ID)
	if kind == "" || id == "" {
		return "", errors.New("qr kind and id are required")
	}
	summary, err := a.store.Summary(accountID)
	if err != nil {
		return "", err
	}
	switch kind {
	case qrKindConfig:
		for _, cfg := range summary.Configs {
			if cfg.ID != id || cfg.Status != model.StatusActive || cfg.Kind == model.ConfigHy2URI {
				continue
			}
			if strings.TrimSpace(cfg.Config) == "" {
				return "", errors.New("qr config is empty")
			}
			return cfg.Config, nil
		}
		return "", store.ErrNotFound
	case qrKindHPLink:
		shortLink, err := a.store.GetShortLink(id)
		if err != nil {
			return "", err
		}
		if shortLink.Status != model.StatusActive || strings.TrimSpace(shortLink.EncryptedURL) == "" {
			return "", store.ErrNotFound
		}
		for _, token := range a.store.ListConnectionSubscriptionTokens() {
			if token.ID != shortLink.SubscriptionTokenID || token.Status != model.StatusActive {
				continue
			}
			connection, err := a.store.GetConnection(token.ConnectionID)
			if err != nil || connection.AccountID != accountID {
				continue
			}
			return shortLink.EncryptedURL, nil
		}
		return "", store.ErrNotFound
	default:
		return "", errors.New("unsupported qr kind")
	}
}
