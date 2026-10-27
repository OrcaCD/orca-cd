package composewarnings

import (
	"reflect"
	"testing"
)

func TestAnalyzeReportsApplicationWarnings(t *testing.T) {
	warnings := Analyze(`services:
  web:
    image: nginx:latest
    restart: unless-stopped
  postgres:
    image: docker.io/library/postgres:16
    privileged: true
    network_mode: host
  redis:
    image: redis:7
    volumes:
      - redis-data:/data
  app:
    image: ghcr.io/example/app
    restart: unless-stopped
volumes:
  redis-data:
`)

	want := []Warning{
		{Code: "floating_image_reference", Service: "app"},
		{Code: "missing_restart_policy", Service: "postgres"},
		{Code: "database_without_volume", Service: "postgres"},
		{Code: "privileged_container", Service: "postgres"},
		{Code: "host_network", Service: "postgres"},
		{Code: "missing_restart_policy", Service: "redis"},
		{Code: "floating_image_reference", Service: "web"},
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("Analyze() = %#v, want %#v", warnings, want)
	}
}

func TestHasFloatingImageReference(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{image: "nginx", want: true},
		{image: "nginx:latest", want: true},
		{image: "nginx:1.27", want: false},
		{image: "registry.example:5000/team/app", want: true},
		{image: "registry.example:5000/team/app:1.2", want: false},
		{image: "nginx@sha256:0123456789abcdef", want: false},
		{image: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := hasFloatingImageReference(tt.image); got != tt.want {
				t.Errorf("hasFloatingImageReference(%q) = %t, want %t", tt.image, got, tt.want)
			}
		})
	}
}

func TestAnalyzeReportsInvalidCompose(t *testing.T) {
	warnings := Analyze("not: [valid")
	want := []Warning{{Code: "invalid_compose"}}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("Analyze() = %#v, want %#v", warnings, want)
	}
}
