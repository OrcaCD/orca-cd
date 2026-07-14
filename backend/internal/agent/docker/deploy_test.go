package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
)

func TestDeploy_WritesComposeFileAndRunsComposeUp(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	originalLoadProject := loadProject
	originalUpProject := upProject
	t.Cleanup(func() {
		loadProject = originalLoadProject
		upProject = originalUpProject
	})

	var gotLoadOptions api.ProjectLoadOptions
	loadProject = func(_ context.Context, _ api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
		gotLoadOptions = options
		return &composetypes.Project{Name: options.ProjectName}, nil
	}

	var gotUpOptions api.UpOptions
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, options api.UpOptions) error {
		if project.Name != "billing" {
			t.Fatalf("expected project name %q, got %q", "billing", project.Name)
		}
		gotUpOptions = options
		return nil
	}

	req := DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: ghcr.io/orcacd/app:1.0.0\n",
	}

	if err := c.Deploy(t.Context(), req); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	composePath := filepath.Join(c.deploymentsDir, req.ApplicationName, composeFileName)
	//nolint:gosec // composePath is built from t.TempDir() and a fixed test application id
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != req.ComposeFile {
		t.Fatalf("expected compose file to be written to deployment volume")
	}

	if gotLoadOptions.ProjectName != "billing" {
		t.Fatalf("expected load project name %q, got %q", "billing", gotLoadOptions.ProjectName)
	}
	if len(gotLoadOptions.ConfigPaths) != 1 || gotLoadOptions.ConfigPaths[0] != composePath {
		t.Fatalf("unexpected config paths: %#v", gotLoadOptions.ConfigPaths)
	}
	if gotLoadOptions.WorkingDir != filepath.Join(c.deploymentsDir, req.ApplicationName) {
		t.Fatalf("expected working dir %q, got %q", filepath.Join(c.deploymentsDir, req.ApplicationName), gotLoadOptions.WorkingDir)
	}

	if !gotUpOptions.Start.Wait {
		t.Fatal("expected compose up to wait for services to become ready")
	}
	if gotUpOptions.Start.WaitTimeout != deployWaitTimeout {
		t.Fatalf("expected wait timeout %s, got %s", deployWaitTimeout, gotUpOptions.Start.WaitTimeout)
	}
	if !gotUpOptions.Create.RemoveOrphans {
		t.Fatal("expected compose up to remove orphaned containers")
	}
	if gotUpOptions.Create.Recreate != api.RecreateDiverged {
		t.Fatalf("expected recreate strategy %q, got %q", api.RecreateDiverged, gotUpOptions.Create.Recreate)
	}
}

func TestDeploy_RejectsUnsafeApplicationName(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	for _, name := range []string{"../bad", "bad/../name"} {
		err := c.Deploy(t.Context(), DeployRequest{
			ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
			ApplicationName: name,
			ComposeFile:     "services: {}\n",
		})
		if err == nil {
			t.Fatalf("expected deploy to reject application name %q", name)
		}
	}
}

func TestDeploy_LoadProjectError(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	t.Cleanup(func() { loadProject = origLoad })
	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return nil, errors.New("load error")
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err == nil {
		t.Fatal("expected error when loadProject fails")
	}
}

func TestDeploy_UpProjectError(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{Name: opts.ProjectName}, nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return errors.New("up failed")
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err == nil {
		t.Fatal("expected error when upProject fails")
	}
}

func TestDeploy_RejectsPrivilegedService(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{Privileged: true}},
		}, nil
	}
	upCalled := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    privileged: true\n",
	})
	if err == nil {
		t.Fatal("expected deploy of a privileged service to be rejected")
	}
	if upCalled {
		t.Fatal("expected compose up to never be called for a rejected deploy")
	}
}

func TestDeploy_AllowsPrivilegedServiceWhenApplicationAllowlisted(t *testing.T) {
	const allowedAppID = "019e1ce8-7938-71b8-be55-4b184f307a2d"

	c := newTestClientWithAllowlist(t, map[string]struct{}{allowedAppID: {}})
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{Privileged: true}},
		}, nil
	}
	upCalled := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   allowedAppID,
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    privileged: true\n",
	})
	if err != nil {
		t.Fatalf("expected deploy for allowlisted application to succeed, got: %v", err)
	}
	if !upCalled {
		t.Fatal("expected compose up to be called for an allowlisted deploy")
	}
}

func TestDeploy_AllowlistBypassIsPerApplicationNotGlobal(t *testing.T) {
	c := newTestClientWithAllowlist(t, map[string]struct{}{"019e1ce8-7938-71b8-be55-4b184f307a2d": {}})
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{Privileged: true}},
		}, nil
	}
	upCalled := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "a-different-application-id",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    privileged: true\n",
	})
	if err == nil {
		t.Fatal("expected deploy for a non-allowlisted application to be rejected, even with a non-empty allowlist")
	}
	if upCalled {
		t.Fatal("expected compose up to never be called for a rejected deploy")
	}
}

