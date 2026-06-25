package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentstore "laz/internal/agent/store"
)

type fakeStore struct {
	users   map[string]agentstore.AuthUser
	pending int64
}

func (f fakeStore) GetAuthUserByUsername(_ context.Context, username string) (agentstore.AuthUser, error) {
	user, ok := f.users[username]
	if !ok {
		return agentstore.AuthUser{}, agentstore.ErrNotFound
	}
	return user, nil
}

func (f fakeStore) PendingUsageForCredential(context.Context, string) (int64, error) {
	return f.pending, nil
}

func TestAuthEndpointAllowedCredentialSucceedsWithCredentialID(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(fakeStore{users: map[string]agentstore.AuthUser{
		"alice": {UserID: "usr_1", CredentialID: "cred_1", Username: "alice", PasswordHash: hash},
	}}, 100).Handler()
	res := postAuth(t, handler, Request{Auth: "alice:secret"})
	if !res.OK || res.ID != "cred_1" {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestAuthEndpointRejectsUnknownInvalidExpiredAndQuotaGuard(t *testing.T) {
	hash, _ := HashPassword("secret")
	cases := []struct {
		name    string
		store   fakeStore
		payload Request
	}{
		{name: "unknown", store: fakeStore{users: map[string]agentstore.AuthUser{}}, payload: Request{Auth: "alice:secret"}},
		{name: "invalid password", store: fakeStore{users: map[string]agentstore.AuthUser{"alice": {Username: "alice", CredentialID: "cred", PasswordHash: hash}}}, payload: Request{Auth: "alice:bad"}},
		{name: "expired", store: fakeStore{users: map[string]agentstore.AuthUser{"alice": {Username: "alice", CredentialID: "cred", PasswordHash: hash, ExpiresAtMS: time.Now().Add(-time.Minute).UnixMilli()}}}, payload: Request{Auth: "alice:secret"}},
		{name: "quota guard", store: fakeStore{pending: 201, users: map[string]agentstore.AuthUser{"alice": {Username: "alice", CredentialID: "cred", PasswordHash: hash, QuotaLimitBytes: 100, QuotaGuardOverageBytes: 100}}}, payload: Request{Auth: "alice:secret"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			res := postAuth(t, New(tt.store, 100).Handler(), tt.payload)
			if res.OK {
				t.Fatalf("expected reject, got %+v", res)
			}
		})
	}
}

func TestQuotaGuardDoesNotRejectAtBoundary(t *testing.T) {
	hash, _ := HashPassword("secret")
	res := postAuth(t, New(fakeStore{pending: 200, users: map[string]agentstore.AuthUser{
		"alice": {Username: "alice", CredentialID: "cred", PasswordHash: hash, QuotaLimitBytes: 100, QuotaGuardOverageBytes: 100},
	}}, 100).Handler(), Request{Auth: "alice:secret"})
	if !res.OK {
		t.Fatalf("expected boundary to be allowed, got %+v", res)
	}
}

func postAuth(t *testing.T, handler http.Handler, payload Request) Response {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var res Response
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}
