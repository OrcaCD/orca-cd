package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

func setupImageStatusTest(t *testing.T, project *composetypes.Project) *Client {
	t.Helper()
	saveRestoreVars(t)
	origListServices := listServiceImageIDs
	origInspect := inspectLocalImage
	t.Cleanup(func() {
		listServiceImageIDs = origListServices
		inspectLocalImage = origInspect
	})

	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return project, nil
	}
	return c
}

func TestImageStatus_ReportsCurrentAndLatest(t *testing.T) {
	project := &composetypes.Project{Name: "myapp", Services: composetypes.Services{
		"web":    {Name: "web", Image: "ghcr.io/org/web:latest"},
		"db":     {Name: "db", Image: "postgres:17"},
		"worker": {Name: "worker", Build: &composetypes.BuildConfig{Context: "."}},
	}}
	c := setupImageStatusTest(t, project)

	listServiceImageIDs = func(_ context.Context, _ client.APIClient, appID string) (map[string]string, error) {
		if appID != "app-1" {
			t.Errorf("unexpected app ID %q", appID)
		}
		return map[string]string{"web": "sha256:webcontainerimage"}, nil
	}
	var inspected []string
	inspectLocalImage = func(_ context.Context, _ client.APIClient, ref string) (localImage, error) {
		inspected = append(inspected, ref)
		switch ref {
		case "sha256:webcontainerimage":
			return localImage{
				repoDigests: []string{"other/mirror@sha256:mirror", "ghcr.io/org/web@sha256:old"},
				labels:      map[string]string{ociVersionLabel: "1.2.0"},
			}, nil
		case "postgres:17":
			return localImage{repoDigests: []string{"postgres@sha256:pg"}}, nil
		}
		return localImage{}, errors.New("not found")
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, ref string) (string, error) {
		if ref == "ghcr.io/org/web:latest" {
			return "sha256:new", nil
		}
		return "sha256:pg", nil
	}

	statuses, err := c.ImageStatus(t.Context(), "app-1", "myapp", true)
	if err != nil {
		t.Fatalf("ImageStatus: %v", err)
	}
	want := []ServiceImageStatus{
		{Service: "db", Image: "postgres:17", CurrentDigest: "sha256:pg", LatestDigest: "sha256:pg"},
		{Service: "web", Image: "ghcr.io/org/web:latest", CurrentDigest: "sha256:old", CurrentVersion: "1.2.0", LatestDigest: "sha256:new", UpdateAvailable: true},
	}
	if len(statuses) != len(want) {
		t.Fatalf("expected %d statuses, got %+v", len(want), statuses)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Errorf("status %d = %+v, want %+v", i, statuses[i], want[i])
		}
	}
	// The running container's image wins over the tag, which may already be newer.
	if len(inspected) != 2 || inspected[0] != "postgres:17" || inspected[1] != "sha256:webcontainerimage" {
		t.Errorf("unexpected inspected refs: %v", inspected)
	}
}

func TestImageStatus_SkipsRegistryWithoutCheckLatest(t *testing.T) {
	c := setupImageStatusTest(t, &composetypes.Project{Name: "myapp", Services: composetypes.Services{
		"web": {Name: "web", Image: "nginx"},
	}})
	listServiceImageIDs = func(context.Context, client.APIClient, string) (map[string]string, error) {
		return nil, nil
	}
	inspectLocalImage = func(context.Context, client.APIClient, string) (localImage, error) {
		return localImage{repoDigests: []string{"nginx@sha256:cur"}}, nil
	}
	getRemoteDigest = func(context.Context, command.Cli, string) (string, error) {
		t.Error("registry must not be queried")
		return "", nil
	}

	statuses, err := c.ImageStatus(t.Context(), "app-1", "myapp", false)
	if err != nil {
		t.Fatalf("ImageStatus: %v", err)
	}
	if len(statuses) != 1 || statuses[0].CurrentDigest != "sha256:cur" || statuses[0].LatestDigest != "" || statuses[0].UpdateAvailable {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
}

func TestImageStatus_RegistryErrorIsPerService(t *testing.T) {
	c := setupImageStatusTest(t, &composetypes.Project{Name: "myapp", Services: composetypes.Services{
		"web": {Name: "web", Image: "nginx"},
	}})
	listServiceImageIDs = func(context.Context, client.APIClient, string) (map[string]string, error) {
		return nil, nil
	}
	inspectLocalImage = func(context.Context, client.APIClient, string) (localImage, error) {
		return localImage{repoDigests: []string{"nginx@sha256:cur"}}, nil
	}
	getRemoteDigest = func(context.Context, command.Cli, string) (string, error) {
		return "", errors.New("unauthorized")
	}

	statuses, err := c.ImageStatus(t.Context(), "app-1", "myapp", true)
	if err != nil {
		t.Fatalf("ImageStatus: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Error != "unauthorized" || statuses[0].UpdateAvailable {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
}

func TestImageStatus_MissingComposeFile(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	if _, err := c.ImageStatus(t.Context(), "app-1", "myapp", false); err == nil {
		t.Fatal("expected error when compose file does not exist")
	}
}

func TestImageRepository(t *testing.T) {
	tests := map[string]string{
		"nginx":                         "nginx",
		"nginx:1.27":                    "nginx",
		"ghcr.io/org/app:latest":        "ghcr.io/org/app",
		"registry:5000/app":             "registry:5000/app",
		"registry:5000/app:v1":          "registry:5000/app",
		"nginx@sha256:abc":              "nginx",
		"ghcr.io/org/app:v1@sha256:abc": "ghcr.io/org/app",
	}
	for ref, want := range tests {
		if got := imageRepository(ref); got != want {
			t.Errorf("imageRepository(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestRepoDigest(t *testing.T) {
	digests := []string{"mirror/app@sha256:mirror", "ghcr.io/org/app@sha256:match"}
	if got := repoDigest(digests, "ghcr.io/org/app:latest"); got != "sha256:match" {
		t.Errorf("expected matching repository digest, got %q", got)
	}
	if got := repoDigest(digests, "other/app"); got != "sha256:mirror" {
		t.Errorf("expected fallback to first digest, got %q", got)
	}
	if got := repoDigest(nil, "nginx"); got != "" {
		t.Errorf("expected empty digest, got %q", got)
	}
}
