package docker

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
)

func readOverride(t *testing.T, dir string) []byte {
	t.Helper()
	//nolint:gosec // path is built from t.TempDir() and a constant
	content, err := os.ReadFile(filepath.Join(dir, overrideFileName))
	if err != nil {
		t.Fatalf("ReadFile override: %v", err)
	}
	return content
}

func TestWriteComposeOverride_NoServices(t *testing.T) {
	dir := t.TempDir()
	if err := writeComposeOverride(dir, orcaOverride(&composetypes.Project{}, "app-123")); err != nil {
		t.Fatalf("writeComposeOverride: %v", err)
	}
	project := loadMergedProject(t, "services:\n  app:\n    image: nginx\n", readOverride(t, dir))
	if len(project.Services["app"].Labels) != 0 {
		t.Errorf("expected an empty override to leave labels untouched, got %v", project.Services["app"].Labels)
	}
}

func TestWriteComposeOverride_RoundTripsSpecialCharacters(t *testing.T) {
	appID := "id: \"with\" #special\nchars"
	dir := t.TempDir()
	project := &composetypes.Project{Services: composetypes.Services{"app": {}}}
	if err := writeComposeOverride(dir, orcaOverride(project, appID)); err != nil {
		t.Fatalf("writeComposeOverride: %v", err)
	}

	merged := loadMergedProject(t, "services:\n  app:\n    image: nginx\n", readOverride(t, dir))
	if got := merged.Services["app"].Labels[labelApplicationID]; got != appID {
		t.Errorf("expected label to round-trip, got %q", got)
	}
}

// Differing labels would make compose recreate containers on manual runs.
func TestOrcaOverride_MatchesApplyOrcaLabels(t *testing.T) {
	base := "services:\n  app:\n    image: nginx\n    labels:\n      custom: keep\n  db:\n    image: postgres\n"
	project := loadMergedProject(t, base, nil)

	dir := t.TempDir()
	if err := writeComposeOverride(dir, orcaOverride(project, "app-123")); err != nil {
		t.Fatalf("writeComposeOverride: %v", err)
	}
	merged := loadMergedProject(t, base, readOverride(t, dir))

	applyOrcaLabels(project, "app-123")

	for name, service := range project.Services {
		if !maps.Equal(service.Labels, merged.Services[name].Labels) {
			t.Errorf("service %q: in-memory labels %v differ from override labels %v", name, service.Labels, merged.Services[name].Labels)
		}
	}
	if merged.Services["app"].Labels["custom"] != "keep" {
		t.Error("expected existing labels to be preserved")
	}
}

func TestWriteComposeOverride_ReplacesStaleServices(t *testing.T) {
	dir := t.TempDir()
	two := &composetypes.Project{Services: composetypes.Services{"old": {}, "app": {}}}
	one := &composetypes.Project{Services: composetypes.Services{"app": {}}}

	if err := writeComposeOverride(dir, orcaOverride(two, "app-123")); err != nil {
		t.Fatalf("writeComposeOverride: %v", err)
	}
	if err := writeComposeOverride(dir, orcaOverride(one, "app-123")); err != nil {
		t.Fatalf("writeComposeOverride: %v", err)
	}

	project := loadMergedProject(t, "services:\n  app:\n    image: nginx\n", readOverride(t, dir))
	if _, ok := project.Services["old"]; ok {
		t.Error("expected stale service to be dropped from the override")
	}
}

func TestDeploy_WritesLabelOverride(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()

	loadProject = func(_ context.Context, _ api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{
			Name:     options.ProjectName,
			Services: composetypes.Services{"app": {Name: "app", Image: "nginx"}},
		}, nil
	}
	upProject = func(_ context.Context, _ api.Compose, _ *composetypes.Project, _ api.UpOptions) error {
		return nil
	}

	composeFile := "services:\n  app:\n    image: nginx\n"
	if err := c.Deploy(t.Context(), DeployRequest{
		ApplicationID:   "app-123",
		ApplicationName: "billing",
		ComposeFile:     composeFile,
	}); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	dir := filepath.Join(c.deploymentsDir, "billing")
	project := loadMergedProject(t, composeFile, readOverride(t, dir))
	labels := project.Services["app"].Labels
	if labels[labelApplicationID] != "app-123" || labels[labelManagedBy] != "orca-cd" {
		t.Errorf("expected orca labels in override, got %v", labels)
	}

	//nolint:gosec // path is built from t.TempDir() and constants
	base, err := os.ReadFile(filepath.Join(dir, composeFileName))
	if err != nil {
		t.Fatalf("ReadFile compose: %v", err)
	}
	if string(base) != composeFile {
		t.Error("compose file must not be modified by the override")
	}
}

// loadMergedProject merges base and override like `docker compose` does.
func loadMergedProject(t *testing.T, base string, override []byte) *composetypes.Project {
	t.Helper()
	files := []composetypes.ConfigFile{{Filename: composeFileName, Content: []byte(base)}}
	if override != nil {
		files = append(files, composetypes.ConfigFile{Filename: overrideFileName, Content: override})
	}
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir:  t.TempDir(),
		ConfigFiles: files,
	}, func(o *loader.Options) { o.SetProjectName("test", true) })
	if err != nil {
		t.Fatalf("failed to load compose project: %v", err)
	}
	return project
}
