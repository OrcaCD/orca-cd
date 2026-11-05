package oidc

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestGroupsFromClaims(t *testing.T) {
	var claims map[string]any
	if err := json.Unmarshal([]byte(`{
		"groups": ["devs", " ops ", "devs", "", 42],
		"single": "admins",
		"realm_access": {"roles": ["deployers"]},
		"empty": []
	}`), &claims); err != nil {
		t.Fatalf("invalid claims: %v", err)
	}

	tests := []struct {
		path      string
		want      []string
		wantFound bool
	}{
		{"groups", []string{"devs", "ops"}, true},
		{"single", []string{"admins"}, true},
		{"realm_access.roles", []string{"deployers"}, true},
		{"empty", []string{}, true},
		{"missing", nil, false},
		{"realm_access.missing", nil, false},
		{"single.nested", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, found := groupsFromClaims(claims, tt.path)
			if found != tt.wantFound || !slices.Equal(got, tt.want) {
				t.Fatalf("groupsFromClaims(%q) = %v, %v; want %v, %v", tt.path, got, found, tt.want, tt.wantFound)
			}
		})
	}
}
