package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
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

	if gotUpOptions.Start.Wait {
		t.Fatal("expected compose up not to block on healthchecks; health is reported after deployment")
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

func TestDeploy_DeleteOldImagesRemovesReplacedImages(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	upDone, removed := stubImageCleanup(t, []string{"sha256:app-1.0.0"}, []string{"sha256:app-1.1.0"})
	loadProject = func(_ context.Context, _ api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{Name: options.ProjectName}, nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upDone()
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: ghcr.io/orcacd/app:1.1.0\n",
		DeleteOldImages: true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(*removed) != 1 || (*removed)[0] != "sha256:app-1.0.0" {
		t.Errorf("expected the replaced image to be removed, got %v", *removed)
	}
}

func TestDeploy_KeepsOldImagesWhenDisabled(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	upDone, removed := stubImageCleanup(t, []string{"sha256:app-1.0.0"}, []string{"sha256:app-1.1.0"})
	loadProject = func(_ context.Context, _ api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{Name: options.ProjectName}, nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upDone()
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: ghcr.io/orcacd/app:1.1.0\n",
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(*removed) != 0 {
		t.Errorf("expected no images to be removed, got %v", *removed)
	}
}

func TestDeploy_DeleteOldImages_SkipsWhenImagesUnchanged(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	upDone, removed := stubImageCleanup(t, []string{"sha256:app-1.0.0"}, []string{"sha256:app-1.0.0"})
	loadProject = func(_ context.Context, _ api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{Name: options.ProjectName}, nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		upDone()
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: ghcr.io/orcacd/app:1.0.0\n",
		DeleteOldImages: true,
	})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(*removed) != 0 {
		t.Errorf("expected no images to be removed, got %v", *removed)
	}
}

func stubProjectContainers(t *testing.T, items []container.Summary, err error) {
	t.Helper()
	orig := listProjectContainers
	t.Cleanup(func() { listProjectContainers = orig })
	listProjectContainers = func(_ context.Context, _ client.APIClient, _ string) ([]container.Summary, error) {
		return items, err
	}
}

func TestEnsureProjectManaged(t *testing.T) {
	managed := container.Summary{Labels: map[string]string{labelManagedBy: managedByValue}}
	foreign := container.Summary{Labels: map[string]string{"com.docker.compose.project": "billing"}}

	tests := []struct {
		name    string
		items   []container.Summary
		listErr error
		wantErr bool
	}{
		{name: "project does not exist"},
		{name: "all containers managed", items: []container.Summary{managed, managed}},
		{name: "foreign project", items: []container.Summary{foreign}, wantErr: true},
		{name: "mixed project", items: []container.Summary{managed, foreign}, wantErr: true},
		{name: "list error", listErr: errors.New("daemon unavailable"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t)
			stubProjectContainers(t, tt.items, tt.listErr)

			err := c.ensureProjectManaged(t.Context(), "billing")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ensureProjectManaged() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeploy_RejectsForeignProject(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	stubProjectContainers(t, []container.Summary{{Labels: map[string]string{}}}, nil)

	origUp := upProject
	t.Cleanup(func() { upProject = origUp })
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		t.Fatal("compose up must not run for a foreign project")
		return nil
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err == nil {
		t.Fatal("expected deploy to reject a project not managed by OrcaCD")
	}
	if _, statErr := os.Stat(filepath.Join(c.deploymentsDir, "billing")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no deployment directory to be created, stat err = %v", statErr)
	}
}

func TestRemove_RejectsForeignProject(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	stubProjectContainers(t, []container.Summary{{Labels: map[string]string{}}}, nil)

	origDown := downProject
	t.Cleanup(func() { downProject = origDown })
	downProject = func(_ context.Context, _ api.Compose, _ string, _ api.DownOptions) error {
		t.Fatal("compose down must not run for a foreign project")
		return nil
	}

	if err := c.Remove(t.Context(), DeleteRequest{ApplicationID: "app-1", ApplicationName: "billing"}); err == nil {
		t.Fatal("expected remove to reject a project not managed by OrcaCD")
	}
}

func stubPrivilegedProjectLoad(t *testing.T) (upCalled *bool) {
	t.Helper()
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
	called := false
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		called = true
		return nil
	}
	return &called
}

func TestDeploy_RejectedComposeFileIsNotKept(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	upCalled := stubPrivilegedProjectLoad(t)

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    privileged: true\n",
	})
	if err == nil {
		t.Fatal("expected deploy of a privileged service to be rejected")
	}
	if *upCalled {
		t.Fatal("expected compose up to never be called for a rejected deploy")
	}

	composePath := filepath.Join(c.deploymentsDir, "billing", composeFileName)
	if _, err := os.Lstat(composePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the rejected compose file to be removed, got %v", err)
	}
}

func TestDeploy_RejectedComposeFileRestoresPreviousDeployment(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	stubPrivilegedProjectLoad(t)

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const previous = "services:\n  app:\n    image: img:1.0.0\n"
	composePath := filepath.Join(appDir, composeFileName)
	if err := os.WriteFile(composePath, []byte(previous), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "019e1ce8-7938-71b8-be55-4b184f307a2d",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    privileged: true\n",
	})
	if err == nil {
		t.Fatal("expected deploy of a privileged service to be rejected")
	}

	//nolint:gosec // composePath is built from t.TempDir() and a fixed application name
	content, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != previous {
		t.Fatalf("expected the previous compose file to be restored, got %q", content)
	}
}

func TestDeploy_LoadErrorRestoresPreviousDeployment(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	origLoad := loadProject
	t.Cleanup(func() { loadProject = origLoad })
	loadProject = func(_ context.Context, _ api.Compose, _ api.ProjectLoadOptions) (*composetypes.Project, error) {
		return nil, errors.New("load error")
	}

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const previous = "services:\n  app:\n    image: img:1.0.0\n"
	composePath := filepath.Join(appDir, composeFileName)
	if err := os.WriteFile(composePath, []byte(previous), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "not: [valid",
	}); err == nil {
		t.Fatal("expected error when loadProject fails")
	}

	//nolint:gosec // composePath is built from t.TempDir() and a fixed application name
	if content, _ := os.ReadFile(composePath); string(content) != previous {
		t.Fatalf("expected the previous compose file to be restored, got %q", content)
	}
}

