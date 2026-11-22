package docker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var (
	ErrContainerNotFound = errors.New("container not found")
	ErrDaemonUnreachable = errors.New("the Docker daemon is not reachable")
)

const minContainerIDPrefix = 12

func (c *Client) ListContainers(ctx context.Context, appID string) ([]*messages.ContainerInfo, error) {
	if !c.Ready() {
		return nil, ErrDaemonUnreachable
	}
	return listContainerInfos(ctx, c.cli.Client(), appID)
}

func (c *Client) InspectContainer(ctx context.Context, appID, containerID string) (string, error) {
	if !c.Ready() {
		return "", ErrDaemonUnreachable
	}
	return inspectSanitized(ctx, c.cli.Client(), appID, containerID)
}

// resolveContainer only accepts containers of the application, so the hub
// cannot read other containers of the host. Callers must use the returned ID,
// never the caller-supplied one.
func resolveContainer(ctx context.Context, cli client.APIClient, appID, containerID string) (container.Summary, error) {
	if len(containerID) < minContainerIDPrefix {
		return container.Summary{}, ErrContainerNotFound
	}
	items, err := listApplicationContainers(ctx, cli, appID)
	if err != nil {
		return container.Summary{}, fmt.Errorf("list containers: %w", err)
	}
	for _, item := range items {
		if isOneOff(item) || !strings.HasPrefix(item.ID, containerID) {
			continue
		}
		return item, nil
	}
	return container.Summary{}, ErrContainerNotFound
}

func isOneOff(item container.Summary) bool {
	return item.Labels[api.OneoffLabel] == "True"
}

func inspectSanitized(ctx context.Context, cli client.APIClient, appID, containerID string) (string, error) {
	item, err := resolveContainer(ctx, cli, appID, containerID)
	if err != nil {
		return "", err
	}
	result, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", ErrContainerNotFound
		}
		return "", fmt.Errorf("inspect container: %w", err)
	}
	return sanitizeInspect(result.Raw)
}

func listContainerInfos(ctx context.Context, cli client.APIClient, appID string) ([]*messages.ContainerInfo, error) {
	items, err := listApplicationContainers(ctx, cli, appID)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	infos := make([]*messages.ContainerInfo, 0, len(items))
	for _, item := range items {
		if isOneOff(item) {
			continue
		}
		result, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if err != nil {
			// Removed between the list and the inspect.
			if cerrdefs.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("inspect container %q: %w", item.ID, err)
		}
		infos = append(infos, buildContainerInfo(ctx, cli, item, result.Container))
	}

	slices.SortFunc(infos, func(a, b *messages.ContainerInfo) int {
		return cmp.Or(cmp.Compare(a.Service, b.Service), cmp.Compare(a.Name, b.Name))
	})
	return infos, nil
}

func buildContainerInfo(ctx context.Context, cli client.APIClient, item container.Summary, inspected container.InspectResponse) *messages.ContainerInfo {
	info := &messages.ContainerInfo{
		Id:           item.ID,
		Name:         strings.TrimPrefix(inspected.Name, "/"),
		Service:      serviceName(item),
		Image:        item.Image,
		ImageId:      item.ImageID,
		RestartCount: clampInt32(inspected.RestartCount),
		CreatedAtMs:  parseDockerTime(inspected.Created),
	}
	if info.Name == "" && len(item.Names) > 0 {
		info.Name = strings.TrimPrefix(item.Names[0], "/")
	}
	if n, err := strconv.ParseInt(item.Labels[api.ContainerNumberLabel], 10, 32); err == nil {
		info.ContainerNumber = int32(n)
	}

	state := inspected.State
	if state == nil {
		state = &container.State{Status: item.State}
	}
	info.State = string(state.Status)
	info.ExitCode = clampInt32(state.ExitCode)
	info.OomKilled = state.OOMKilled
	info.Error = state.Error
	info.StartedAtMs = parseDockerTime(state.StartedAt)
	info.FinishedAtMs = parseDockerTime(state.FinishedAt)

	stateSummary := container.Summary{State: state.Status}
	if state.Health != nil {
		info.HealthRaw = string(state.Health.Status)
		info.HealthFailingStreak = clampInt32(state.Health.FailingStreak)
		stateSummary.Health = &container.HealthSummary{Status: state.Health.Status, FailingStreak: state.Health.FailingStreak}
	}
	info.Health = aggregateHealth([]container.Summary{stateSummary}).Proto()

	if hostConfig := inspected.HostConfig; hostConfig != nil {
		info.LogDriver = hostConfig.LogConfig.Type
		info.RestartPolicy = string(hostConfig.RestartPolicy.Name)
		if retries := hostConfig.RestartPolicy.MaximumRetryCount; retries > 0 {
			info.RestartPolicy += ":" + strconv.Itoa(retries)
		}
	}

	if item.ImageID != "" {
		if digests, err := getLocalDigests(ctx, cli, item.ImageID); err == nil {
			info.ImageDigest = pinnedReference(item.Image, digests)
		}
	}

	info.Ports, info.Networks = portsAndNetworks(inspected.NetworkSettings)
	info.Mounts = mountInfos(inspected.Mounts)
	return info
}

