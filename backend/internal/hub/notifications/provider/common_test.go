package provider

import (
	"slices"
	"strings"
	"testing"
)

func TestParseDirectTargets(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wants   []string
		wantErr string
	}{
		{
			name:    "empty config",
			raw:     " \n\t ",
			wantErr: "notification config is empty",
		},
		{
			name:    "invalid json object",
			raw:     "{",
			wantErr: "invalid JSON notification config",
		},
		{
			name:    "invalid json list",
			raw:     "[",
			wantErr: "invalid JSON notification URL list",
		},
		{
			name:  "json object",
			raw:   `{"url":" discord://a@1 ","urls":["discord://b@2","discord://a@1","", "  "]}`,
			wants: []string{"discord://a@1", "discord://b@2"},
		},
		{
			name:  "json list",
			raw:   `["discord://a@1", " discord://a@1 ", "discord://b@2"]`,
			wants: []string{"discord://a@1", "discord://b@2"},
		},
		{
			name:  "comma and newline delimited",
			raw:   "discord://a@1,\n discord://b@2\r,discord://a@1",
			wants: []string{"discord://a@1", "discord://b@2"},
		},
		{
			name:    "no usable targets",
			raw:     "\n\r , ,, ",
			wantErr: "notification config does not contain any targets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDirectTargets(tt.raw)
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
				t.Fatalf("parseDirectTargets() error = %v", err)
			}
			if !slices.Equal(got, tt.wants) {
				t.Fatalf("expected %v, got %v", tt.wants, got)
			}
		})
	}
}

func TestNormalizeTargets(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		wants   []string
		wantErr string
	}{
		{
			name:  "trim and deduplicate",
			raw:   []string{" discord://a@1 ", "", "discord://a@1", "discord://b@2", " discord://b@2 "},
			wants: []string{"discord://a@1", "discord://b@2"},
		},
		{
			name:    "all empty",
			raw:     []string{" ", "", "\t"},
			wantErr: "notification config does not contain any targets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTargets(tt.raw)
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
				t.Fatalf("normalizeTargets() error = %v", err)
			}
			if !slices.Equal(got, tt.wants) {
				t.Fatalf("expected %v, got %v", tt.wants, got)
			}
		})
	}
}
