package provider

import (
	"net/url"
	"strings"
	"testing"

	"github.com/nicholas-fedor/shoutrrr"
)

func TestEmailProviderBuildShoutrrrUrls(t *testing.T) {
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
			wantErr: "invalid JSON email config",
		},
		{
			name:    "missing host",
			raw:     `{"smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email config requires smtpHost",
		},
		{
			name:    "invalid host",
			raw:     `{"smtpHost":"smtp.example.com/path","smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email smtpHost must be a hostname or IP address without scheme or port",
		},
		{
			name:    "host with scheme",
			raw:     `{"smtpHost":"smtp://smtp.example.com","smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email smtpHost must be a hostname or IP address without scheme or port",
		},
		{
			name:    "host with port",
			raw:     `{"smtpHost":"smtp.example.com:587","smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email smtpHost must be a hostname or IP address without scheme or port",
		},
		{
			name:    "invalid port",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":70000,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email smtpPort must be between 1 and 65535",
		},
		{
			name:    "missing from address",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":587,"toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email config requires fromAddress",
		},
		{
			name:    "invalid to address",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["not an email"],"useTls":true}`,
			wantErr: "email toAddresses[0] must be a valid email address",
		},
		{
			name:    "from address with display name",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":587,"fromAddress":"Orca <orca@example.com>","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email fromAddress must be a valid email address",
		},
		{
			name:    "to address with display name",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":587,"fromAddress":"orca@example.com","toAddresses":["Ops <ops@example.com>"],"useTls":true}`,
			wantErr: "email toAddresses[0] must be a valid email address",
		},
		{
			name:    "password without username",
			raw:     `{"smtpHost":"smtp.example.com","smtpPort":587,"password":"secret","fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`,
			wantErr: "email password requires username",
		},
		{
			name:    "direct target string",
			raw:     "smtp://smtp.example.com:587",
			wantErr: "invalid JSON email config",
		},
	}

	provider := EmailProvider{}

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
			if !strings.HasPrefix(got[0], "smtp://") {
				t.Fatalf("expected smtp URL, got %q", got[0])
			}
		})
	}
}

func TestEmailProviderBuildsStructuredURL(t *testing.T) {
	provider := EmailProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"smtpHost":"smtp.example.com","smtpPort":587,"username":"orca","password":"s e:c/r?et","fromAddress":"orca+deploy@example.com","fromName":"Orca CD","toAddresses":["ops@example.com","dev+deploy@example.com"],"useTls":true}`)
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

	if parsed.Scheme != "smtp" {
		t.Fatalf("expected scheme smtp, got %q", parsed.Scheme)
	}
	if parsed.Host != "smtp.example.com:587" {
		t.Fatalf("expected host %q, got %q", "smtp.example.com:587", parsed.Host)
	}
	if parsed.User.Username() != "orca" {
		t.Fatalf("expected username %q, got %q", "orca", parsed.User.Username())
	}
	password, ok := parsed.User.Password()
	//nolint:gosec
	if !ok || password != "s e:c/r?et" {
		t.Fatalf("expected encoded password to round-trip, got %q", password)
	}

	query := parsed.Query()
	if query.Get("fromaddress") != "orca+deploy@example.com" {
		t.Fatalf("expected fromaddress query parameter, got %q", query.Get("fromaddress"))
	}
	if query.Get("fromname") != "Orca CD" {
		t.Fatalf("expected fromname query parameter, got %q", query.Get("fromname"))
	}
	if query.Get("toaddresses") != "ops@example.com,dev+deploy@example.com" {
		t.Fatalf("expected toaddresses query parameter, got %q", query.Get("toaddresses"))
	}
	if query.Get("encryption") != "" || query.Get("usestarttls") != "" {
		t.Fatalf("expected TLS defaults to be omitted, got query %q", parsed.RawQuery)
	}

	if _, err := shoutrrr.CreateSender(urls...); err != nil {
		t.Fatalf("expected Shoutrrr to accept URL %q: %v", urls[0], err)
	}
}

func TestEmailProviderCanDisableTLS(t *testing.T) {
	provider := EmailProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"smtpHost":"localhost","smtpPort":25,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":false}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	query := parsed.Query()
	if query.Get("encryption") != "" {
		t.Fatalf("expected encryption default to be omitted, got %q", query.Get("encryption"))
	}
	if query.Get("usestarttls") != "No" {
		t.Fatalf("expected usestarttls=No, got %q", query.Get("usestarttls"))
	}
}

func TestEmailProviderSupportsBracketedIPv6Host(t *testing.T) {
	provider := EmailProvider{}

	urls, err := provider.BuildShoutrrrUrls(`{"smtpHost":"[::1]","smtpPort":25,"fromAddress":"orca@example.com","toAddresses":["ops@example.com"],"useTls":true}`)
	if err != nil {
		t.Fatalf("BuildShoutrrrUrls() error = %v", err)
	}

	parsed, err := url.Parse(urls[0])
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", urls[0], err)
	}

	if parsed.Host != "[::1]:25" {
		t.Fatalf("expected IPv6 host with port, got %q", parsed.Host)
	}
}
