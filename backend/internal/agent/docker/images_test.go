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

func saveRestoreVars(t *testing.T) {
	t.Helper()
	origGetRemote := getRemoteDigest
	origGetLocal := getLocalDigests
	origPull := pullProject
	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		getRemoteDigest = origGetRemote
		getLocalDigests = origGetLocal
		pullProject = origPull
		loadProject = origLoad
		upProject = origUp
	})
}

func makeProject(images ...string) *composetypes.Project {
	services := composetypes.Services{}
	for i, img := range images {
		services[string(rune('a'+i))] = composetypes.ServiceConfig{Image: img}
	}
	return &composetypes.Project{Name: "test", Services: services}
}

func TestDigestMatchesLocal(t *testing.T) {
	tests := []struct {
		name         string
		localDigests []string
		remoteDigest string
		expectMatch  bool
	}{
		{
			name:         "match",
			localDigests: []string{"ghcr.io/org/app@sha256:abc123"},
			remoteDigest: "sha256:abc123",
			expectMatch:  true,
		},
		{
			name:         "no match",
			localDigests: []string{"ghcr.io/org/app@sha256:abc123"},
			remoteDigest: "sha256:def456",
			expectMatch:  false,
		},
		{
			name:         "empty local",
			localDigests: []string{},
			remoteDigest: "sha256:abc123",
			expectMatch:  false,
		},
		{
			name:         "multiple locals one matches",
			localDigests: []string{"ghcr.io/org/app@sha256:old", "ghcr.io/org/app@sha256:abc123"},
			remoteDigest: "sha256:abc123",
			expectMatch:  true,
		},
		{
			name:         "entry without @ separator",
			localDigests: []string{"sha256:abc123"},
			remoteDigest: "sha256:abc123",
			expectMatch:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := digestMatchesLocal(tc.localDigests, tc.remoteDigest); got != tc.expectMatch {
				t.Errorf("digestMatchesLocal() = %v, want %v", got, tc.expectMatch)
			}
		})
	}
}

func TestCheckAndPullImages_MissingComposeFile(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	_, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err == nil {
		t.Fatal("expected error when compose file does not exist")
	}
}

func TestCheckAndPullImages_UnsafeAppName(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	_, err := c.CheckAndPullImages(t.Context(), "app-123", "../bad", false)
	if err == nil {
		t.Fatal("expected error for unsafe application name")
	}
}

func TestCheckAndPullImages_NothingStale(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	// Write a compose file so the function doesn't bail early.
	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}

	const digest = "sha256:abc123"
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return digest, nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@" + digest}, nil
	}

	var pullCalled, upCalled bool
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated {
		t.Error("expected updated=false when all images are up-to-date")
	}
	if pullCalled {
		t.Error("pull should not be called when nothing is stale")
	}
	if upCalled {
		t.Error("up should not be called when nothing is stale")
	}
}

func TestCheckAndPullImages_StaleImages(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:newdigest", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@sha256:olddigest"}, nil
	}

	var pullCalled, upCalled bool
	var upRecreate string
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, opts api.UpOptions) error {
		upCalled = true
		upRecreate = opts.Create.Recreate
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "billing", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when images are stale")
	}
	if !pullCalled {
		t.Error("expected pull to be called for stale images")
	}
	if !upCalled {
		t.Error("expected compose up to be called after pull")
	}
	if upRecreate != api.RecreateForce {
		t.Errorf("expected recreate=%q, got %q", api.RecreateForce, upRecreate)
	}
}

