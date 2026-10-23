package docker

import (
	"os"
	"path/filepath"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func setupDeploymentDirs(t *testing.T) (deploymentsDir, applicationDir string) {
	t.Helper()
	deploymentsDir = t.TempDir()
	applicationDir = filepath.Join(deploymentsDir, "billing")
	if err := os.MkdirAll(filepath.Join(applicationDir, "data"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return deploymentsDir, applicationDir
}

func TestCheckDeploymentPaths_AllowsPathsInsideApplicationDir(t *testing.T) {
	deploymentsDir, applicationDir := setupDeploymentDirs(t)

	for _, source := range []string{applicationDir, filepath.Join(applicationDir, "data"), filepath.Join(applicationDir, "not-created-yet/sub"), "/srv/shared"} {
		if err := checkDeploymentPaths(bindMountProject(source), deploymentsDir, applicationDir); err != nil {
			t.Fatalf("expected %q to be allowed, got: %v", source, err)
		}
	}
}

func TestCheckDeploymentPaths_RejectsOtherApplicationsAndDeploymentsRoot(t *testing.T) {
	deploymentsDir, applicationDir := setupDeploymentDirs(t)

	for _, source := range []string{deploymentsDir, filepath.Join(deploymentsDir, "other-app"), filepath.Join(applicationDir, "..", "other-app", "data")} {
		if err := checkDeploymentPaths(bindMountProject(source), deploymentsDir, applicationDir); err == nil {
			t.Fatalf("expected %q to be rejected", source)
		}
	}
}

func TestCheckDeploymentPaths_RejectsSymlinks(t *testing.T) {
	deploymentsDir, applicationDir := setupDeploymentDirs(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(applicationDir, "data", "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	source := filepath.Join(applicationDir, "data", "link", "sub")

	projects := map[string]*composetypes.Project{
		"bind mount": bindMountProject(source),
		"env_file":   envFileProject(source),
		"secret":     {Secrets: composetypes.Secrets{"s": {File: source}}},
		"config":     {Configs: composetypes.Configs{"c": {File: source}}},
	}
	for name, project := range projects {
		t.Run(name, func(t *testing.T) {
			if err := checkDeploymentPaths(project, deploymentsDir, applicationDir); err == nil {
				t.Fatalf("expected %s through a symlink to be rejected", name)
			}
		})
	}
}

func TestCheckDeploymentPaths_RejectsSymlinkedApplicationDir(t *testing.T) {
	deploymentsDir := t.TempDir()
	applicationDir := filepath.Join(deploymentsDir, "billing")
	if err := os.Symlink(t.TempDir(), applicationDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if err := checkDeploymentPaths(bindMountProject(filepath.Join(applicationDir, "data")), deploymentsDir, applicationDir); err == nil {
		t.Fatal("expected a path through a symlinked application directory to be rejected")
	}
}
