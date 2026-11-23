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

	composeFile, ok := readDeploymentFile(filepath.Dir(composePath), filepath.Base(composePath))
	if !ok {
		return false, nil, fmt.Errorf("failed to safely read compose file at expected path %s", composePath)
	}
	if err := validateComposeFeatures(string(composeFile)); err != nil {
		return false, nil, err
	}

	project, err := loadProject(ctx, c.compose, composeProjectLoadOptions(projectName, composePath, filepath.Dir(composePath)))
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

	units := imageUnitsOf(project)
	var updates []*messages.ImageUpdate
	var digestErrs []error
	for _, unit := range units {
		if err := c.checkImageUnit(ctx, appID, policy, dockerCLI, unit); err != nil {
			digestErrs = append(digestErrs, err)
		}
		for _, use := range unit.uses {
			if !use.hook && use.update.Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UNSPECIFIED {
				updates = append(updates, use.update)
			}
		}
	}
	digestErr := errors.Join(digestErrs...)
	slices.SortFunc(updates, func(a, b *messages.ImageUpdate) int { return strings.Compare(a.Service, b.Service) })

	var stale []*messages.ImageUpdate
	var updatedUnits []*imageUnit
	for _, unit := range units {
		if !unit.updated {
			continue
		}
		updatedUnits = append(updatedUnits, unit)
		for _, use := range unit.uses {
			if !use.hook {
				stale = append(stale, use.update)
			}
		}
	}
	if len(updatedUnits) == 0 {
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

	pullTarget := pullTargetProject(project, units)
	if err := pullProject(ctx, c.compose, pullTarget, api.PullOptions{}); err != nil {
		return false, updates, errors.Join(failStale(fmt.Errorf("pull images: %w", err)), digestErr)
	}
	for _, unit := range updatedUnits {
		if unit.pinned == "" {
			continue
		}
		if err := tagImage(ctx, dockerCLI, unit.pinned, unit.ref); err != nil {
			return false, updates, errors.Join(failStale(fmt.Errorf("tag image %s: %w", unit.ref, err)), digestErr)
		}
	}

	// Everything was pulled above. compose up must not pull again, as the tags
	// may point to other images by now and pre_start hooks inherit the pull
	// policy of their service.
	for name, service := range project.Services {
		service.PullPolicy = composetypes.PullPolicyNever
		project.Services[name] = service
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

	c.log.Info().Str("application_name", appName).Int("images_updated", len(updatedUnits)).Msg("image pull completed")

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

// imageUse is one image a service runs: its own, or that of a pre_start hook.
type imageUse struct {
	// service has the image of the use as its image.
	service composetypes.ServiceConfig
	hook    bool
	update  *messages.ImageUpdate
}

// imageUnit is an image tag and every use of it. They are updated together,
// as pulling the tag for one of them changes the image of all of them.
type imageUnit struct {
	ref     string
	uses    []*imageUse
	updated bool
	// pinned is the reference the unit is pulled by if its release age was
	// checked, so a tag moved since the check can't change what is pulled.
	pinned string
}

// imageUnitsOf groups the images of the services and their pre_start hooks by
// tag, in the order of the service names.
func imageUnitsOf(project *composetypes.Project) []*imageUnit {
	var units []*imageUnit
	byTag := map[string]*imageUnit{}
	add := func(service composetypes.ServiceConfig, image string, hook bool) {
		key := tagKey(image)
		unit, ok := byTag[key]
		if !ok {
			unit = &imageUnit{ref: image}
			byTag[key] = unit
			units = append(units, unit)
		}
		service.Image = image
		unit.uses = append(unit.uses, &imageUse{
			service: service,
			hook:    hook,
			update:  &messages.ImageUpdate{Service: service.Name, Image: image},
		})
	}
	for _, name := range slices.Sorted(maps.Keys(project.Services)) {
		service := project.Services[name]
		if service.Image != "" {
			add(service, service.Image, false)
		}
		var hookImages []string
		for _, image := range api.GetDependentImages(service, project.Name) {
			if !slices.Contains(hookImages, image) {
				hookImages = append(hookImages, image)
				add(service, image, true)
			}
		}
	}
	return units
}

// checkImageUnit compares the image of unit with its registry and decides
// whether it is updated, recording the result in the update of every use.
func (c *Client) checkImageUnit(ctx context.Context, appID string, policy ReleaseAgePolicy, dockerCLI client.APIClient, unit *imageUnit) error {
	setAll := func(set func(update *messages.ImageUpdate)) {
		for _, use := range unit.uses {
			set(use.update)
		}
	}
	fail := func(err error) {
		setAll(func(update *messages.ImageUpdate) {
			update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED
			update.Reason = err.Error()
		})
	}

	remoteDigest, err := getRemoteDigest(ctx, c.cli, unit.ref)
	if err != nil {
		if isLocalOnlyImage(ctx, dockerCLI, unit.ref) {
			c.log.Debug().Err(err).Str("image", unit.ref).Msg("skipping digest check for local-only image")
			return nil
		}
		c.log.Error().Err(err).Str("image", unit.ref).Msg("failed to get remote digest for image")
		fail(err)
		return fmt.Errorf("image %s: %w", unit.ref, err)
	}

	localDigests, err := getLocalDigests(ctx, dockerCLI, unit.ref)
	if err != nil && !cerrdefs.IsNotFound(err) {
		// Only an image that is missing locally may be pulled unchecked.
		c.log.Error().Err(err).Str("image", unit.ref).Msg("failed to inspect local image")
		fail(err)
		return fmt.Errorf("image %s: inspect local image: %w", unit.ref, err)
	}
	missing := err != nil
	if !missing {
		current, version := localDigest(localDigests), getLocalVersion(ctx, dockerCLI, unit.ref)
		setAll(func(update *messages.ImageUpdate) {
			update.CurrentDigest = current
			update.CurrentVersion = version
		})
	}
	if !missing && digestMatchesLocal(localDigests, remoteDigest) {
		for _, use := range unit.uses {
			use.update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE
			use.update.CurrentDigest = remoteDigest
			// Shown even without an update, so the tab lists the age of every service.
			_, _ = policy.setMinimumAge(use.service, use.update)
		}
		return nil
	}

	setAll(func(update *messages.ImageUpdate) {
		update.NewDigest = remoteDigest
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED
	})
	// An image missing locally is deployed rather than updated, so its
	// release age isn't checked.
	if missing {
		unit.updated = true
		return nil
	}

	version := c.imageVersion(ctx, unit.ref, remoteDigest)
	checked := false
	for _, use := range unit.uses {
		use.update.NewVersion = version
		if c.evaluateUpdate(ctx, appID, policy, use.service, remoteDigest, use.update) {
			checked = true
		}
	}
	if holder := holdingUse(unit.uses); holder != nil {
		holdTogether(unit, holder)
		c.log.Info().Str("image", unit.ref).Str("reason", holder.update.Reason).Msg("image update held back")
		return nil
	}
	if checked {
		pinned, err := pinnedRef(unit.ref, remoteDigest)
		if err != nil {
			err = fmt.Errorf("pin image %s: %w", unit.ref, err)
			fail(err)
			return err
		}
		unit.pinned = pinned
	}
	unit.updated = true
	return nil
}

// holdingUse returns the use that holds back the update of its unit the
// longest, or nil if none does: an invalid configuration before the latest
// eligible time, before an image the webhook didn't announce.
func holdingUse(uses []*imageUse) *imageUse {
	rank := func(use *imageUse) int {
		switch use.update.Status {
		case messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED:
			return 3
		case messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING:
			return 2
		case messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED:
			return 1
		default:
			return 0
		}
	}
	var holder *imageUse
	for _, use := range uses {
		r := rank(use)
		if r == 0 {
			continue
		}
		if holder == nil || r > rank(holder) ||
			(r == rank(holder) && use.update.EligibleAt > holder.update.EligibleAt) {
			holder = use
		}
	}
	return holder
}

// holdTogether holds back the other uses of the unit as long as holder.
func holdTogether(unit *imageUnit, holder *imageUse) {
	name := "service " + holder.service.Name
	if holder.hook {
		name = "a pre_start hook of " + name
	}
	for _, use := range unit.uses {
		if use == holder || use.update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED {
			continue
		}
		// A use that waits at least as long keeps its own time.
		if use.update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING &&
			(holder.update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED ||
				use.update.EligibleAt >= holder.update.EligibleAt) {
			continue
		}
		use.update.Status = holder.update.Status
		use.update.PushedAt = holder.update.PushedAt
		use.update.FirstSeen = holder.update.FirstSeen
		use.update.EligibleAt = holder.update.EligibleAt
		use.update.Reason = fmt.Sprintf("%s uses the same image tag: %s", name, holder.update.Reason)
	}
}

// pullTargetProject returns a copy of project that pulls the images of the
// updated units, the checked ones by digest, and nothing else. The images of
// pre_start hooks are pulled as services of their own, as compose would pull
// them with the pull policy of the service they belong to.
func pullTargetProject(project *composetypes.Project, units []*imageUnit) *composetypes.Project {
	pull := *project
	pull.Services = make(composetypes.Services, len(project.Services))
	for name, service := range project.Services {
		service.PullPolicy = composetypes.PullPolicyNever
		service.PreStart = nil
		pull.Services[name] = service
	}

	hooks := 0
	for _, unit := range units {
		if !unit.updated {
			continue
		}
		pulled := false
		for _, use := range unit.uses {
			if use.hook {
				continue
			}
			service := pull.Services[use.service.Name]
			service.PullPolicy = use.service.PullPolicy
			if unit.pinned != "" {
				service.Image = unit.pinned
				service.PullPolicy = ""
			}
			pull.Services[use.service.Name] = service
			pulled = true
		}
		if pulled {
			continue
		}

		image := unit.ref
		// Like compose, a hook image is pulled with the pull policy of its
		// service, but can't be built.
		policy := unit.uses[0].service.PullPolicy
		if policy == composetypes.PullPolicyBuild {
			policy = composetypes.PullPolicyMissing
		}
		if unit.pinned != "" {
			image, policy = unit.pinned, ""
		}
		var name string
		for {
			name = fmt.Sprintf("orca-cd-pre-start-image-%d", hooks)
			hooks++
			if _, exists := pull.Services[name]; !exists {
				break
			}
		}
		pull.Services[name] = composetypes.ServiceConfig{Name: name, Image: image, PullPolicy: policy}
	}
	return &pull
}