func TestCheckAndPullImages_TranslatesBindMountBeforeUp(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.hostDeploymentsDir = "/srv/orca/deployments"

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		project := makeProject("ghcr.io/org/app:latest")
		service := project.Services["a"]
		service.Volumes = []composetypes.ServiceVolumeConfig{
			{
				Type:   composetypes.VolumeTypeBind,
				Source: filepath.Join(appDir, "data"),
				Target: "/data",
			},
		}
		project.Services["a"] = service
		return project, nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:newdigest", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@sha256:olddigest"}, nil
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return nil
	}
	var gotSource string
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		gotSource = project.Services["a"].Volumes[0].Source
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "billing", false)
	if err != nil {
		t.Fatalf("CheckAndPullImages: %v", err)
	}
	if !updated {
		t.Fatal("expected stale image to be updated")
	}
	if gotSource != "/srv/orca/deployments/billing/data" {
		t.Errorf("bind source = %q, want %q", gotSource, "/srv/orca/deployments/billing/data")
	}
}

func TestCheckAndPullImages_OrcaLabelsAppliedBeforeUp(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:new", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@sha256:old"}, nil
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return nil
	}

	var gotProject *composetypes.Project
	upProject = func(_ context.Context, _ api.Compose, p *composetypes.Project, _ api.UpOptions) error {
		gotProject = p
		return nil
	}

	const appID = "app-abc-123"
	if _, err := c.CheckAndPullImages(t.Context(), appID, "myapp", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotProject == nil {
		t.Fatal("upProject was not called")
	}
	for name, svc := range gotProject.Services {
		if got := svc.Labels[labelManagedBy]; got != "orca-cd" {
			t.Errorf("service %q: expected label %q=%q, got %q", name, labelManagedBy, "orca-cd", got)
		}
		if got := svc.Labels[labelApplicationID]; got != appID {
			t.Errorf("service %q: expected label %q=%q, got %q", name, labelApplicationID, appID, got)
		}
	}
}

func TestCheckAndPullImages_LocalOnlyImage(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: localimage:dev\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("localimage:dev"), nil
	}
	// Simulate a local-only image (registry returns error).
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "", errors.New("no such image in registry")
	}

	var pullCalled bool
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated {
		t.Error("expected updated=false when all services fail remote digest check")
	}
	if pullCalled {
		t.Error("pull should not be called when no services are actionable")
	}
}

func TestCheckAndPullImages_ImageNotPresentLocally(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:new", nil
	}
	// Image not present locally — treat as stale.
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return nil, errors.New("no such image")
	}

	var pullCalled bool
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when image is not present locally")
	}
	if !pullCalled {
		t.Error("expected pull to be called")
	}
}

func TestCheckAndPullImages_LoadProjectError(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: img:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return nil, errors.New("load error")
	}

	_, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err == nil {
		t.Fatal("expected error when loadProject fails")
	}
}

func TestCheckAndPullImages_PullError(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: img:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("img:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:new", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return nil, errors.New("no such image")
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return errors.New("pull failed")
	}

	_, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err == nil {
		t.Fatal("expected error when pullProject fails")
	}
}

func TestCheckAndPullImages_UpProjectError(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: img:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("img:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:new", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return nil, errors.New("no such image")
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return errors.New("up failed")
	}

	_, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err == nil {
		t.Fatal("expected error when upProject fails")
	}
}

func TestCheckAndPullImages_DeleteOldImages(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:newdigest", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@sha256:olddigest"}, nil
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return nil
	}

	// ImageRemove on the real daemon will fail with "not found" — logged as a warning, not an error.
	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when images are stale")
	}
}

func TestCheckAndPullImages_DeleteOldImages_SkipsEmptyDigest(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:new", nil
	}
	// Image not present locally — stale with empty oldDigest (first pull).
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return nil, errors.New("no such image")
	}
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return nil
	}

	// deleteOldImages=true but oldDigest is "" so ImageRemove must be skipped.
	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected updated=true for first-pull of missing image")
	}
}

func TestCheckAndPullImages_NoBuildServices(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    build: .\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Project with service that has no Image field (build-only service).
	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     "test",
			Services: composetypes.Services{"app": {Image: ""}},
		}, nil
	}

	var remoteCalled bool
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		remoteCalled = true
		return "sha256:abc", nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated {
		t.Error("expected updated=false for build-only services")
	}
	if remoteCalled {
		t.Error("expected no remote digest call for build-only services")
	}
}