func portsAndNetworks(settings *container.NetworkSettings) ([]*messages.PortBinding, []*messages.NetworkInfo) {
	if settings == nil {
		return nil, nil
	}

	var ports []*messages.PortBinding
	for port, bindings := range settings.Ports {
		base := &messages.PortBinding{PrivatePort: uint32(port.Num()), Protocol: string(port.Proto())}
		if len(bindings) == 0 {
			ports = append(ports, base)
			continue
		}
		for _, binding := range bindings {
			published := &messages.PortBinding{PrivatePort: base.PrivatePort, Protocol: base.Protocol}
			if binding.HostIP.IsValid() {
				published.Ip = binding.HostIP.String()
			}
			if public, err := strconv.ParseUint(binding.HostPort, 10, 32); err == nil {
				published.PublicPort = uint32(public)
			}
			ports = append(ports, published)
		}
	}
	slices.SortFunc(ports, func(a, b *messages.PortBinding) int {
		return cmp.Or(
			cmp.Compare(a.PrivatePort, b.PrivatePort),
			cmp.Compare(a.Protocol, b.Protocol),
			cmp.Compare(a.Ip, b.Ip),
			cmp.Compare(a.PublicPort, b.PublicPort),
		)
	})

	networks := make([]*messages.NetworkInfo, 0, len(settings.Networks))
	for name, endpoint := range settings.Networks {
		if endpoint == nil {
			continue
		}
		info := &messages.NetworkInfo{Name: name, Aliases: slices.Clone(endpoint.Aliases)}
		if endpoint.IPAddress.IsValid() {
			info.IpAddress = endpoint.IPAddress.String()
		}
		networks = append(networks, info)
	}
	slices.SortFunc(networks, func(a, b *messages.NetworkInfo) int { return cmp.Compare(a.Name, b.Name) })
	return ports, networks
}

// mountInfos omits the host-side source, which is only part of the inspect output.
func mountInfos(mounts []container.MountPoint) []*messages.MountInfo {
	infos := make([]*messages.MountInfo, 0, len(mounts))
	for _, m := range mounts {
		infos = append(infos, &messages.MountInfo{
			Type:        string(m.Type),
			Name:        m.Name,
			Destination: m.Destination,
			ReadOnly:    !m.RW,
		})
	}
	slices.SortFunc(infos, func(a, b *messages.MountInfo) int { return cmp.Compare(a.Destination, b.Destination) })
	return infos
}

// parseDockerTime returns zero for unset timestamps, which Docker reports as 0001-01-01T00:00:00Z.
func parseDockerTime(value string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Year() <= 1 {
		return 0
	}
	return parsed.UnixMilli()
}

func clampInt32(n int) int32 {
	return int32(min(max(n, math.MinInt32), math.MaxInt32)) //nolint:gosec // clamped to the int32 range
}