func TestDeploy_RestrictMountsToDeployDirRejectsMountOutsideDeploymentsDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.restrictMountsToDeployDir = true

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name: opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: "/data", Target: "/data"},
				},
			}},
		}, nil
	}
	upCalled := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    volumes:\n      - /data:/data\n",
	})
	if err == nil {
		t.Fatal("expected deploy with a bind mount outside the deployments directory to be rejected")
	}
	if upCalled {
		t.Fatal("expected compose up to never be called for a rejected deploy")
	}
}

func TestDeploy_RestrictMountsToDeployDirAllowsMountInsideDeploymentsDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.restrictMountsToDeployDir = true

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name: opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: filepath.Join(c.deploymentsDir, "billing", "data"), Target: "/data"},
				},
			}},
		}, nil
	}
	upCalled := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upCalled = true
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err != nil {
		t.Fatalf("expected deploy with a bind mount inside the deployments directory to succeed, got: %v", err)
	}
	if !upCalled {
		t.Fatal("expected compose up to be called")
	}
}

func TestDeploy_TranslatesBindMountForAllowlistedApplication(t *testing.T) {
	const allowedAppID = "019e1ce8-7938-71b8-be55-4b184f307a2d"

	c := newTestClientWithAllowlist(t, map[string]struct{}{allowedAppID: {}})
	c.deploymentsDir = t.TempDir()
	c.mu.Lock()
	c.hostDeploymentsDir = "/srv/orcacd/deployments"
	c.ready = true
	c.mu.Unlock()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name: opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{
				Privileged: true,
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: filepath.Join(c.deploymentsDir, "billing", "data"),
						Target: "/data",
					},
				},
			}},
		}, nil
	}
	var gotSource string
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		gotSource = project.Services["app"].Volumes[0].Source
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   allowedAppID,
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if gotSource != "/srv/orcacd/deployments/billing/data" {
		t.Errorf("bind source = %q, want %q", gotSource, "/srv/orcacd/deployments/billing/data")
	}
}

func TestDeploy_RestrictMountsAllowsTranslatedDeploymentBind(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.mu.Lock()
	c.hostDeploymentsDir = "/srv/orcacd/deployments"
	c.restrictMountsToDeployDir = true
	c.ready = true
	c.mu.Unlock()

	origLoad := loadProject
	origUp := upProject
	t.Cleanup(func() {
		loadProject = origLoad
		upProject = origUp
	})
	loadProject = func(_ context.Context, _ api.Compose, opts api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name: opts.ProjectName,
			Services: composetypes.Services{"app": composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: filepath.Join(c.deploymentsDir, "billing", "data"),
						Target: "/data",
					},
				},
			}},
		}, nil
	}
	var gotSource string
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		gotSource = project.Services["app"].Volumes[0].Source
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if gotSource != "/srv/orcacd/deployments/billing/data" {
		t.Errorf("bind source = %q, want %q", gotSource, "/srv/orcacd/deployments/billing/data")
	}
}

func TestRemove_DownsProjectAndRemovesDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origDown := downProject
	t.Cleanup(func() { downProject = origDown })

	var gotProject string
	var gotOptions api.DownOptions
	downProject = func(_ context.Context, _ api.Compose, projectName string, options api.DownOptions) error {
		gotProject = projectName
		gotOptions = options
		return nil
	}

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := c.Remove(t.Context(), DeleteRequest{ApplicationID: "app-1", ApplicationName: "billing"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if gotProject != "billing" {
		t.Fatalf("expected project %q, got %q", "billing", gotProject)
	}
	if !gotOptions.RemoveOrphans {
		t.Fatal("expected RemoveOrphans on compose down")
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf("expected deployment directory to be removed, stat err = %v", err)
	}
}

func TestRemove_RejectsUnsafeApplicationName(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	for _, name := range []string{"../bad", "bad/../name"} {
		if err := c.Remove(t.Context(), DeleteRequest{ApplicationID: "app-1", ApplicationName: name}); err == nil {
			t.Fatalf("expected Remove to reject application name %q", name)
		}
	}
}

func TestRemove_DownProjectError(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origDown := downProject
	t.Cleanup(func() { downProject = origDown })
	downProject = func(_ context.Context, _ api.Compose, _ string, _ api.DownOptions) error {
		return errors.New("down failed")
	}

	if err := c.Remove(t.Context(), DeleteRequest{ApplicationID: "app-1", ApplicationName: "billing"}); err == nil {
		t.Fatal("expected error when downProject fails")
	}
}

func TestRemove_NotConfigured(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = ""

	if err := c.Remove(t.Context(), DeleteRequest{ApplicationID: "app-1", ApplicationName: "billing"}); err == nil {
		t.Fatal("expected error when deployments directory is not configured")
	}
}

func TestNormalizeProjectName(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"billing", "billing", false},
		{"My App", "my-app", false},
		{"orcacd docs", "orcacd-docs", false},
		{"test/orcacd-docs", "test-orcacd-docs", false},
		{"Hello World!", "hello-world", false},
		{"  ---  ", "", true},
		{"___", "", true},
		{"123app", "123app", false},
	}
	for _, tt := range tests {
		got, err := normalizeProjectName(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("normalizeProjectName(%q): expected error, got %q", tt.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeProjectName(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("normalizeProjectName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
