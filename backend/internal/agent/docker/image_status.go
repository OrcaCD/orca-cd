package docker

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

// ociVersionLabel is the standard OCI annotation images use to advertise
// their version, e.g. "1.27.3".
const ociVersionLabel = "org.opencontainers.image.version"

// ServiceImageStatus describes the image a compose service runs and, when
// requested, the digest the registry currently serves for its reference.
type ServiceImageStatus struct {
	Service         string
	Image           string
	CurrentDigest   string
	CurrentVersion  string
	LatestDigest    string
	UpdateAvailable bool
	Error           string
}

type localImage struct {
	repoDigests []string
	labels      map[string]string
}

// These functions are extracted to allow testing
var listServiceImageIDs = func(ctx context.Context, cli client.APIClient, appID string) (map[string]string, error) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID+"="+appID),
	})
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(result.Items))
	for _, item := range result.Items {
		if service := item.Labels[api.ServiceLabel]; service != "" && item.ImageID != "" {
			ids[service] = item.ImageID
		}
	}
	return ids, nil
}

var inspectLocalImage = func(ctx context.Context, cli client.APIClient, imageRef string) (localImage, error) {
	result, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return localImage{}, err
	}
	img := localImage{repoDigests: result.RepoDigests}
	if result.Config != nil {
		img.labels = result.Config.Labels
	}
	return img, nil
}

// ImageStatus reports the image version every service of the deployed compose
// project runs. With checkLatest, it also asks the registry which digest a pull
// would fetch, so callers can show what an image update would change.
func (c *Client) ImageStatus(ctx context.Context, appID, appName string, checkLatest bool) ([]ServiceImageStatus, error) {
	project, err := c.loadDeployedProject(ctx, appName)
	if err != nil {
		return nil, err
	}

	dockerCLI := c.cli.Client()
	serviceImageIDs, err := listServiceImageIDs(ctx, dockerCLI, appID)
	if err != nil {
		return nil, fmt.Errorf("list application containers: %w", err)
	}

	statuses := make([]ServiceImageStatus, 0, len(project.Services))
	for _, name := range slices.Sorted(maps.Keys(project.Services)) {
		service := project.Services[name]
		if service.Image == "" {
			continue
		}
		status := ServiceImageStatus{Service: name, Image: service.Image}

		// Prefer the image the running container was created from: the tag may
		// already point to a newer image that has not been deployed yet.
		imageRef := service.Image
		if id, ok := serviceImageIDs[name]; ok {
			imageRef = id
		}
		local, err := inspectLocalImage(ctx, dockerCLI, imageRef)
		if err != nil {
			c.log.Debug().Err(err).Str("image", service.Image).Msg("image status: failed to inspect local image")
		} else {
			status.CurrentDigest = repoDigest(local.repoDigests, service.Image)
			status.CurrentVersion = local.labels[ociVersionLabel]
		}

		if checkLatest {
			latest, err := getRemoteDigest(ctx, c.cli, service.Image)
			if err != nil {
				c.log.Warn().Err(err).Str("image", service.Image).Msg("image status: failed to get remote digest")
				status.Error = err.Error()
			} else {
				status.LatestDigest = latest
				status.UpdateAvailable = !digestMatchesLocal(local.repoDigests, latest)
			}
		}

		statuses = append(statuses, status)
	}
	return statuses, nil
}

// repoDigest returns the bare "sha256:…" digest from repoDigests that belongs
// to the repository of imageRef, falling back to the first entry when an image
// was pulled under several repository names.
func repoDigest(repoDigests []string, imageRef string) string {
	repo := imageRepository(imageRef)
	fallback := ""
	for _, d := range repoDigests {
		name, digest, ok := strings.Cut(d, "@")
		if !ok {
			continue
		}
		if name == repo {
			return digest
		}
		if fallback == "" {
			fallback = digest
		}
	}
	return fallback
}

// imageRepository strips the tag and digest from an image reference. A colon
// only starts a tag after the last slash, since registries may carry a port.
func imageRepository(imageRef string) string {
	ref, _, _ := strings.Cut(imageRef, "@")
	lastSlash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > lastSlash {
		ref = ref[:i]
	}
	return ref
}
