package adminauth

import (
	"context"
	"strings"
	"testing"
)

func TestPermissionsForUnknownRole(t *testing.T) {
	if permissions := PermissionsForRole(Role("unknown")); len(permissions) != 0 {
		t.Fatalf("expected no permissions for unknown role, got %v", permissions)
	}
}

func TestValidateAdminPasswordPolicy(t *testing.T) {
	tests := []struct {
		name      string
		login     string
		password  string
		wantError string
	}{
		{name: "accepts minimum composed password", login: "owner", password: "abc1!x"},
		{name: "rejects short password", login: "owner", password: "a1!x", wantError: "password must be at least 6 characters"},
		{name: "rejects missing letter", login: "owner", password: "123456!", wantError: "password must include at least one letter, one digit, and one symbol"},
		{name: "rejects missing digit", login: "owner", password: "abcdef!", wantError: "password must include at least one letter, one digit, and one symbol"},
		{name: "rejects missing symbol", login: "owner", password: "abcdef1", wantError: "password must include at least one letter, one digit, and one symbol"},
		{name: "rejects weak list", login: "owner", password: "password", wantError: "password is too weak"},
		{name: "rejects login password", login: "abc1!x", password: "abc1!x", wantError: "password is too weak"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAdminPassword(tt.login, tt.password)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("expected valid password, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
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
