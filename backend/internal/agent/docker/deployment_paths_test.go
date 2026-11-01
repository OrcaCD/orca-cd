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

func TestDeploymentPaths_ProtectEnvFilesOfOtherApplications(t *testing.T) {
	rules := setupDeploymentDirs(t)
	rules.applicationDir = filepath.Join(rules.deploymentsDir, "attacker")
	hostDir := filepath.Join(t.TempDir(), "host", "deployments")
	rules.hostDeploymentsDir = hostDir

	victimEnv := filepath.Join("billing", ".env")
	sources := map[string]string{
		"agent path of the env file":      filepath.Join(rules.deploymentsDir, victimEnv),
		"host path of the env file":       filepath.Join(hostDir, victimEnv),
		"host path of the other app dir":  filepath.Join(hostDir, "billing"),
		"deployments directory":           rules.deploymentsDir,
		"host deployments directory":      hostDir,
		"directory above the agent dir":   filepath.Dir(rules.deploymentsDir),
		"directory above the host dir":    filepath.Dir(hostDir),
		"host path with parent reference": filepath.Join(hostDir, "attacker", "..", "billing", ".env"),
	}
	// The agent reads env_file at the literal path, so host paths do not apply.
	hostOnly := map[string]bool{
		"host path of the env file":       true,
		"host path of the other app dir":  true,
		"host deployments directory":      true,
		"directory above the host dir":    true,
		"host path with parent reference": true,
	}
	for name, source := range sources {
		for _, restrict := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				rules := rules
				if restrict {
					rules.restrictMountsDir = rules.deploymentsDir
				}
				for kind, project := range map[string]*composetypes.Project{
					"bind mount": bindMountProject(source),
					"env_file":   envFileProject(source),
					"secret":     {Secrets: composetypes.Secrets{"s": {File: source}}},
				} {
					if kind == "env_file" && hostOnly[name] {
						continue
					}
					if err := checkDeployPolicy(project, rules); err == nil {
						t.Fatalf("expected %s of %q to be rejected (restrict=%v)", kind, source, restrict)
					}
				}
			})
		}
	}
}

func TestDeploymentPaths_EnvFileIsCheckedAsAgentPath(t *testing.T) {
	rules := setupDeploymentDirs(t)
	rules.applicationDir = filepath.Join(rules.deploymentsDir, "attacker")
	hostDir := filepath.Join(t.TempDir(), "host", "deployments")
	rules.hostDeploymentsDir = hostDir

	hostPath := filepath.Join(hostDir, "billing", ".env")
	if err := checkDeployPolicy(envFileProject(hostPath), rules); err != nil {
		t.Fatalf("expected env_file %q not to be mapped to the agent's deployments directory, got: %v", hostPath, err)
	}
	if err := checkDeployPolicy(bindMountProject(hostPath), rules); err == nil {
		t.Fatalf("expected the bind mount of %q to be mapped and rejected", hostPath)
	}

	agentPath := filepath.Join(rules.deploymentsDir, "billing", ".env")
	if err := checkDeployPolicy(envFileProject(agentPath), rules); err == nil {
		t.Fatalf("expected env_file %q of another application to be rejected", agentPath)
	}
}

func TestDeploymentPaths_AllowsOwnDirectoryByHostPath(t *testing.T) {
	rules := setupDeploymentDirs(t)
	hostDir := filepath.Join(t.TempDir(), "host", "deployments")
	rules.hostDeploymentsDir = hostDir

	for _, source := range []string{
		filepath.Join(hostDir, "billing"),
		filepath.Join(hostDir, "billing", ".env"),
		filepath.Join(hostDir, "billing", "data", "new"),
		filepath.Join(rules.applicationDir, ".env"),
	} {
		if err := checkDeployPolicy(bindMountProject(source), rules); err != nil {
			t.Fatalf("expected %q to be allowed, got: %v", source, err)
		}
		if err := checkDeployPolicy(envFileProject(source), rules); err != nil {
			t.Fatalf("expected env_file %q to be allowed, got: %v", source, err)
		}
	}
}

func TestDeploymentPaths_HostPathsAreSymlinkChecked(t *testing.T) {
	rules := setupDeploymentDirs(t)
	hostDir := filepath.Join(t.TempDir(), "host", "deployments")
	rules.hostDeploymentsDir = hostDir
	link := filepath.Join(rules.applicationDir, "data", "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	hostLink := filepath.Join(hostDir, "billing", "data", "link", "sub")
	requireErrorContains(t, checkDeployPolicy(bindMountProject(hostLink), rules), "traverses symlink")
}
