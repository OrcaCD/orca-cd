package releaseage

import (
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func TestPolicy(t *testing.T) {
	global := models.Settings{MinimumReleaseAgeSeconds: 86400, ReleaseAgeAllowedRegistries: []string{"registry.local"}}
	custom := int64(3 * 86400)
	below := int64(3600)

	tests := []struct {
		name           string
		app            models.Application
		wantSeconds    int64
		wantInherited  bool
		wantRegistries []string
	}{
		{
			name:           "inherits the global default",
			app:            models.Application{},
			wantSeconds:    86400,
			wantInherited:  true,
			wantRegistries: []string{"registry.local"},
		},
		{
			name:           "own age and registries",
			app:            models.Application{MinimumReleaseAgeSeconds: &custom, ReleaseAgeAllowedRegistries: []string{"harbor.local", "registry.local"}},
			wantSeconds:    3 * 86400,
			wantRegistries: []string{"registry.local", "harbor.local"},
		},
		{
			name:           "own age below the global minimum",
			app:            models.Application{MinimumReleaseAgeSeconds: &below},
			wantSeconds:    86400,
			wantInherited:  true,
			wantRegistries: []string{"registry.local"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seconds, inherited := MinimumSeconds(&tt.app, global)
			if seconds != tt.wantSeconds || inherited != tt.wantInherited {
				t.Errorf("MinimumSeconds = %d, %v", seconds, inherited)
			}
			policy := Policy(&tt.app, global)
			if policy.MinimumSeconds != tt.wantSeconds || !slices.Equal(policy.AllowedRegistries, tt.wantRegistries) ||
				policy.EnforcedMinimumSeconds != 86400 || !slices.Equal(policy.EnforcedExemptRegistries, []string{"registry.local"}) {
				t.Errorf("Policy = %v", policy)
			}
		})
	}
}

func TestNormalizeRegistries(t *testing.T) {
	got, ok := NormalizeRegistries([]string{" GHCR.io ", "ghcr.io", "registry.local:5000"})
	if !ok || !slices.Equal(got, []string{"ghcr.io", "registry.local:5000"}) {
		t.Errorf("NormalizeRegistries() = %v, %v", got, ok)
	}
	if got, ok := NormalizeRegistries(nil); !ok || got == nil || len(got) != 0 {
		t.Errorf("expected no registries to give an empty list, got %v, %v", got, ok)
	}

	tooMany := make([]string, maxAllowedRegistries+1)
	for i := range tooMany {
		tooMany[i] = "registry.local"
	}
	for name, registries := range map[string][]string{
		"empty":    {" "},
		"path":     {"ghcr.io/owner"},
		"space":    {"ghcr .io"},
		"digest":   {"ghcr.io@sha256"},
		"too long": {strings.Repeat("a", maxRegistryLength+1)},
		"too many": tooMany,
	} {
		if _, ok := NormalizeRegistries(registries); ok {
			t.Errorf("%s: expected %v to be rejected", name, registries)
		}
	}
}
