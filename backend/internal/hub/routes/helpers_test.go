package routes

import (
	"testing"
)

func TestSetGitHubActionsConfig(t *testing.T) {
	prev := githubActionsAppURL
	t.Cleanup(func() { githubActionsAppURL = prev })

	SetGitHubActionsConfig("https://hub.example.com")
	if githubActionsAppURL != "https://hub.example.com" {
		t.Errorf("githubActionsAppURL = %q, want %q", githubActionsAppURL, "https://hub.example.com")
	}
}

func TestExtractBranchFromRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"heads prefix stripped", "refs/heads/main", "main"},
		{"nested branch", "refs/heads/feature/foo", "feature/foo"},
		{"no prefix returns as-is", "develop", "develop"},
		{"empty returns empty", "", ""},
		{"whitespace only returns empty", "   ", ""},
		{"trims surrounding whitespace", "  refs/heads/dev  ", "dev"},
		{"tag ref not stripped", "refs/tags/v1.0", "refs/tags/v1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractBranchFromRef(tt.ref); got != tt.want {
				t.Errorf("extractBranchFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestNormalizeSyncError(t *testing.T) {
	empty := ""
	msg := "boom"

	if got := normalizeSyncError(nil); got != nil {
		t.Errorf("nil input: got %v, want nil", got)
	}
	if got := normalizeSyncError(&empty); got != nil {
		t.Errorf("empty string input: got %v, want nil", got)
	}
	got := normalizeSyncError(&msg)
	if got == nil || *got != "boom" {
		t.Errorf("non-empty input: got %v, want %q", got, "boom")
	}
}
