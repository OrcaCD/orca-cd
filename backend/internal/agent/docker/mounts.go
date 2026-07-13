package docker

import (
	"fmt"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func translateBindMountSources(project *composetypes.Project, deploymentsDir, hostDeploymentsDir string) error {
	if hostDeploymentsDir == "" {
		return nil
	}

	agentBase, err := filepath.Abs(deploymentsDir)
	if err != nil {
		return fmt.Errorf("resolve deployments directory: %w", err)
	}
	hostBase := filepath.Clean(hostDeploymentsDir)

	for serviceName, service := range project.Services {
		for i, volume := range service.Volumes {
			if volume.Type != composetypes.VolumeTypeBind {
				continue
			}

			source := volume.Source
			if !filepath.IsAbs(source) {
				source = filepath.Join(agentBase, source)
			}
			rel, err := filepath.Rel(agentBase, filepath.Clean(source))
			if err != nil {
				return fmt.Errorf("service %q: resolve bind mount source %q: %w", serviceName, volume.Source, err)
			}
			if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}

			service.Volumes[i].Source = filepath.Join(hostBase, rel)
		}
		project.Services[serviceName] = service
	}

	return nil
}
