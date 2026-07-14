package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

var (
	errNotContainerized = errors.New("agent is not running in a Docker container")
	containerIDPattern  = regexp.MustCompile(`(?:^|[-/])([0-9a-f]{64})(?:\.scope)?(?:$|/)`)
	mountRootPattern    = regexp.MustCompile(`(?:^|/)containers/([0-9a-f]{64})/(?:hostname|hosts|resolv\.conf)$`)
)

func detectContainerID(readFile func(string) ([]byte, error)) (string, error) {
	cgroup, cgroupErr := readFile("/proc/self/cgroup")
	if cgroupErr == nil {
		if id := containerIDFromCgroup(cgroup); id != "" {
			return id, nil
		}
	}

	mountInfo, mountInfoErr := readFile("/proc/self/mountinfo")
	if mountInfoErr == nil {
		if id := containerIDFromMountInfo(mountInfo); id != "" {
			return id, nil
		}
	}

	if cgroupErr != nil && !errors.Is(cgroupErr, os.ErrNotExist) {
		return "", fmt.Errorf("read /proc/self/cgroup: %w", cgroupErr)
	}
	if mountInfoErr != nil && !errors.Is(mountInfoErr, os.ErrNotExist) {
		return "", fmt.Errorf("read /proc/self/mountinfo: %w", mountInfoErr)
	}
	return "", errNotContainerized
}

func containerIDFromCgroup(data []byte) string {
	for line := range strings.Lines(string(data)) {
		match := containerIDPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func containerIDFromMountInfo(data []byte) string {
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		switch fields[4] {
		case "/etc/hostname", "/etc/hosts", "/etc/resolv.conf":
		default:
			continue
		}
		match := mountRootPattern.FindStringSubmatch(fields[3])
		if len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

type containerInspector interface {
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

func detectHostDeploymentsDir(
	ctx context.Context,
	inspector containerInspector,
	containerID func() (string, error),
	deploymentsDir string,
) (string, error) {
	id, err := containerID()
	if err != nil {
		return "", fmt.Errorf("identify agent container: %w", err)
	}
	if id == "" {
		return "", fmt.Errorf("agent container ID is empty")
	}

	result, err := inspector.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect agent container %q: %w", id, err)
	}
	if result.Container.ID != id {
		return "", fmt.Errorf("inspected container ID %q does not match agent container ID %q", result.Container.ID, id)
	}
	return hostBindSource(result.Container.Mounts, filepath.Clean(deploymentsDir))
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
