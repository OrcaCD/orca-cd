package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/pkg/authconfig"
	registrytypes "github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

// These functions are extracted to allow testing
var getRemoteDigest = func(ctx context.Context, dockerCli command.Cli, imageRef string) (string, error) {
	encodedAuth, credErr := command.RetrieveAuthTokenFromImage(dockerCli.ConfigFile(), imageRef)
	if credErr != nil {
		encodedAuth = ""
	}
	dist, err := dockerCli.Client().DistributionInspect(ctx, imageRef, client.DistributionInspectOptions{
		EncodedRegistryAuth: encodedAuth,
	})
	if err != nil {
		if isUnauthorized(err) {
			err = withCredentialsHint(err, imageRef, encodedAuth, credErr)
		}
		return "", err
	}
	return string(dist.Descriptor.Digest), nil
}

// unauthorizedMessages are fragments of registry errors that the daemon doesn't
// classify as unauthorized, e.g. a registry with basic auth answers an anonymous
// request with HTTP 500 "no basic auth credentials".
var unauthorizedMessages = []string{"unauthorized", "denied", "no basic auth credentials", "authentication required"}

func isUnauthorized(err error) bool {
	if cerrdefs.IsUnauthorized(err) || cerrdefs.IsPermissionDenied(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return slices.ContainsFunc(unauthorizedMessages, func(fragment string) bool {
		return strings.Contains(msg, fragment)
	})
}

// withCredentialsHint explains an unauthorized registry answer that was sent
// without credentials, which is otherwise indistinguishable from a bad login.
func withCredentialsHint(err error, imageRef, encodedAuth string, credErr error) error {
	registry := registryHost(imageRef)
	if credErr != nil {
		return fmt.Errorf("%w (reading credentials for %s from the agent's Docker config failed: %v)", err, registry, credErr)
	}
	if auth, decodeErr := authconfig.Decode(encodedAuth); decodeErr == nil && !hasCredentials(auth) {
		return fmt.Errorf("%w (no credentials for %s found in the agent's Docker config)", err, registry)
	}
	return err
}

func registryHost(imageRef string) string {
	host, _, found := strings.Cut(imageRef, "/")
	if found && (host == "localhost" || strings.ContainsAny(host, ".:")) {
		return host
	}
	return "docker.io"
}

func hasCredentials(auth *registrytypes.AuthConfig) bool {
	return auth.Username != "" || auth.Password != "" || auth.Auth != "" ||
		auth.IdentityToken != "" || auth.RegistryToken != ""
}

var getLocalDigests = func(ctx context.Context, cli client.APIClient, imageRef string) ([]string, error) {
	result, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, err
	}
	return result.RepoDigests, nil
}

// isLocalOnlyImage reports whether imageRef exists locally without having come
// from a registry (e.g. built locally), so there is nothing to compare it with.
// An image that is missing locally is not local-only: it should have been pulled.
var isLocalOnlyImage = func(ctx context.Context, cli client.APIClient, imageRef string) bool {
	result, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return false
	}

	if id := result.Identity; id != nil && (len(id.Pull) > 0 || len(id.Build) > 0) {
		return len(id.Pull) == 0
	}
	return len(result.RepoDigests) == 0
}

var pullProject = func(ctx context.Context, svc api.Compose, project *composetypes.Project, opts api.PullOptions) error {
	return svc.Pull(ctx, project, opts)
}

// digestMatchesLocal reports whether remoteDigest (a bare "sha256:…" string)
// appears in any of the "repo@sha256:…" entries from ImageInspect.RepoDigests.
func digestMatchesLocal(localDigests []string, remoteDigest string) bool {
	for _, d := range localDigests {
		if _, digest, ok := strings.CutLast(d, "@"); ok && digest == remoteDigest {
			return true
		}
	}
	return false
}

// ErrRedeployFailed reports that new images were pulled but recreating the
// containers with them failed.
var ErrRedeployFailed = errors.New("compose up after pull")

