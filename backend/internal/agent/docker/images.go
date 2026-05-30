package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

// These functions are extracted to allow testing
var getRemoteDigest = func(ctx context.Context, dockerCli command.Cli, imageRef string) (string, error) {
	encodedAuth, err := command.RetrieveAuthTokenFromImage(dockerCli.ConfigFile(), imageRef)
	if err != nil {
		// No stored credentials — proceed anonymously (public images will still work).
		encodedAuth = ""
	}
	dist, err := dockerCli.Client().DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{
		EncodedRegistryAuth: encodedAuth,
	})
	if err != nil {
		return "", err
	}
	return string(dist.Descriptor.Digest), nil
}

var getLocalDigests = func(ctx context.Context, cli client.APIClient, imageRef string) ([]string, error) {
	result, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	return result.RepoDigests, nil
}

var pullProject = func(ctx context.Context, svc api.Compose, project *composetypes.Project, opts api.PullOptions) error {
	return svc.Pull(ctx, project, opts)
}

// digestMatchesLocal reports whether remoteDigest (a bare "sha256:…" string)
// appears in any of the "repo@sha256:…" entries from ImageInspect.RepoDigests.
func digestMatchesLocal(localDigests []string, remoteDigest string) bool {
	for _, d := range localDigests {
		if idx := strings.LastIndex(d, "@"); idx >= 0 {
			if d[idx+1:] == remoteDigest {
				return true
			}
		}
	}
	return false
}

// CheckAndPullImages checks whether any service image in the deployed compose
// project for appName is stale compared to its registry. If any are stale, it
// pulls all images and redeploys. Returns true if at least one image
// was updated.
func (c *Client) CheckAndPullImages(ctx context.Context, appName string, deleteOldImages bool) (bool, error) {

	projectName, err := normalizeProjectName(appName)
	if err != nil {
		return false, fmt.Errorf("invalid application name: %w", err)
	}

	composePath := filepath.Join(c.deploymentsDir, projectName, composeFileName)
	if _, err := os.Stat(composePath); errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("compose file not found at expected path %s: %w", composePath, err)
	}

	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  filepath.Dir(composePath),
	})
	if err != nil {
		return false, fmt.Errorf("load compose project: %w", err)
	}

	dockerCLI := c.cli.Client()

	type staleImage struct {
		ref       string
		oldDigest string // may be empty for first-pull
	}

	var stale []staleImage
	for _, service := range project.Services {
		if service.Image == "" {
			continue
		}

		remoteDigest, err := getRemoteDigest(ctx, c.cli, service.Image)
		if err != nil {
			c.log.Error().Err(err).Str("image", service.Image).Msg("failed to get remote digest for image")
			continue
		}

		localDigests, err := getLocalDigests(ctx, dockerCLI, service.Image)
		if err != nil {
			// Image not present locally
			stale = append(stale, staleImage{ref: service.Image})
			continue
		}

		if !digestMatchesLocal(localDigests, remoteDigest) {
			var oldDigest string
			for _, d := range localDigests {
				if idx := strings.LastIndex(d, "@"); idx >= 0 {
					oldDigest = d[idx+1:]
					break
				}
			}
			stale = append(stale, staleImage{ref: service.Image, oldDigest: oldDigest})
		}
	}

	if len(stale) == 0 {
		return false, nil
	}

	if err := pullProject(ctx, c.compose, project, api.PullOptions{}); err != nil {
		return false, fmt.Errorf("pull images: %w", err)
	}

	if err := upProject(ctx, c.compose, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        true,
			Recreate:             api.RecreateForce,
			RecreateDependencies: api.RecreateForce,
		},
		Start: api.StartOptions{
			Wait:        true,
			WaitTimeout: deployWaitTimeout,
		},
	}); err != nil {
		return false, fmt.Errorf("compose up after pull: %w", err)
	}

	if deleteOldImages {
		for _, img := range stale {
			if img.oldDigest == "" {
				continue
			}
			if _, err := dockerCLI.ImageRemove(ctx, img.oldDigest, client.ImageRemoveOptions{PruneChildren: true}); err != nil {
				c.log.Warn().Err(err).Str("digest", img.oldDigest).Msg("could not remove old image")
			}
		}
	}

	c.log.Info().Str("application_name", appName).Int("images_updated", len(stale)).Msg("image pull completed")
	return true, nil
}
