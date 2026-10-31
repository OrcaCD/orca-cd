package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/utils"
)

// deploymentPathViolation rejects a host path inside the deployments directory
// that belongs to another application or traverses a symlink, and returns why,
// or "" if the path is allowed. Containers can write to their deployment
// directory, and the string-based checks cannot see where a symlink created
// there points to. The files of an application, such as its env files, must
// not be reachable for others, so a path that contains the deployments
// directory is rejected as well. Both are also checked for the path of the
// deployments directory on the Docker host.
func deploymentPathViolation(path string, rules pathRules) string {
	deploymentsDir, applicationDir := rules.deploymentsDir, rules.applicationDir
	if deploymentsDir == "" || !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)

	if hostDir := rules.hostDeploymentsDir; hostDir != "" {
		if isStrictParent(path, hostDir) {
			return "contains the deployments directory of the agent, which holds the files of other applications"
		}
		if rel, err := filepath.Rel(hostDir, path); err == nil && filepath.IsLocal(rel) {
			path = filepath.Join(deploymentsDir, rel)
		}
	}
	if isStrictParent(path, deploymentsDir) {
		return "contains the deployments directory of the agent, which holds the files of other applications"
	}

	if utils.IsPathWithinBase(deploymentsDir, path) != nil {
		return ""
	}
	if utils.IsPathWithinBase(applicationDir, path) != nil {
		return fmt.Sprintf("is inside the deployments directory but outside the application's directory %q", applicationDir)
	}
	link, err := firstSymlink(deploymentsDir, path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Sprintf("could not be checked for symlinks: %v; allow the agent to search the parent directories, or opt in to the agent's DAC_READ_SEARCH capability (bypasses read/search permissions for all files accessible to the agent)", err)
		}
		return fmt.Sprintf("could not be checked for symlinks: %v", err)
	}
	if link != "" {
		return fmt.Sprintf("traverses symlink %q, which is not allowed", link)
	}
	return ""
}

// isStrictParent reports whether parent is a directory above path.
func isStrictParent(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// firstSymlink returns the first existing path component below base that is a
// symlink, or "" if there is none. Missing components are fine: Docker creates
// them as directories.
func firstSymlink(base, path string) (string, error) {
	base = filepath.Clean(base)
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}

	current := base
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return current, nil
		}
	}
	return "", nil
}
