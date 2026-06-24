package public

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type App struct {
	blankPageHTML string
}

type Config struct {
	BlankPageHTML string
}

func New(config Config) *App {
	return &App{blankPageHTML: strings.TrimSpace(config.BlankPageHTML)}
}

func (a *App) Mount(router chi.Router) {
	router.HandleFunc("/healthz", a.health)
	router.HandleFunc("/robots.txt", a.robots)
	router.HandleFunc("/", a.blankPage)
}

func NoIndexMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		next.ServeHTTP(w, r)
	})
}

func NotFound(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func (a *App) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (a *App) robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Account-agent: *\nDisallow: /\n"))
}

func (a *App) blankPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	html := a.blankPageHTML
	if html == "" {
		html = defaultBlankPageHTML
	}
	_, _ = w.Write([]byte(html))
}

const defaultBlankPageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex,nofollow,noarchive">
  <title>laz</title>
  <style>
    html { color-scheme: light dark; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0f1115;
      color: #e8e8ea;
    }
    main {
      width: min(90vw, 34rem);
      line-height: 1.5;
    }
    h1 {
      margin: 0 0 0.75rem;
      font-size: 1.35rem;
      font-weight: 650;
    }
    p {
      margin: 0.35rem 0;
      color: #b9bbc3;
    }
  </style>
</head>
<body>
  <main>
    <h1>Blank page</h1>
    <p>This service is intentionally not public.</p>
  </main>
</body>
</html>
`
