package oidc

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestGroupsFromClaims(t *testing.T) {
	var claims map[string]any
	if err := json.Unmarshal([]byte(`{
		"groups": ["devs", " ops ", "devs", ""],
		"mixed": ["devs", 42],
		"object": {"devs": true},
		"https://example.com/groups": ["namespaced"],
		"null": null,
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
		wantErr   bool
	}{
		{"groups", []string{"devs", "ops"}, true, false},
		{"single", []string{"admins"}, true, false},
		{"realm_access.roles", []string{"deployers"}, true, false},
		{"empty", []string{}, true, false},
		{"missing", nil, false, false},
		{"realm_access.missing", nil, false, false},
		{"single.nested", nil, false, false},
		{"https://example.com/groups", []string{"namespaced"}, true, false},
		{"null", []string{}, true, false},
		{"mixed", nil, true, true},
		{"object", nil, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, found, err := groupsFromClaims(claims, tt.path)
			if found != tt.wantFound || !slices.Equal(got, tt.want) || (err != nil) != tt.wantErr {
				t.Fatalf("groupsFromClaims(%q) = %v, %v, %v; want %v, %v, err=%v", tt.path, got, found, err, tt.want, tt.wantFound, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrGroupsUnavailable) {
				t.Fatalf("expected ErrGroupsUnavailable, got %v", err)
			}
		})
	}
}
