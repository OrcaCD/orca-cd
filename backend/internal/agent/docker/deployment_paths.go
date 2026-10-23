package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

type hostPath struct {
	description string
	path        string
}

// projectHostPaths lists every path that is read from the host: bind mount
// sources, env_file paths and the files of secrets and configs.
func projectHostPaths(project *composetypes.Project) []hostPath {
	var paths []hostPath
	for name, svc := range project.Services {
		for _, vol := range svc.Volumes {
			if vol.Type == composetypes.VolumeTypeBind {
				paths = append(paths, hostPath{fmt.Sprintf("service %q: bind mount source %q", name, vol.Source), vol.Source})
			}
		}
		for _, envFile := range svc.EnvFiles {
			paths = append(paths, hostPath{fmt.Sprintf("service %q: env_file %q", name, envFile.Path), envFile.Path})
		}
	}
	for name, secret := range project.Secrets {
		if secret.File != "" {
			paths = append(paths, hostPath{fmt.Sprintf("secret %q: file %q", name, secret.File), secret.File})
		}
	}
	for name, config := range project.Configs {
		if config.File != "" {
			paths = append(paths, hostPath{fmt.Sprintf("config %q: file %q", name, config.File), config.File})
		}
	}
	return paths
}

// checkDeploymentPaths rejects host paths inside the deployments directory that
// belong to another application or traverse a symlink. Containers can write to
// their deployment directory, and the string-based checks cannot see where a
// symlink created there points to.
func checkDeploymentPaths(project *composetypes.Project, deploymentsDir, applicationDir string) error {
	var violations []error
	for _, hp := range projectHostPaths(project) {
		if !filepath.IsAbs(hp.path) {
			continue
		}
		path := filepath.Clean(hp.path)
		if utils.IsPathWithinBase(deploymentsDir, path) != nil {
			continue
		}
		if utils.IsPathWithinBase(applicationDir, path) != nil {
			violations = append(violations, fmt.Errorf("%s is inside the deployments directory but outside the application's directory %q", hp.description, applicationDir))
			continue
		}
		link, err := firstSymlink(deploymentsDir, path)
		if err != nil {
			violations = append(violations, fmt.Errorf("%s: %w", hp.description, err))
			continue
		}
		if link != "" {
			violations = append(violations, fmt.Errorf("%s traverses symlink %q, which is not allowed", hp.description, link))
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("compose project violates deployment security policy: %w", errors.Join(violations...))
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
