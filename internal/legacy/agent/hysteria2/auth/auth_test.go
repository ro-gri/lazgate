package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentstore "laz/internal/agent/common/store"
)

type fakeStore struct {
	users map[string]agentstore.AuthUser
}

func (f fakeStore) GetAuthUserByNativeAuthID(_ context.Context, connectionID string) (agentstore.AuthUser, error) {
	user, ok := f.users[connectionID]
	if !ok {
		return agentstore.AuthUser{}, agentstore.ErrNotFound
	}
	return user, nil
}

func TestAuthEndpointAllowedCredentialSucceedsWithConnectionID(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	connectionID := "22222222-2222-4222-8222-222222222222"
	nativeID := "native-user"
	handler := New(fakeStore{users: map[string]agentstore.AuthUser{
		nativeID: {ConnectionID: connectionID, NativeAuthID: nativeID, NativeMeta: []byte(`{"password_hash":"` + hash + `"}`)},
	}}).Handler()
	res := postAuth(t, handler, Request{Auth: nativeID + ":secret"})
	if !res.OK || res.ID != nativeID {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestAuthEndpointRejectsUnknownAndInvalidCredentials(t *testing.T) {
	hash, _ := HashPassword("secret")
	cases := []struct {
		name    string
		store   fakeStore
		payload Request
	}{
		{name: "unknown", store: fakeStore{users: map[string]agentstore.AuthUser{}}, payload: Request{Auth: "cred:secret"}},
		{name: "invalid password", store: fakeStore{users: map[string]agentstore.AuthUser{"cred": {ConnectionID: "cred", NativeAuthID: "cred", NativeMeta: []byte(`{"password_hash":"` + hash + `"}`)}}}, payload: Request{Auth: "cred:bad"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			res := postAuth(t, New(tt.store).Handler(), tt.payload)
			if res.OK {
				t.Fatalf("expected reject, got %+v", res)
			}
		})
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
