package models

import (
	"testing"
	"time"
)

func TestNewAPITokenScope(t *testing.T) {
	if got := NewAPITokenScope(ResourceApplication, ActionDeploy); got != "application:deploy" {
		t.Errorf("NewAPITokenScope() = %q, want %q", got, "application:deploy")
	}
}

func TestAPITokenScopePermission(t *testing.T) {
	tests := []struct {
		scope        APITokenScope
		wantResource PermissionResource
		wantAction   PermissionAction
		wantOk       bool
	}{
		{"application:deploy", ResourceApplication, ActionDeploy, true},
		{"repository:sync", ResourceRepository, ActionSync, true},
		{APITokenScopeAdmin, "", "", false},
		{"application", "", "", false},
		{":deploy", "", "", false},
		{"application:", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			resource, action, ok := tt.scope.Permission()
			if resource != tt.wantResource || action != tt.wantAction || ok != tt.wantOk {
				t.Errorf("Permission() = (%q, %q, %v), want (%q, %q, %v)",
					resource, action, ok, tt.wantResource, tt.wantAction, tt.wantOk)
			}
		})
	}
}

func TestIsValidAPITokenScope(t *testing.T) {
	tests := []struct {
		scope APITokenScope
		want  bool
	}{
		{APITokenScopeAdmin, true},
		{"application:deploy", true},
		{"repository:sync", true},
		{"notification:read", true},
		{"agent:deploy", false},
		{"application:sync", false},
		{"cluster:read", false},
		{"application", false},
		{"Admin", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			if got := IsValidAPITokenScope(tt.scope); got != tt.want {
				t.Errorf("IsValidAPITokenScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestIsValidAPITokenScope_AllPermissions(t *testing.T) {
	for resource, actions := range PermissionActions {
		for _, action := range actions {
			if scope := NewAPITokenScope(resource, action); !IsValidAPITokenScope(scope) {
				t.Errorf("expected scope %q to be valid", scope)
			}
		}
	}
}

func TestAPITokenScopes(t *testing.T) {
	scopes := APITokenScopes()

	if scopes[0] != APITokenScopeAdmin {
		t.Errorf("expected the admin scope first, got %q", scopes[0])
	}
	if scopes[1] != NewAPITokenScope(ResourceApplication, ActionRead) {
		t.Errorf("expected application:read second, got %q", scopes[1])
	}

	want := 1
	for _, actions := range PermissionActions {
		want += len(actions)
	}
	if len(scopes) != want {
		t.Errorf("expected %d scopes, got %d", want, len(scopes))
	}

	seen := make(map[APITokenScope]bool)
	for _, scope := range scopes {
		if !IsValidAPITokenScope(scope) {
			t.Errorf("expected scope %q to be valid", scope)
		}
		if seen[scope] {
			t.Errorf("scope %q is listed twice", scope)
		}
		seen[scope] = true
	}
}

func TestAPITokenIsExpired(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"in the future", now.Add(time.Hour), false},
		{"in the past", now.Add(-time.Hour), true},
		{"exactly now", now, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := APIToken{ExpiresAt: tt.expiresAt}
			if got := token.IsExpired(now); got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAPITokenHasScope(t *testing.T) {
	token := APIToken{Scopes: []APITokenScope{"application:read", "application:deploy"}}

	if !token.HasScope(NewAPITokenScope(ResourceApplication, ActionDeploy)) {
		t.Error("expected token to have application:deploy")
	}
	if token.HasScope(NewAPITokenScope(ResourceApplication, ActionDelete)) {
		t.Error("expected token not to have application:delete")
	}
	if token.HasScope(APITokenScopeAdmin) {
		t.Error("expected token not to have the admin scope")
	}
}

func TestAPITokenTableName(t *testing.T) {
	if got := (APIToken{}).TableName(); got != "api_tokens" {
		t.Errorf("TableName() = %q, want %q", got, "api_tokens")
	}
}
