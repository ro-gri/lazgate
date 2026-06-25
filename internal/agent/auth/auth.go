package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	agentstore "laz/internal/agent/store"

	"golang.org/x/crypto/bcrypt"
)

type Store interface {
	GetAuthUserByUsername(context.Context, string) (agentstore.AuthUser, error)
	PendingUsageForCredential(context.Context, string) (int64, error)
}

type Server struct {
	store             Store
	defaultGuardBytes int64
}

type Request struct {
	Addr string `json:"addr"`
	Auth string `json:"auth"`
	TX   int64  `json:"tx"`
}

type Response struct {
	OK  bool   `json:"ok"`
	ID  string `json:"id,omitempty"`
	Msg string `json:"msg,omitempty"`
}

func New(st Store, defaultGuardBytes int64) *Server {
	return &Server{store: st, defaultGuardBytes: defaultGuardBytes}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", s.auth)
	return mux
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	username, password := splitAuth(req.Auth)
	if username == "" || password == "" {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	user, err := s.store.GetAuthUserByUsername(r.Context(), username)
	if err != nil {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	if user.ExpiresAtMS > 0 && time.Now().UnixMilli() >= user.ExpiresAtMS {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	if !verifyPassword(user.PasswordHash, password) {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	if exceeded, _ := s.quotaExceeded(r.Context(), user); exceeded {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	write(w, Response{OK: true, ID: user.CredentialID})
}

func splitAuth(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	before, after, ok := strings.Cut(raw, ":")
	if !ok {
		return "", raw
	}
	return before, after
}

func verifyPassword(hash, password string) bool {
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(password)) == 1
}

func HashPassword(password string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(raw), err
}

func (s *Server) quotaExceeded(ctx context.Context, user agentstore.AuthUser) (bool, error) {
	if user.QuotaLimitBytes <= 0 {
		return false, nil
	}
	guard := user.QuotaGuardOverageBytes
	if guard == 0 {
		guard = s.defaultGuardBytes
	}
	pending, err := s.store.PendingUsageForCredential(ctx, user.CredentialID)
	if err != nil {
		return false, err
	}
	return user.LastKnownGlobalUsageBytes+pending > user.QuotaLimitBytes+guard, nil
}

func write(w http.ResponseWriter, res Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
