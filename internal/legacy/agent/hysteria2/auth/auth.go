package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	agentstore "laz/internal/agent/common/store"

	"golang.org/x/crypto/bcrypt"
)

type Store interface {
	GetAuthUserByNativeAuthID(context.Context, string) (agentstore.AuthUser, error)
}

type Server struct {
	store Store
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

func New(st Store) *Server {
	return &Server{store: st}
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
	nativeAuthID, password := splitAuth(req.Auth)
	if nativeAuthID == "" || password == "" {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	user, err := s.store.GetAuthUserByNativeAuthID(r.Context(), nativeAuthID)
	if err != nil {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	var meta struct {
		PasswordHash string `json:"password_hash"`
	}
	if json.Unmarshal(user.NativeMeta, &meta) != nil || !verifyPassword(meta.PasswordHash, password) {
		write(w, Response{OK: false, Msg: "not allowed"})
		return
	}
	write(w, Response{OK: true, ID: user.NativeAuthID})
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

func write(w http.ResponseWriter, res Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
