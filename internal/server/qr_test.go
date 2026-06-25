package server

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/server/transport/http/httpx"
)

func TestWriteQRCodePNG(t *testing.T) {
	rr := httptest.NewRecorder()

	httpx.QRCodePNG(rr, "https://example.com/connect/test")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("expected image/png, got %q", got)
	}
	body := rr.Body.Bytes()
	if len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("expected PNG signature, got %q", body[:min(len(body), 8)])
	}
}

func TestWriteQRCodePNGRejectsEmptyValue(t *testing.T) {
	rr := httptest.NewRecorder()

	httpx.QRCodePNG(rr, " ")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestAmneziaQRCodeSeriesFramesVpnConfig(t *testing.T) {
	raw := append([]byte{0, 0, 0, 5}, []byte("hello")...)
	config := "vpn://" + base64.RawURLEncoding.EncodeToString(raw)

	items, err := httpx.AmneziaQRPayloadSeries(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one QR payload, got %d", len(items))
	}
	frame, err := base64.RawURLEncoding.DecodeString(items[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(frame[0:2]); got != 1984 {
		t.Fatalf("unexpected magic %d", got)
	}
	if got := frame[2]; got != 1 {
		t.Fatalf("unexpected chunks count %d", got)
	}
	if got := frame[3]; got != 0 {
		t.Fatalf("unexpected chunk index %d", got)
	}
	if got := binary.BigEndian.Uint32(frame[4:8]); got != uint32(len(raw)) {
		t.Fatalf("unexpected chunk length %d", got)
	}
	if string(frame[8:]) != string(raw) {
		t.Fatalf("unexpected payload %q", frame[8:])
	}
}

func TestWriteQRCodePNGReturnsJSONForAmneziaConfig(t *testing.T) {
	raw := append([]byte{0, 0, 0, 5}, []byte("hello")...)
	config := "vpn://" + base64.RawURLEncoding.EncodeToString(raw)
	rr := httptest.NewRecorder()

	httpx.QRCodePNG(rr, config)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected application/json, got %q", got)
	}
	if !strings.Contains(rr.Body.String(), "data:image/png;base64,") {
		t.Fatalf("expected data uri QR series, got %q", rr.Body.String())
	}
}
