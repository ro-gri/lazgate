package httpx

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
	"strings"

	"laz/internal/common/apperrors"
	"laz/internal/storage"

	qrcode "github.com/skip2/go-qrcode"
)

func Decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func StoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		Error(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, store.ErrDuplicateConnection) {
		Error(w, http.StatusConflict, "connection already exists for this account, client and node")
		return
	}
	PrivateError(w, http.StatusInternalServerError, "internal error", err)
}

func ServiceError(w http.ResponseWriter, err error) {
	var validation apperrors.ValidationError
	if errors.As(err, &validation) {
		ValidationError(w, err)
		return
	}
	StoreError(w, err)
}

func ValidationError(w http.ResponseWriter, err error) {
	PrivateError(w, http.StatusBadRequest, "invalid request", err)
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func QRCodePNG(w http.ResponseWriter, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		Error(w, http.StatusBadRequest, "value is required")
		return
	}
	if strings.HasPrefix(strings.ToLower(value), "vpn://") {
		items, err := amneziaQRCodeSeries(value)
		if err != nil {
			PrivateError(w, http.StatusBadRequest, "invalid QR value", err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		JSON(w, http.StatusOK, map[string]any{
			"total": len(items),
			"items": items,
		})
		return
	}
	if len([]byte(value)) > 2900 {
		Error(w, http.StatusBadRequest, "value is too large for a QR code")
		return
	}
	png, err := encodeQRCodePNG(value)
	if err != nil {
		PrivateError(w, http.StatusBadRequest, "invalid QR value", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

func HTMLJSON(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

func PrivateError(w http.ResponseWriter, status int, message string, err error) string {
	id := LogError(status, err)
	JSON(w, status, map[string]string{"error": message, "error_id": id})
	return id
}

func LogError(status int, err error) string {
	id := NewErrorID()
	if err != nil {
		log.Printf("error_id=%s status=%d error=%v", id, status, err)
	}
	return id
}

func NewErrorID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "err_" + strings.ReplaceAll(fmt.Sprintf("%d", len(raw)), "-", "")
	}
	return "err_" + hex.EncodeToString(raw[:])
}

func amneziaQRCodeSeries(config string) ([]string, error) {
	payloads, err := AmneziaQRPayloadSeries(config)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payloads))
	for _, qrPayload := range payloads {
		png, err := encodeQRCodePNG(qrPayload)
		if err != nil {
			return nil, err
		}
		out = append(out, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png))
	}
	return out, nil
}

func encodeQRCodePNG(value string) ([]byte, error) {
	const size = 288
	const quietZone = 2
	qr, err := qrcode.New(value, qrcode.Low)
	if err != nil {
		return nil, err
	}
	qr.DisableBorder = true
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	src := qr.Image(size - quietZone*2)
	draw.Draw(dst, image.Rect(quietZone, quietZone, size-quietZone, size-quietZone), src, image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func AmneziaQRPayloadSeries(config string) ([]string, error) {
	config = strings.TrimSpace(config)
	if !strings.HasPrefix(strings.ToLower(config), "vpn://") {
		return nil, fmt.Errorf("invalid amnezia config")
	}
	payload := config[len("vpn://"):]
	if payload == "" {
		return nil, fmt.Errorf("invalid amnezia config")
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode amnezia config: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("invalid amnezia config")
	}
	const (
		magicCode = 1984
		chunkSize = 850
	)
	chunks := (len(data) + chunkSize - 1) / chunkSize
	if chunks > 255 {
		return nil, fmt.Errorf("amnezia config is too large for QR series")
	}
	out := make([]string, 0, chunks)
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunkIndex := offset / chunkSize
		chunk := data[offset:end]
		frame := make([]byte, 8+len(chunk))
		binary.BigEndian.PutUint16(frame[0:2], magicCode)
		frame[2] = byte(chunks)
		frame[3] = byte(chunkIndex)
		binary.BigEndian.PutUint32(frame[4:8], uint32(len(chunk)))
		copy(frame[8:], chunk)
		qrPayload := base64.RawURLEncoding.EncodeToString(frame)
		out = append(out, qrPayload)
	}
	return out, nil
}