func TestDeploy_ReplacesSymlinkedComposeFile(t *testing.T) {
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
		return nil
	}

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "agent-file")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	for _, name := range []string{composeFileName, overrideFileName} {
		if err := os.Symlink(outside, filepath.Join(appDir, name)); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
	}

	req := DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	}
	if err := c.Deploy(t.Context(), req); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	//nolint:gosec // path is built from t.TempDir()
	if content, _ := os.ReadFile(outside); string(content) != "original" {
		t.Fatalf("expected the symlink target outside the deployment directory to stay unchanged, got %q", content)
	}
	for _, name := range []string{composeFileName, overrideFileName} {
		info, err := os.Lstat(filepath.Join(appDir, name))
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("expected %s to be replaced by a regular file, got mode %v", name, info.Mode())
		}
	}
}

func TestDeploy_RejectsSymlinkedApplicationDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(c.deploymentsDir, "billing")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     "services:\n  app:\n    image: img:latest\n",
	})
	if err == nil {
		t.Fatal("expected deploy into a symlinked application directory to be rejected")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("expected nothing to be written through the symlink, got %v", entries)
	}
}

func TestDeploy_RejectsBindMountThroughSymlinkInApplicationDir(t *testing.T) {
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	c.restrictMountsToDeployDir = true

	appDir := filepath.Join(c.deploymentsDir, "billing")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(appDir, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

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
					{Type: composetypes.VolumeTypeBind, Source: filepath.Join(opts.WorkingDir, "link"), Target: "/data"},
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
		ComposeFile:     "services:\n  app:\n    image: img:latest\n    volumes:\n      - ./link:/data\n",
	})
	if err == nil {
		t.Fatal("expected a bind mount through a symlink to be rejected")
	}
	if upCalled {
		t.Fatal("expected compose up to never be called for a rejected deploy")
	}
}
