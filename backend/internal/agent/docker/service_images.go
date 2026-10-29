package docker

import (
	"cmp"
	"context"
	"slices"
	"strings"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var listApplicationContainers = func(ctx context.Context, cli client.APIClient, appID string) ([]container.Summary, error) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID+"="+appID),
	})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ServiceImages reports the image each compose service of the application
// runs, so the hub can later roll back to exactly these images. Images from a
// registry are pinned by digest, which keeps them pullable even after the
// local image was removed.
func (c *Client) ServiceImages(ctx context.Context, appID string) ([]*messages.ServiceImage, error) {
	containers, err := listApplicationContainers(ctx, c.cli.Client(), appID)
	if err != nil {
		return nil, err
	}

	// Running containers show what the service runs now. Stopped ones of an
	// older configuration or `compose run` containers may still be around.
	slices.SortStableFunc(containers, func(a, b container.Summary) int {
		return cmp.Compare(runningRank(a), runningRank(b))
	})
	byService := make(map[string]*messages.ServiceImage)
	for _, item := range containers {
		service := item.Labels[api.ServiceLabel]
		if service == "" || item.ImageID == "" || item.Labels[api.OneoffLabel] == "True" {
			continue
		}
		if _, seen := byService[service]; seen {
			continue
		}
		image := &messages.ServiceImage{Service: service, ImageId: item.ImageID}
		if digests, err := getLocalDigests(ctx, c.cli.Client(), item.ImageID); err == nil {
			image.PinnedRef = pinnedReference(item.Image, digests)
		} else {
			c.log.Debug().Err(err).Str("application_id", appID).Str("image_id", item.ImageID).Msg("could not inspect service image")
		}
		byService[service] = image
	}

	images := make([]*messages.ServiceImage, 0, len(byService))
	for _, image := range byService {
		images = append(images, image)
	}
	slices.SortFunc(images, func(a, b *messages.ServiceImage) int { return cmp.Compare(a.Service, b.Service) })
	return images, nil
}

func runningRank(item container.Summary) int {
	if item.State == container.StateRunning {
		return 0
	}
	return 1
}

// pinnedReference picks the repo digest of the repository imageRef names. An
// image can carry digests of several repositories when it was tagged for
// more than one. Containers created from an image ID name no repository, so
// only an unambiguous digest is used for them.
func pinnedReference(imageRef string, repoDigests []string) string {
	if strings.HasPrefix(imageRef, "sha256:") {
		if len(repoDigests) == 1 {
			return repoDigests[0]
		}
		return ""
	}
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return ""
	}
	for _, digest := range repoDigests {
		digestNamed, err := reference.ParseNormalizedNamed(digest)
		if err == nil && digestNamed.Name() == named.Name() {
			return digest
		}
	}
	return ""
}

// pinServiceImages replaces the image of every pinned service. Services that
// are built locally keep their image, since compose would build them instead.
func pinServiceImages(project *composetypes.Project, pins map[string]string) {
	for name, service := range project.Services {
		ref := pins[name]
		if ref == "" || service.Build != nil {
			continue
		}
		service.Image = ref
		project.Services[name] = service
	}
}
