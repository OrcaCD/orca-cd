package docker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestDeploymentPaths_RejectsInaccessiblePathsWithGuidance(t *testing.T) {
	rules := setupDeploymentDirs(t)
	parent := filepath.Join(rules.applicationDir, "data")
	source := filepath.Join(parent, "private")
	if err := os.Mkdir(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		//nolint:gosec // Restore directory traversal so t.TempDir can clean up.
		if err := os.Chmod(parent, 0o750); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Lstat(source); !errors.Is(err, os.ErrPermission) {
		if err == nil {
			t.Skip("process can bypass directory permissions")
		}
		t.Fatal(err)
	}

	err := checkDeployPolicy(bindMountProject(source), rules)
	requireErrorContains(t, err, "could not be checked for symlinks")
	requireErrorContains(t, err, "allow the agent to search the parent directories")
	requireErrorContains(t, err, "opt in to the agent's DAC_READ_SEARCH capability")
	requireErrorContains(t, err, "bypasses read/search permissions for all files accessible to the agent")
}

func setupDeploymentDirs(t *testing.T) pathRules {
	t.Helper()
	deploymentsDir := t.TempDir()
	applicationDir := filepath.Join(deploymentsDir, "billing")
	if err := os.MkdirAll(filepath.Join(applicationDir, "data"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return pathRules{deploymentsDir: deploymentsDir, applicationDir: applicationDir}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected an error containing %q, got: %v", want, err)
	}
}

func TestDeploymentPaths_AllowsPathsInsideApplicationDir(t *testing.T) {
	rules := setupDeploymentDirs(t)

	for _, source := range []string{rules.applicationDir, filepath.Join(rules.applicationDir, "data"), filepath.Join(rules.applicationDir, "not-created-yet/sub"), "/srv/shared"} {
		if err := checkDeployPolicy(bindMountProject(source), rules); err != nil {
			t.Fatalf("expected %q to be allowed, got: %v", source, err)
		}
	}
}

func TestDeploymentPaths_RejectsOtherApplicationsAndDeploymentsRoot(t *testing.T) {
	rules := setupDeploymentDirs(t)

	for _, source := range []string{rules.deploymentsDir, filepath.Join(rules.deploymentsDir, "other-app"), filepath.Join(rules.applicationDir, "..", "other-app", "data")} {
		for name, restrictMountsDir := range map[string]string{"deny-list": "", "restrict": rules.deploymentsDir} {
			t.Run(name+" "+source, func(t *testing.T) {
				rules := rules
				rules.restrictMountsDir = restrictMountsDir
				err := checkDeployPolicy(bindMountProject(source), rules)
				requireErrorContains(t, err, "outside the application's directory")
			})
		}
	}
}

func TestDeploymentPaths_RejectsSymlinks(t *testing.T) {
	rules := setupDeploymentDirs(t)
	link := filepath.Join(rules.applicationDir, "data", "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	source := filepath.Join(link, "sub")

	projects := map[string]*composetypes.Project{
		"bind mount": bindMountProject(source),
		"env_file":   envFileProject(source),
		"secret":     {Secrets: composetypes.Secrets{"s": {File: source}}},
		"config":     {Configs: composetypes.Configs{"c": {File: source}}},
	}
	for name, project := range projects {
		t.Run(name, func(t *testing.T) {
			err := checkDeployPolicy(project, rules)
			requireErrorContains(t, err, "traverses symlink \""+link+"\"")
		})
	}
}

func TestDeploymentPaths_RejectsSymlinkedApplicationDir(t *testing.T) {
	deploymentsDir := t.TempDir()
	applicationDir := filepath.Join(deploymentsDir, "billing")
	if err := os.Symlink(t.TempDir(), applicationDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	rules := pathRules{deploymentsDir: deploymentsDir, applicationDir: applicationDir}
	err := checkDeployPolicy(bindMountProject(filepath.Join(applicationDir, "data")), rules)
	requireErrorContains(t, err, "traverses symlink \""+applicationDir+"\"")
}
