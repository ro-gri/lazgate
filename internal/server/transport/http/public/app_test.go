package public

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRobotsDisallowsAllCrawlers(t *testing.T) {
	router := chi.NewRouter()
	router.Use(NoIndexMiddleware)
	New(Config{}).Mount(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got, want := response.Body.String(), "User-agent: *\nDisallow: /\n"; got != want {
		t.Fatalf("robots.txt = %q, want %q", got, want)
	}
	if got := response.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
}
