package docker

import (
	"context"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

var listApplicationImageIDs = func(ctx context.Context, cli client.APIClient, appID string) (map[string]struct{}, error) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelApplicationID+"="+appID),
	})
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(result.Items))
	for _, item := range result.Items {
		if item.ImageID != "" {
			ids[item.ImageID] = struct{}{}
		}
	}
	return ids, nil
}

var removeImage = func(ctx context.Context, cli client.APIClient, imageID string) error {
	_, err := cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{PruneChildren: true})
	return err
}

// applicationImages snapshots the image IDs used by the application's
// containers so they can be compared after an update. Returns nil when cleanup
// is disabled or the snapshot fails, which makes removeReplacedImages a no-op.
func (c *Client) applicationImages(ctx context.Context, appID string, deleteOldImages bool) map[string]struct{} {
	if !deleteOldImages {
		return nil
	}
	ids, err := listApplicationImageIDs(ctx, c.cli.Client(), appID)
	if err != nil {
		c.log.Warn().Err(err).Str("application_id", appID).Msg("could not list application images, skipping old image cleanup")
		return nil
	}
	return ids
}

// removeReplacedImages removes images that the application's containers used
// before an update but no longer use afterwards. Images are referenced by ID,
// so this works regardless of whether the update changed a tag in the compose
// file or pulled a new digest for the same tag. Images still used by other
// containers are kept, since the daemon refuses to remove them without force.
func (c *Client) removeReplacedImages(ctx context.Context, appID string, previous map[string]struct{}) {
	if len(previous) == 0 {
		return
	}
	current, err := listApplicationImageIDs(ctx, c.cli.Client(), appID)
	if err != nil {
		c.log.Warn().Err(err).Str("application_id", appID).Msg("could not list application images, skipping old image cleanup")
		return
	}

	for imageID := range previous {
		if _, inUse := current[imageID]; inUse {
			continue
		}
		if err := removeImage(ctx, c.cli.Client(), imageID); err != nil {
			if cerrdefs.IsConflict(err) || cerrdefs.IsNotFound(err) {
				c.log.Debug().Err(err).Str("application_id", appID).Str("image_id", imageID).Msg("old image not removed")
				continue
			}
			c.log.Warn().Err(err).Str("application_id", appID).Str("image_id", imageID).Msg("could not remove old image")
			continue
		}
		c.log.Info().Str("application_id", appID).Str("image_id", imageID).Msg("removed old image")
	}
}
