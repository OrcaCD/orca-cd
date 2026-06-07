package provider

import (
	"slices"
	"strings"
	"testing"
)

func TestCustomProviderBuildShouterrrUrls(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{
			name:    "empty config",
			raw:     " \n ",
			wantErr: "notification config is empty",
		},
		{
			name:    "missing scheme",
			raw:     "example.com/hook",
			wantErr: "custom Shouterrr URL must include a scheme",
		},
		{
			name: "direct shouterrr url",
			raw:  "discord://token@123456789?title=Deploy+done",
			want: []string{"discord://token@123456789?title=Deploy+done"},
		},
		{
			name: "json config",
			raw:  `{"url":"slack://token-a/token-b/token-c"}`,
			want: []string{"slack://token-a/token-b/token-c"},
		},
		{
			name:    "json config missing url",
			raw:     `{}`,
			wantErr: "custom notification config requires url",
		},
		{
			name:    "invalid json config",
			raw:     `{`,
			wantErr: "invalid JSON custom notification config",
		},
	}

	provider := CustomProvider{}

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
			if !slices.Equal(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
