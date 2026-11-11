package releaseage

import (
	"slices"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func TestPolicy(t *testing.T) {
	global := models.Settings{MinimumReleaseAgeSeconds: 86400, ReleaseAgeAllowedRegistries: []string{"registry.local"}}
	custom := int64(3600)

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
			wantSeconds:    3600,
			wantRegistries: []string{"registry.local", "harbor.local"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seconds, inherited := MinimumSeconds(&tt.app, global)
			if seconds != tt.wantSeconds || inherited != tt.wantInherited {
				t.Errorf("MinimumSeconds = %d, %v", seconds, inherited)
			}
			policy := Policy(&tt.app, global)
			if policy.MinimumSeconds != tt.wantSeconds || !slices.Equal(policy.AllowedRegistries, tt.wantRegistries) {
				t.Errorf("Policy = %v", policy)
			}
		})
	}
}
