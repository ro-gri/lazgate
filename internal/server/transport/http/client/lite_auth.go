package client

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	commontokens "laz/internal/server/security/tokens"
	"laz/internal/server/transport/http/httpx"
)

const (
	liteChallengeTTL      = 5 * time.Minute
	liteAccessTTL         = 15 * time.Minute
	liteChallengePoWBits  = 18
	liteChallengePurpose  = "lite-challenge-v1"
	liteCookiePurpose     = "lite-cookie-v1"
	liteCSRFPurpose       = "lite-csrf-v1"
	liteCookieName        = "laz_lite_session"
	liteChallengePosition = 4
)

var errLiteInvalid = errors.New("invalid lite auth")

type liteChallenge struct {
	TokenHash string `json:"token_hash"`
	Positions []int  `json:"positions"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expires_at"`
	Signature string `json:"signature"`
}

func (a *App) newLiteChallenge(rawToken string, username string) (liteChallenge, error) {
	positions, err := challengePositions(username)
	if err != nil {
		return liteChallenge{}, err
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return liteChallenge{}, err
	}
	challenge := liteChallenge{
		TokenHash: commontokens.Hash(rawToken),
		Positions: positions,
		Nonce:     base64.RawURLEncoding.EncodeToString(nonceBytes),
		ExpiresAt: time.Now().UTC().Add(liteChallengeTTL).Unix(),
	}
	challenge.Signature = a.sign(liteChallengePurpose, challengePayload(challenge))
	return challenge, nil
}

func challengePositions(username string) ([]int, error) {
	runes := []rune(strings.TrimSpace(username))
	if len(runes) < liteChallengePosition {
		return nil, errLiteInvalid
	}
	seed := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	seen := map[int]bool{}
	positions := make([]int, 0, liteChallengePosition)
	for i := 0; len(positions) < liteChallengePosition; i++ {
		idx := int(seed[i%len(seed)]) % len(runes)
		if seen[idx] {
			continue
		}
		seen[idx] = true
		positions = append(positions, idx)
	}
	slices.Sort(positions)
	return positions, nil
}

func (a *App) verifyLiteChallenge(rawToken, username, answer, powNonce string, challenge liteChallenge) bool {
	if time.Now().UTC().Unix() > challenge.ExpiresAt || challenge.TokenHash != commontokens.Hash(rawToken) {
		return false
	}
	if !hmac.Equal([]byte(challenge.Signature), []byte(a.sign(liteChallengePurpose, challengePayload(challenge)))) {
		return false
	}
	positions, err := challengePositions(username)
	if err != nil || fmt.Sprint(positions) != fmt.Sprint(challenge.Positions) {
		return false
	}
	runes := []rune(strings.ToLower(strings.TrimSpace(username)))
	want := make([]rune, 0, len(challenge.Positions))
	for _, position := range challenge.Positions {
		if position < 0 || position >= len(runes) {
			return false
		}
		want = append(want, runes[position])
	}
	if !hmac.Equal([]byte(string(want)), []byte(strings.ToLower(strings.TrimSpace(answer)))) {
		return false
	}
	return validPoW(challenge.Signature, powNonce, liteChallengePoWBits)
}

func challengePayload(challenge liteChallenge) string {
	parts := make([]string, 0, len(challenge.Positions))
	for _, position := range challenge.Positions {
		parts = append(parts, strconv.Itoa(position))
	}
	return strings.Join([]string{challenge.TokenHash, strings.Join(parts, ","), challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)}, "|")
}

func validPoW(signature string, nonce string, bits int) bool {
	sum := sha256.Sum256([]byte(signature + ":" + strings.TrimSpace(nonce)))
	fullBytes := bits / 8
	remaining := bits % 8
	for i := 0; i < fullBytes; i++ {
		if sum[i] != 0 {
			return false
		}
	}
	if remaining == 0 {
		return true
	}
	mask := byte(0xff << (8 - remaining))
	return sum[fullBytes]&mask == 0
}

func (a *App) issueLiteCookie(w http.ResponseWriter, r *http.Request, rawToken string) string {
	expiresAt := time.Now().UTC().Add(liteAccessTTL).Unix()
	value := a.signedValue(liteCookiePurpose, commontokens.Hash(rawToken), expiresAt)
	cookie := &http.Cookie{
		Name:     liteCookieName,
		Value:    value,
		Path:     "/client/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   int(liteAccessTTL.Seconds()),
	}
	http.SetCookie(w, cookie)
	return a.newLiteCSRF(value)
}

func (a *App) clearLiteCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: liteCookieName, Value: "", Path: "/client/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (a *App) authenticateLite(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie(liteCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		httpx.Error(w, http.StatusUnauthorized, "lite cookie required")
		return "", false
	}
	parts := strings.Split(cookie.Value, ":")
	if len(parts) != 3 {
		httpx.Error(w, http.StatusUnauthorized, "invalid lite cookie")
		return "", false
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().UTC().Unix() > expiresAt || !hmac.Equal([]byte(parts[2]), []byte(a.sign(liteCookiePurpose, parts[0]+":"+parts[1]))) {
		httpx.Error(w, http.StatusUnauthorized, "invalid lite cookie")
		return "", false
	}
	if !a.validateLiteCSRF(cookie.Value, r.Header.Get("X-CSRF-Token")) {
		httpx.Error(w, http.StatusForbidden, "csrf token required")
		return "", false
	}
	return parts[0], true
}

func (a *App) newLiteCSRF(cookieValue string) string {
	expiresAt := time.Now().UTC().Add(liteAccessTTL).Unix()
	return a.signedValue(liteCSRFPurpose, commontokens.Hash(cookieValue), expiresAt)
}

func (a *App) validateLiteCSRF(cookieValue, csrf string) bool {
	parts := strings.Split(strings.TrimSpace(csrf), ":")
	if len(parts) != 3 || parts[0] != commontokens.Hash(cookieValue) {
		return false
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().UTC().Unix() > expiresAt {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(a.sign(liteCSRFPurpose, parts[0]+":"+parts[1])))
}

func (a *App) signedValue(purpose, subject string, expiresAt int64) string {
	payload := subject + ":" + strconv.FormatInt(expiresAt, 10)
	return payload + ":" + a.sign(purpose, payload)
}

func (a *App) sign(purpose, payload string) string {
	mac := hmac.New(sha256.New, a.signingKey(purpose))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (a *App) signingKey(purpose string) []byte {
	secret := strings.TrimSpace(a.secret)
	if secret == "" {
		secret = "laz-lite-development-secret"
	}
	sum := sha256.Sum256([]byte("laz:" + purpose + ":" + secret))
	return sum[:]
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
