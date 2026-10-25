package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/pkg/authconfig"
	imagetypes "github.com/moby/moby/api/types/image"
	registrytypes "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

func saveRestoreVars(t *testing.T) {
	t.Helper()
	origGetRemote := getRemoteDigest
	origGetLocal := getLocalDigests
	origIsLocalOnly := isLocalOnlyImage
	origPull := pullProject
	origLoad := loadProject
	origUp := upProject
	origListImages := listApplicationImageIDs
	origRemoveImage := removeImage
	t.Cleanup(func() {
		getRemoteDigest = origGetRemote
		getLocalDigests = origGetLocal
		isLocalOnlyImage = origIsLocalOnly
		pullProject = origPull
		loadProject = origLoad
		upProject = origUp
		listApplicationImageIDs = origListImages
		removeImage = origRemoveImage
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

	var pullCalled, upCalled, upSkipProviders bool
	var upRecreate string
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, opts api.UpOptions) error {
		upCalled = true
		upRecreate = opts.Create.Recreate
		upSkipProviders = opts.Create.SkipProviders
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
	if !upSkipProviders {
		t.Error("expected compose up to skip provider services")
	}
}

func TestCheckAndPullImages_TranslatesBindMountBeforeUp(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.mu.Lock()
	c.hostDeploymentsDir = "/srv/orcacd/deployments"
	c.mu.Unlock()

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
	if gotSource != "/srv/orcacd/deployments/billing/data" {
		t.Errorf("bind source = %q, want %q", gotSource, "/srv/orcacd/deployments/billing/data")
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
	isLocalOnlyImage = func(_ context.Context, _ client.APIClient, _ string) bool {
		return true
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

func TestCheckAndPullImages_DigestLookupFailure(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	writeTestComposeFile(t, c.deploymentsDir, "myapp")

	registryErr := errors.New("unauthorized: authentication required")
	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("reg.example.com/x:main"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "", registryErr
	}
	// Missing locally or pulled from a registry
	isLocalOnlyImage = func(_ context.Context, _ client.APIClient, _ string) bool {
		return false
	}

	var pullCalled bool
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pullCalled = true
		return nil
	}

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false)
	if err == nil {
		t.Fatal("expected an error for a failed digest lookup")
	}
	if !errors.Is(err, registryErr) {
		t.Errorf("expected the registry error to be wrapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "reg.example.com/x:main") {
		t.Errorf("expected the error to name the image, got %v", err)
	}
	if updated {
		t.Error("expected updated=false when nothing is stale")
	}
	if pullCalled {
		t.Error("pull should not be called when nothing is stale")
	}
}

type fakeInspectClient struct {
	client.APIClient
	result client.ImageInspectResult
	err    error
}

func (f *fakeInspectClient) ImageInspect(context.Context, string, ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	return f.result, f.err
}

func TestIsLocalOnlyImage(t *testing.T) {
	inspected := func(identity *imagetypes.Identity, repoDigests ...string) *fakeInspectClient {
		var result client.ImageInspectResult
		result.RepoDigests = repoDigests
		result.Identity = identity
		return &fakeInspectClient{result: result}
	}
	pulled := &imagetypes.Identity{Pull: []imagetypes.PullIdentity{{Repository: "docker.io/library/app"}}}
	built := &imagetypes.Identity{Build: []imagetypes.BuildIdentity{{Ref: "abc"}}}

	tests := []struct {
		name string
		cli  *fakeInspectClient
		want bool
	}{
		{"missing locally", &fakeInspectClient{err: errors.New("no such image")}, false},
		{"containerd store, pulled", inspected(pulled, "app@sha256:a"), false},
		// The containerd store lists a digest for the local tag of a build as well
		{"containerd store, built", inspected(built, "reg.example.com/app@sha256:a"), true},
		{"classic store, pulled", inspected(nil, "reg.example.com/app@sha256:a"), false},
		{"classic store, built", inspected(nil), true},
		{"identity without origin, pulled", inspected(&imagetypes.Identity{}, "app@sha256:a"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLocalOnlyImage(t.Context(), tc.cli, "reg.example.com/app:latest"); got != tc.want {
				t.Errorf("isLocalOnlyImage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckAndPullImages_DigestFailureAlongsideStaleImage(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	writeTestComposeFile(t, c.deploymentsDir, "myapp")

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest", "reg.example.com/x:main"), nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, imageRef string) (string, error) {
		if imageRef == "reg.example.com/x:main" {
			return "", errors.New("unauthorized: authentication required")
		}
		return "sha256:new", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return []string{"ghcr.io/org/app@sha256:old"}, nil
	}
	isLocalOnlyImage = func(_ context.Context, _ client.APIClient, _ string) bool {
		return false
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
	if !updated {
		t.Error("expected the healthy stale image to be updated")
	}
	if !pullCalled || !upCalled {
		t.Errorf("expected pull and up to run, got pull=%v up=%v", pullCalled, upCalled)
	}
	if err == nil || !strings.Contains(err.Error(), "reg.example.com/x:main") {
		t.Errorf("expected an error naming the unreachable image, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "ghcr.io/org/app") {
		t.Errorf("did not expect the healthy image in the error, got %v", err)
	}
}

func TestRegistryHost(t *testing.T) {
	tests := map[string]string{
		"reg.example.com/x:main":    "reg.example.com",
		"127.0.0.1:5055/e2e/x:1":    "127.0.0.1:5055",
		"localhost/x":               "localhost",
		"ghcr.io/org/app@sha256:ab": "ghcr.io",
		"nginx:latest":              "docker.io",
		"library/nginx":             "docker.io",
		"org/app:1.0":               "docker.io",
	}
	for ref, want := range tests {
		if got := registryHost(ref); got != want {
			t.Errorf("registryHost(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestIsUnauthorized(t *testing.T) {
	tests := []struct {
		err  string
		want bool
	}{
		{`Head "http://reg.example.com/v2/x/manifests/1": no basic auth credentials`, true},
		{"unauthorized: authentication required", true},
		{"pull access denied for x, repository does not exist or may require authorization", true},
		{`Get "http://reg.example.com/v2/": dial tcp: connect: connection refused`, false},
		{"manifest unknown", false},
	}
	for _, tc := range tests {
		if got := isUnauthorized(errors.New(tc.err)); got != tc.want {
			t.Errorf("isUnauthorized(%q) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestWithCredentialsHint(t *testing.T) {
	base := errors.New("unauthorized: authentication required")
	empty, err := authconfig.Encode(registrytypes.AuthConfig{ServerAddress: "reg.example.com"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	stored, err := authconfig.Encode(registrytypes.AuthConfig{Username: "u", Password: "secret"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got := withCredentialsHint(base, "reg.example.com/x:main", empty, nil)
	if !errors.Is(got, base) {
		t.Errorf("expected the original error to be wrapped, got %v", got)
	}
	if !strings.Contains(got.Error(), "no credentials for reg.example.com found") {
		t.Errorf("expected a missing credentials hint, got %q", got)
	}

	got = withCredentialsHint(base, "reg.example.com/x:main", stored, nil)
	if got.Error() != base.Error() {
		t.Errorf("expected no hint when credentials exist, got %q", got)
	}

	got = withCredentialsHint(base, "reg.example.com/x:main", "", errors.New("helper failed"))
	if !strings.Contains(got.Error(), "reg.example.com") || !strings.Contains(got.Error(), "helper failed") {
		t.Errorf("expected the credential lookup failure in the hint, got %q", got)
	}
	if strings.Contains(got.Error(), "secret") {
		t.Errorf("hint must not contain secrets, got %q", got)
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

// stubImageCleanup simulates the application's containers switching from the
// images in before to the images in after once compose up has run, and records
// every image removal.
func stubImageCleanup(t *testing.T, before, after []string) (upDone func(), removed *[]string) {
	t.Helper()
	var up bool
	listApplicationImageIDs = func(_ context.Context, _ client.APIClient, appID string) (map[string]struct{}, error) {
		if appID != "app-123" {
			t.Errorf("expected application id %q, got %q", "app-123", appID)
		}
		ids := before
		if up {
			ids = after
		}
		set := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			set[id] = struct{}{}
		}
		return set, nil
	}
	var removedIDs []string
	removeImage = func(_ context.Context, _ client.APIClient, imageID string) error {
		removedIDs = append(removedIDs, imageID)
		return nil
	}
	return func() { up = true }, &removedIDs
}

func setupStaleImageApp(t *testing.T, c *Client, upDone func()) {
	t.Helper()
	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return makeProject("ghcr.io/org/app:latest", "ghcr.io/org/sidecar:1"), nil
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
		upDone()
		return nil
	}
}

func TestCheckAndPullImages_DeleteOldImages(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	upDone, removed := stubImageCleanup(t, []string{"sha256:old-app", "sha256:sidecar"}, []string{"sha256:new-app", "sha256:sidecar"})
	setupStaleImageApp(t, c, upDone)

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when images are stale")
	}
	if len(*removed) != 1 || (*removed)[0] != "sha256:old-app" {
		t.Errorf("expected only the replaced image to be removed, got %v", *removed)
	}
}

func TestCheckAndPullImages_KeepsOldImagesWhenDisabled(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	upDone, removed := stubImageCleanup(t, []string{"sha256:old-app"}, []string{"sha256:new-app"})
	setupStaleImageApp(t, c, upDone)

	if _, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*removed) != 0 {
		t.Errorf("expected no images to be removed, got %v", *removed)
	}
}

func TestCheckAndPullImages_DeleteOldImages_FirstDeploy(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	// No containers existed before the update, so there is nothing to clean up.
	upDone, removed := stubImageCleanup(t, nil, []string{"sha256:new-app"})
	setupStaleImageApp(t, c, upDone)

	if _, err := c.CheckAndPullImages(t.Context(), "app-123", "myapp", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*removed) != 0 {
		t.Errorf("expected no images to be removed, got %v", *removed)
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

func writeTestComposeFile(t *testing.T, deploymentsDir, appName string) {
	t.Helper()
	appDir := filepath.Join(deploymentsDir, appName)
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: img:latest\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func stubStalePrivilegedProject(t *testing.T) (pullCalled, upCalled *bool) {
	t.Helper()
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{Image: "img:latest", Privileged: true}},
		}, nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, _ string) (string, error) {
		return "sha256:newdigest", nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, _ string) ([]string, error) {
		return nil, errors.New("not present locally")
	}
	var pulled, upped bool
	pullProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.PullOptions) error {
		pulled = true
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upped = true
		return nil
	}
	return &pulled, &upped
}

func TestCheckAndPullImages_RejectsPolicyViolation(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	writeTestComposeFile(t, c.deploymentsDir, "billing")
	pullCalled, upCalled := stubStalePrivilegedProject(t)

	updated, err := c.CheckAndPullImages(t.Context(), "app-123", "billing", false)
	if err == nil {
		t.Fatal("expected a compose file violating the security policy to be rejected")
	}
	if updated {
		t.Error("expected updated=false for a rejected project")
	}
	if *pullCalled || *upCalled {
		t.Fatal("expected neither pull nor compose up for a rejected project")
	}
}

func TestCheckAndPullImages_AllowlistedApplicationBypassesPolicy(t *testing.T) {
	const allowedAppID = "019e1ce8-7938-71b8-be55-4b184f307a2d"

	saveRestoreVars(t)
	c := newTestClientWithAllowlist(t, map[string]struct{}{allowedAppID: {}})
	c.deploymentsDir = t.TempDir()
	writeTestComposeFile(t, c.deploymentsDir, "billing")
	_, upCalled := stubStalePrivilegedProject(t)

	updated, err := c.CheckAndPullImages(t.Context(), allowedAppID, "billing", false)
	if err != nil {
		t.Fatalf("expected allowlisted application to be redeployed, got: %v", err)
	}
	if !updated || !*upCalled {
		t.Fatal("expected compose up for an allowlisted application")
	}
}
