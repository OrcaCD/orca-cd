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
// there points to.
func deploymentPathViolation(path, deploymentsDir, applicationDir string) string {
	if deploymentsDir == "" || !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)
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
