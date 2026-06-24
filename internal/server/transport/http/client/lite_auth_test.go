package client

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLiteChallengeSigningExpiryAndPoW(t *testing.T) {
	app := New(Config{Secret: "test-secret"})
	challenge, err := app.newLiteChallenge("raw-token", "qwerty")
	if err != nil {
		t.Fatal(err)
	}
	if len(challenge.Positions) != 4 || challenge.Signature == "" || challenge.TokenHash == "" {
		t.Fatalf("unexpected challenge: %+v", challenge)
	}
	answer := answerForPositions("qwerty", challenge.Positions)
	pow := solvePoWForTest(challenge.Signature, liteChallengePoWBits)
	if !app.verifyLiteChallenge("raw-token", "qwerty", answer, pow, challenge) {
		t.Fatal("expected signed challenge and pow to verify")
	}
	challenge.ExpiresAt = time.Now().UTC().Add(-time.Second).Unix()
	if app.verifyLiteChallenge("raw-token", "qwerty", answer, pow, challenge) {
		t.Fatal("expected expired challenge to fail")
	}
}

func TestLiteChallengeRejectsShortUsername(t *testing.T) {
	app := New(Config{Secret: "test-secret"})
	if _, err := app.newLiteChallenge("raw-token", "abc"); err == nil {
		t.Fatal("expected short username to be rejected")
	}
}

func TestLiteChallengePositionsAreStrictlyIncreasing(t *testing.T) {
	for _, username := range []string{"qwerty", "mrgrien", "phone-01"} {
		t.Run(username, func(t *testing.T) {
			app := New(Config{Secret: "test-secret"})
			challenge, err := app.newLiteChallenge("raw-token", username)
			if err != nil {
				t.Fatal(err)
			}
			if len(challenge.Positions) != liteChallengePosition {
				t.Fatalf("positions = %v", challenge.Positions)
			}
			for index, position := range challenge.Positions {
				if position < 0 || position >= len([]rune(username)) {
					t.Fatalf("out-of-range position: %d", position)
				}
				if index > 0 && challenge.Positions[index-1] >= position {
					t.Fatalf("positions are not strictly increasing: %v", challenge.Positions)
				}
			}
		})
	}
}

func TestLiteCookieAndCSRF(t *testing.T) {
	app := New(Config{Secret: "test-secret"})
	req := httptest.NewRequest(http.MethodPost, "/client/v1/qr", nil)
	rr := httptest.NewRecorder()
	csrf := app.issueLiteCookie(rr, req, "raw-token")
	if csrf == "" {
		t.Fatal("expected csrf token")
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	if cookies[0].Name != liteCookieName || cookies[0].Path != "/client/" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected lite cookie: %+v", cookies[0])
	}

	missingCSRFReq := httptest.NewRequest(http.MethodPost, "/client/v1/qr", nil)
	missingCSRFReq.AddCookie(cookies[0])
	missingCSRFRR := httptest.NewRecorder()
	if _, ok := app.authenticateLite(missingCSRFRR, missingCSRFReq); ok || missingCSRFRR.Code != http.StatusForbidden {
		t.Fatalf("expected csrf rejection, ok=%v code=%d", ok, missingCSRFRR.Code)
	}

	validReq := httptest.NewRequest(http.MethodPost, "/client/v1/qr", nil)
	validReq.AddCookie(cookies[0])
	validReq.Header.Set("X-CSRF-Token", csrf)
	validRR := httptest.NewRecorder()
	if tokenHash, ok := app.authenticateLite(validRR, validReq); !ok || tokenHash == "" {
		t.Fatalf("expected valid lite auth, tokenHash=%q ok=%v code=%d", tokenHash, ok, validRR.Code)
	}
}

func answerForPositions(username string, positions []int) string {
	runes := []rune(username)
	out := make([]rune, 0, len(positions))
	for _, position := range positions {
		out = append(out, runes[position])
	}
	return string(out)
}

func solvePoWForTest(signature string, bits int) string {
	for nonce := 0; ; nonce++ {
		candidate := strconv.Itoa(nonce)
		if validPoW(signature, candidate, bits) {
			return candidate
		}
	}
}
