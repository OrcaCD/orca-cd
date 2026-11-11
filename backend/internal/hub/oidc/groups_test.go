package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestGroupsFromClaims_TooManyGroups(t *testing.T) {
	groups := make([]any, maxGroups+1)
	for i := range groups {
		groups[i] = fmt.Sprintf("group-%d", i)
	}
	if _, _, err := groupsFromClaims(map[string]any{"groups": groups}, "groups"); !errors.Is(err, ErrGroupsUnavailable) {
		t.Fatalf("expected ErrGroupsUnavailable, got %v", err)
	}
	if got, _, err := groupsFromClaims(map[string]any{"groups": groups[:maxGroups]}, "groups"); err != nil || len(got) != maxGroups {
		t.Fatalf("expected %d groups, got %d, %v", maxGroups, len(got), err)
	}
}

// fakeUserInfo serves the claims as userinfo of the subject and counts the requests.
func fakeUserInfo(subject, claims string, err error, requests *int) *userInfo {
	return &userInfo{
		subject: "user-1",
		fetch: func(context.Context) (string, json.RawMessage, error) {
			*requests++
			return subject, json.RawMessage(claims), err
		},
	}
}

func TestReadGroups(t *testing.T) {
	tests := []struct {
		name         string
		idToken      map[string]any
		info         func(requests *int) *userInfo
		want         []string
		wantErr      bool
		wantRequests int
	}{
		{
			name:    "from the id_token",
			idToken: map[string]any{"groups": []any{"devs"}},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("user-1", `{"groups": ["ops"]}`, nil, requests)
			},
			want: []string{"devs"},
		},
		{
			name:    "from the userinfo",
			idToken: map[string]any{},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("user-1", `{"groups": ["ops"]}`, nil, requests)
			},
			want:         []string{"ops"},
			wantRequests: 1,
		},
		{
			name:    "missing from both",
			idToken: map[string]any{},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("user-1", `{"email": "user@example.com"}`, nil, requests)
			},
			want:         []string{},
			wantRequests: 1,
		},
		{
			name:    "missing without a userinfo endpoint",
			idToken: map[string]any{},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("", "", errNoUserInfo, requests)
			},
			want:         []string{},
			wantRequests: 1,
		},
		{
			name:    "userinfo fails",
			idToken: map[string]any{},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("", "", errors.New("unavailable"), requests)
			},
			wantErr:      true,
			wantRequests: 1,
		},
		{
			name:    "userinfo of another user",
			idToken: map[string]any{},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("user-2", `{"groups": ["admins"]}`, nil, requests)
			},
			wantErr:      true,
			wantRequests: 1,
		},
		{
			name:    "invalid claim in the id_token",
			idToken: map[string]any{"groups": 42},
			info: func(requests *int) *userInfo {
				return fakeUserInfo("user-1", `{"groups": ["ops"]}`, nil, requests)
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			got, err := readGroups(t.Context(), tt.idToken, tt.info(&requests), "groups")
			if tt.wantErr {
				if !errors.Is(err, ErrGroupsUnavailable) {
					t.Fatalf("expected ErrGroupsUnavailable, got %v, %v", got, err)
				}
			} else if err != nil || got == nil || !slices.Equal(got, tt.want) {
				t.Fatalf("readGroups() = %#v, %v; want %v", got, err, tt.want)
			}
			if requests != tt.wantRequests {
				t.Fatalf("expected %d userinfo requests, got %d", tt.wantRequests, requests)
			}
		})
	}
}

func TestUserInfo_FetchesOnce(t *testing.T) {
	requests := 0
	info := fakeUserInfo("user-1", `{"email": "user@example.com", "groups": ["ops"]}`, nil, &requests)

	claims := profileClaims{}
	mergeUserInfoClaims(t.Context(), info, &claims)
	groups, err := readGroups(t.Context(), map[string]any{}, info, "groups")
	if err != nil || !slices.Equal(groups, []string{"ops"}) || claims.Email != "user@example.com" {
		t.Fatalf("unexpected result: groups=%v err=%v email=%q", groups, err, claims.Email)
	}
	if requests != 1 {
		t.Fatalf("expected one userinfo request, got %d", requests)
	}
}
