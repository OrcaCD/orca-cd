package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type containerInspector interface {
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
}

func detectHostDeploymentsDir(
	ctx context.Context,
	inspector containerInspector,
	hostname func() (string, error),
	deploymentsDir string,
) (string, error) {
	containerID, err := hostname()
	if err != nil {
		return "", fmt.Errorf("get agent container hostname: %w", err)
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" {
		return "", fmt.Errorf("agent container hostname is empty")
	}

	target := filepath.Clean(deploymentsDir)
	result, directErr := inspector.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if directErr == nil {
		if strings.HasPrefix(result.Container.ID, containerID) {
			hostSource, mountErr := hostBindSource(result.Container.Mounts, target)
			if mountErr == nil {
				return hostSource, nil
			}
			directErr = mountErr
		} else {
			directErr = fmt.Errorf(
				"inspected container ID %q does not match agent hostname %q",
				result.Container.ID,
				containerID,
			)
		}
	}

	containers, err := inspector.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect agent container %q: %v; list running containers: %w", containerID, directErr, err)
	}

	type candidate struct {
		id         string
		hostSource string
	}
	var candidates []candidate
	var candidateErr error
	for _, summary := range containers.Items {
		if !hasMountDestination(summary.Mounts, target) {
			continue
		}

		candidateResult, err := inspector.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
		if err != nil {
			candidateErr = fmt.Errorf("inspect candidate container %q: %w", summary.ID, err)
			continue
		}
		if candidateResult.Container.Config == nil || candidateResult.Container.Config.Hostname != containerID {
			continue
		}

		hostSource, err := hostBindSource(candidateResult.Container.Mounts, target)
		if err != nil {
			candidateErr = err
			continue
		}
		candidates = append(candidates, candidate{id: summary.ID, hostSource: hostSource})
	}

	switch len(candidates) {
	case 1:
		return candidates[0].hostSource, nil
	case 0:
		if candidateErr != nil {
			return "", fmt.Errorf("identify agent container with hostname %q: direct lookup failed: %v; candidate lookup failed: %w", containerID, directErr, candidateErr)
		}
		return "", fmt.Errorf("identify agent container with hostname %q: direct lookup failed: %v; no matching running container", containerID, directErr)
	default:
		candidateIDs := make([]string, 0, len(candidates))
		for _, match := range candidates {
			candidateIDs = append(candidateIDs, match.id)
		}
		return "", fmt.Errorf("identify agent container with hostname %q: multiple matching containers: %s", containerID, strings.Join(candidateIDs, ", "))
	}
}

func hasMountDestination(mountPoints []containerapi.MountPoint, target string) bool {
	for _, mountPoint := range mountPoints {
		if filepath.Clean(mountPoint.Destination) == target {
			return true
		}
	}
	return false
}

func hostBindSource(mountPoints []containerapi.MountPoint, target string) (string, error) {
	for _, mountPoint := range mountPoints {
		if filepath.Clean(mountPoint.Destination) != target {
			continue
		}
		if mountPoint.Type != mount.TypeBind {
			return "", fmt.Errorf("agent deployment directory %q is mounted as %q, not a bind mount", target, mountPoint.Type)
		}
		if mountPoint.Source == "" || !filepath.IsAbs(mountPoint.Source) {
			return "", fmt.Errorf("agent deployment bind source %q is not an absolute host path", mountPoint.Source)
		}
		return filepath.Clean(mountPoint.Source), nil
	}
	return "", fmt.Errorf("agent container has no bind mount at deployment directory %q", target)
}

func translateBindMountSources(project *composetypes.Project, deploymentsDir, hostDeploymentsDir string) error {
	if hostDeploymentsDir == "" {
		return nil
	}

	agentBase, err := filepath.Abs(deploymentsDir)
	if err != nil {
		return fmt.Errorf("resolve deployments directory: %w", err)
	}
	if !filepath.IsAbs(hostDeploymentsDir) {
		return fmt.Errorf("host deployments directory %q is not absolute", hostDeploymentsDir)
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
			relativeSource, err := filepath.Rel(agentBase, filepath.Clean(source))
			if err != nil {
				return fmt.Errorf("service %q: resolve bind mount source %q: %w", serviceName, volume.Source, err)
			}
			if relativeSource == ".." || strings.HasPrefix(relativeSource, ".."+string(filepath.Separator)) {
				continue
			}

			service.Volumes[i].Source = filepath.Join(hostBase, relativeSource)
		}
		project.Services[serviceName] = service
	}

	return nil
}