// tagImage is a package-level var so tests can override it.
var tagImage = func(ctx context.Context, cli client.APIClient, source, target string) error {
	_, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target})
	return err
}

// CheckAndPullImages checks whether any service image in the deployed compose
// project for appName is stale compared to its registry. If any are stale, it
// pulls them and redeploys. Returns true if at least one image was updated,
// and the update state of every service image.
func (c *Client) CheckAndPullImages(ctx context.Context, appID, appName string, deleteOldImages bool, policy ReleaseAgePolicy) (bool, []*messages.ImageUpdate, error) {

	projectName, err := utils.NormalizeProjectName(appName)
	if err != nil {
		return false, nil, fmt.Errorf("invalid application name: %w", err)
	}

	if err := c.ensureProjectManaged(ctx, projectName); err != nil {
		return false, nil, err
	}

	composePath := filepath.Join(c.deploymentsDir, projectName, composeFileName)
	if _, err := os.Stat(composePath); errors.Is(err, os.ErrNotExist) {
		return false, nil, fmt.Errorf("compose file not found at expected path %s: %w", composePath, err)
	}

	if err := c.resolveHostDeploymentsDir(ctx); err != nil {
		return false, nil, fmt.Errorf("resolve host deployments directory: %w", err)
	}

	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  filepath.Dir(composePath),
	})
	if err != nil {
		return false, nil, fmt.Errorf("load compose project: %w", err)
	}
	// The compose file on disk is writable by containers that mount their
	// deployment directory, so it is checked again before every redeploy.
	if err := c.enforceDeployPolicy(appID, project, filepath.Dir(composePath)); err != nil {
		return false, nil, err
	}
	if err := translateBindMountSources(project, c.deploymentsDir, c.hostDeploymentsBase()); err != nil {
		return false, nil, fmt.Errorf("translate bind mount sources: %w", err)
	}

	dockerCLI := c.cli.Client()

	var updates []*messages.ImageUpdate
	var stale []*messages.ImageUpdate
	var digestErrs []error
	// Services whose update waits, and the digests of those whose release age
	// was checked.
	held := map[string]struct{}{}
	pinned := map[string]string{}
	for name, service := range project.Services {
		if service.Image == "" {
			continue
		}

		remoteDigest, err := getRemoteDigest(ctx, c.cli, service.Image)
		if err != nil {
			if isLocalOnlyImage(ctx, dockerCLI, service.Image) {
				c.log.Debug().Err(err).Str("image", service.Image).Msg("skipping digest check for local-only image")
				continue
			}
			c.log.Error().Err(err).Str("image", service.Image).Msg("failed to get remote digest for image")
			digestErrs = append(digestErrs, fmt.Errorf("image %s: %w", service.Image, err))
			updates = append(updates, &messages.ImageUpdate{
				Service: name,
				Image:   service.Image,
				Status:  messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED,
				Reason:  err.Error(),
			})
			continue
		}

		update := &messages.ImageUpdate{Service: name, Image: service.Image}
		updates = append(updates, update)
		localDigests, err := getLocalDigests(ctx, dockerCLI, service.Image)
		if err == nil {
			update.CurrentDigest = localDigest(localDigests)
			update.CurrentVersion = getLocalVersion(ctx, dockerCLI, service.Image)
		}
		if err == nil && digestMatchesLocal(localDigests, remoteDigest) {
			update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE
			update.CurrentDigest = remoteDigest
			// Shown even without an update, so the tab lists the age of every service.
			if age, fromLabel, err := policy.minimumAge(service); err == nil {
				update.MinimumAgeSeconds = int64(age / time.Second)
				update.MinimumAgeFromLabel = fromLabel
			}
			continue
		}

		update.NewDigest = remoteDigest
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED
		// An image missing locally is deployed rather than updated, so its
		// release age isn't checked.
		if err == nil {
			update.NewVersion = c.imageVersion(ctx, service.Image, remoteDigest)
			checked := c.evaluateUpdate(ctx, appID, policy, service, remoteDigest, update)
			if isHeldBack(update) {
				c.log.Info().Str("image", service.Image).Str("reason", update.Reason).Msg("image update held back")
				held[name] = struct{}{}
				continue
			}
			if checked {
				pinned[name] = remoteDigest
			}
		}
		stale = append(stale, update)
	}
	digestErr := errors.Join(digestErrs...)
	slices.SortFunc(updates, func(a, b *messages.ImageUpdate) int { return strings.Compare(a.Service, b.Service) })

	if len(stale) == 0 {
		return false, updates, digestErr
	}
	// failStale marks the updates that were about to be applied as failed.
	failStale := func(err error) error {
		for _, update := range stale {
			update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED
			update.Reason = err.Error()
		}
		return err
	}

	// compose up must not pull held back or checked images by their tag,
	// which may point to another image by now, even with pull_policy: always.
	for name := range project.Services {
		_, isHeld := held[name]
		_, isPinned := pinned[name]
		if isHeld || isPinned {
			service := project.Services[name]
			service.PullPolicy = composetypes.PullPolicyNever
			project.Services[name] = service
		}
	}

	pullTarget, err := pinnedProject(project, pinned)
	if err != nil {
		return false, updates, errors.Join(failStale(err), digestErr)
	}
	if err := pullProject(ctx, c.compose, pullTarget, api.PullOptions{}); err != nil {
		return false, updates, errors.Join(failStale(fmt.Errorf("pull images: %w", err)), digestErr)
	}
	for name := range pinned {
		image := project.Services[name].Image
		if err := tagImage(ctx, dockerCLI, pullTarget.Services[name].Image, image); err != nil {
			return false, updates, errors.Join(failStale(fmt.Errorf("tag image %s: %w", image, err)), digestErr)
		}
	}

	applyOrcaLabels(project, appID)

	var previousImages map[string]struct{}
	if deleteOldImages {
		previousImages = c.applicationImages(ctx, appID)
	}

	// Like Deploy, don't block on healthchecks: health is observed after the
	// containers are recreated and reported separately.
	if err := upProject(ctx, c.compose, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        true,
			Recreate:             api.RecreateForce,
			RecreateDependencies: api.RecreateForce,
			SkipProviders:        !c.isPrivilegedApp(appID),
		},
	}); err != nil {
		return false, updates, failStale(fmt.Errorf("%w: %w", ErrRedeployFailed, err))
	}

	if deleteOldImages {
		c.removeReplacedImages(ctx, appID, previousImages)
	}

	c.log.Info().Str("application_name", appName).Int("images_updated", len(stale)).Msg("image pull completed")

	// The hub resets the application to unknown health when it processes the
	// pull result, so always report the settled state even if it didn't change.
	c.observeApplicationHealth(appID)

	return true, updates, digestErr
}

// localDigest returns the digest of the first "repo@sha256:..." entry.
func localDigest(repoDigests []string) string {
	for _, d := range repoDigests {
		if _, digest, ok := strings.CutLast(d, "@"); ok {
			return digest
		}
	}
	return ""
}

// pinnedProject returns a copy of project that pulls the pinned services by
// their digest, so a tag moved since the check can't change what is pulled.
// The tag is pointed at the pulled image afterwards.
func pinnedProject(project *composetypes.Project, pinned map[string]string) (*composetypes.Project, error) {
	if len(pinned) == 0 {
		return project, nil
	}
	pull := *project
	pull.Services = maps.Clone(project.Services)
	for name, digest := range pinned {
		service := pull.Services[name]
		ref, err := pinnedRef(service.Image, digest)
		if err != nil {
			return nil, fmt.Errorf("pin image %s: %w", service.Image, err)
		}
		service.Image = ref
		service.PullPolicy = ""
		pull.Services[name] = service
	}
	return &pull, nil
}
