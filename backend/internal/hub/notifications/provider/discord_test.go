package provider

import (
	"net/url"
	"strings"
	"testing"
)

func TestDiscordProviderBuildShouterrrUrls(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "empty config",
			raw:     " \n ",
			wantErr: "notification config is empty",
		},
		{
			name:    "invalid json object",
			raw:     "{",
			wantErr: "invalid JSON discord config",
		},
		{
			name:    "json object with direct urls",
			raw:     "{\"url\":\"discord://a@1\"}",
			wantErr: "discord config requires token and webhookId",
		},
		{
			name:    "json object missing token",
			raw:     `{"webhookId":"123"}`,
			wantErr: "discord config requires token and webhookId",
		},
		{
			name:    "direct target string",
			raw:     "discord://a@1,discord://b@2",
			wantErr: "invalid JSON discord config",
		},
		{
			name:    "direct target list",
			raw:     `["discord://a@1","discord://b@2"]`,
			wantErr: "invalid JSON discord config",
		},
	}

	provider := DiscordProvider{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.BuildShouterrrUrls(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("BuildShouterrrUrls() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("expected one URL, got %v", got)
			}
			if !strings.HasPrefix(got[0], "discord://") {
				t.Fatalf("expected discord URL, got %q", got[0])
			}
		})
	}
}

func TestDiscordProviderBuildsStructuredURL(t *testing.T) {
	provider := DiscordProvider{}

	urls, err := provider.BuildShouterrrUrls(`{"token":"token-abc","webhookId":"123456789","threadId":"987654321","username":"Orca Bot","avatarUrl":"https://example.com/avatar.png","title":"Deploy done"}`)
	if err != nil {
		t.Fatalf("BuildShouterrrUrls() error = %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Scheme != "discord" {
		t.Fatalf("expected scheme discord, got %q", parsed.Scheme)
	}
	if parsed.User == nil || parsed.User.Username() != "token-abc" {
		t.Fatalf("expected token in URL userinfo, got %v", parsed.User)
	}
	if parsed.Host != "123456789" {
		t.Fatalf("expected webhook id host %q, got %q", "123456789", parsed.Host)
	}

	query := parsed.Query()
	if query.Get("thread_id") != "987654321" {
		t.Fatalf("expected thread_id query parameter, got %q", query.Get("thread_id"))
	}
	if query.Get("username") != "Orca Bot" {
		t.Fatalf("expected username query parameter, got %q", query.Get("username"))
	}
	if query.Get("avatarurl") != "https://example.com/avatar.png" {
		t.Fatalf("expected avatarurl query parameter, got %q", query.Get("avatarurl"))
	}
	if query.Get("title") != "Deploy done" {
		t.Fatalf("expected title query parameter, got %q", query.Get("title"))
	}
}
