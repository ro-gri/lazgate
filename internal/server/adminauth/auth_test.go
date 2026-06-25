package adminauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
)

func TestAuthenticateRequestToken(t *testing.T) {
	auth := New(Config{Token: "secret"})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")

	principal, ok := auth.AuthenticateRequest(req)
	if !ok {
		t.Fatal("expected token to authenticate")
	}
	if principal.Role != RoleOwner {
		t.Fatalf("unexpected role: %s", principal.Role)
	}
	if !HasPermission(principal, PermissionAdminRead) {
		t.Fatal("expected owner to have admin read permission")
	}
}

func TestPermissionsForUnknownRole(t *testing.T) {
	if permissions := PermissionsForRole(Role("unknown")); len(permissions) != 0 {
		t.Fatalf("expected no permissions for unknown role, got %v", permissions)
	}
}

func TestAuthorize(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{
		Name:        "admin",
		Role:        RoleOwner,
		Permissions: []Permission{PermissionAdminRead},
	})

	if _, ok := Authorize(ctx, PermissionAdminRead); !ok {
		t.Fatal("expected permission to authorize")
	}
	if _, ok := Authorize(ctx, PermissionUsersWrite); ok {
		t.Fatal("expected missing permission to be denied")
	}
}

func TestAuthenticateRequestRejectsWrongToken(t *testing.T) {
	auth := New(Config{Token: "secret"})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")

	if _, ok := auth.AuthenticateRequest(req); ok {
		t.Fatal("expected wrong token to be rejected")
	}
}

func TestAuthenticateRequestTokenHash(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	auth := New(Config{TokenSHA256: hex.EncodeToString(sum[:])})
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")

	if _, ok := auth.AuthenticateRequest(req); !ok {
		t.Fatal("expected hashed token to authenticate")
	}
}
