package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/server/model"
	store "laz/internal/server/storage"
	clientview "laz/internal/server/transport/http/client/view"
)

type exportSummaryStore struct {
	store.Store
	config model.IssuedConfig
}

func (s exportSummaryStore) Summary(accountID string) (model.AccountSummary, error) {
	if accountID != "owner" {
		return model.AccountSummary{}, store.ErrNotFound
	}
	return model.AccountSummary{Configs: []model.IssuedConfig{s.config}}, nil
}

func TestExistingAmneziaConfigExportsVPNURLAndKeepsOriginal(t *testing.T) {
	conf := "[Interface]\nPrivateKey = private\nAddress = 10.8.1.2/32\nDNS = 1.1.1.1\nMTU = 1280\nJc = 4\n[Peer]\nPublicKey = public\nPresharedKey = psk\nEndpoint = awg.example:51820\nAllowedIPs = 0.0.0.0/0\n"
	config := model.IssuedConfig{ID: "config", ConnectionID: "connection", Name: "Phone", Kind: model.ConfigAmneziaConf, Config: conf, Status: model.StatusActive}
	app := New(Config{Store: exportSummaryStore{config: config}})
	view := clientview.IssuedConfigItem(config)
	if view.Config != conf || !strings.HasPrefix(view.ImportURL, "vpn://") {
		t.Fatal("original configuration or import representation missing")
	}
	ref := qrReference{Kind: qrKindConfig, ID: config.ID}
	value, err := app.resolveQRCodeValue(ref, "owner")
	if err != nil || value != conf {
		t.Fatalf("raw representation changed: %v", err)
	}
	if _, err := app.resolveQRCodeValue(ref, "other"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("another account's config must not be exportable: %v", err)
	}
	response := httptest.NewRecorder()
	app.writeReferencedQRCode(response, ref, "owner")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatal("expected unchanged raw configuration QR")
	}
}
