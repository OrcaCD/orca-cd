package applications

import (
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/deployfiles"
)

func TestAdditionalFileDestinations(t *testing.T) {
	const compose = "apps/site/stack.yaml"
	got, err := AdditionalFileDestinations(compose, []string{"apps/site/config/file.txt", "apps/site/data.json"})
	if err != nil || !slices.Equal(got, []string{"config/file.txt", "data.json"}) {
		t.Fatalf("destinations = %v, error = %v", got, err)
	}
	for _, tc := range []struct{ path, message string }{
		{"apps/site/../other", "invalid repository"},
		{"apps/site-other/other", "inside the Compose"},
		{compose, "already included"},
		{"apps/site/.env", "reserved"},
	} {
		if _, err := AdditionalFileDestinations(compose, []string{tc.path}); err == nil || !strings.Contains(err.Error(), tc.message) {
			t.Errorf("selection %q returned %v, want %q", tc.path, err, tc.message)
		}
	}
}

func TestFetchAdditionalFilesLimitsTotalSize(t *testing.T) {
	p := &fileSyncProvider{contentByPath: map[string]string{"assets/a": strings.Repeat("a", deployfiles.MaxBytes), "assets/b": "b"}}
	got, err := FetchAdditionalFiles(t.Context(), p, &models.Repository{}, "commit", "assets/compose.yaml", []string{"assets/a", "assets/b"})
	if got != "" || err == nil || !strings.Contains(err.Error(), "assets/b") {
		t.Fatalf("expected an error naming the file that exceeds the limit, got %v", err)
	}
}
