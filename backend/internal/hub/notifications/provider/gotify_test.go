package provider

import (
	"net/url"
	"strings"
	"testing"
)

func TestGotifyProviderBuildShoutrrrUrls(t *testing.T) {
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
			wantErr: "invalid JSON gotify config",
		},
		{
			name:    "missing serverUrl",
			raw:     `{"appToken":"Aaa.bbb.ccc.ddd"}`,
			wantErr: "gotify config requires serverUrl",
		},
		{
			name:    "missing appToken",
			raw:     `{"serverUrl":"https://gotify.example.com"}`,
			wantErr: "gotify config requires appToken",
		},
		{
			name:    "unsupported URL scheme",
			raw:     `{"serverUrl":"ftp://gotify.example.com","appToken":"Aaa.bbb.ccc.ddd"}`,
			wantErr: "gotify server URL must use http or https",
		},
		{
			name:    "missing host",
			raw:     `{"serverUrl":"https:///gotify","appToken":"Aaa.bbb.ccc.ddd"}`,
			wantErr: "gotify server URL must include a host",
		},
		{
			name:    "server URL with query string",
			raw:     `{"serverUrl":"https://gotify.example.com?token=nope","appToken":"Aaa.bbb.ccc.ddd"}`,
			wantErr: "gotify server URL must not include a query string or fragment",
		},
		{
			name:    "priority too low",
			raw:     `{"serverUrl":"https://gotify.example.com","appToken":"Aaa.bbb.ccc.ddd","priority":-3}`,
			wantErr: "gotify priority must be between -2 and 10",
		},
		{
			name:    "priority too high",
			raw:     `{"serverUrl":"https://gotify.example.com","appToken":"Aaa.bbb.ccc.ddd","priority":11}`,
			wantErr: "gotify priority must be between -2 and 10",
		},
		{
			name:    "direct target string",
			raw:     "gotify://gotify.example.com/Aaa.bbb.ccc.ddd",
			wantErr: "invalid JSON gotify config",
		},
	}

	provider := GotifyProvider{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := provider.BuildShoutrrrUrls(tt.raw)
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
				t.Fatalf("BuildShoutrrrUrls() error = %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("expected one URL, got %v", got)
			}
			if !strings.HasPrefix(got[0], "gotify://") {
				t.Fatalf("expected gotify URL, got %q", got[0])
			}
		})
	}
}

func TestGotifyProviderBuildsStructuredURL(t *testing.T) {
	provider := GotifyProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"serverUrl":"https://gotify.example.com/base","appToken":"Aaa.bbb.ccc.ddd","priority":8,"customPath":"/gotify"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(urls))
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Scheme != "gotify" {
		t.Fatalf("expected scheme gotify, got %q", parsed.Scheme)
	}
	if parsed.Host != "gotify.example.com" {
		t.Fatalf("expected host %q, got %q", "gotify.example.com", parsed.Host)
	}
	if parsed.Path != "/base/gotify/Aaa.bbb.ccc.ddd" {
		t.Fatalf("expected path %q, got %q", "/base/gotify/Aaa.bbb.ccc.ddd", parsed.Path)
	}
	if parsed.Query().Get("priority") != "8" {
		t.Fatalf("expected priority 8, got %q", parsed.Query().Get("priority"))
	}
}

func TestGotifyProviderDefaultsPriorityToFive(t *testing.T) {
	provider := GotifyProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"serverUrl":"https://gotify.example.com","appToken":"Aaa.bbb.ccc.ddd"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Query().Get("priority") != "5" {
		t.Fatalf("expected default priority 5, got %q", parsed.Query().Get("priority"))
	}
}

func TestGotifyProviderHTTPURLDisablesTLS(t *testing.T) {
	provider := GotifyProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"serverUrl":"http://localhost:8123","appToken":"Aaa.bbb.ccc.ddd"}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Query().Get("disabletls") != "yes" {
		t.Fatalf("expected disabletls=yes for HTTP Gotify server, got %q", parsed.Query().Get("disabletls"))
	}
}
