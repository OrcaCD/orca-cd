package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	"github.com/compose-spec/compose-go/v2/cli"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	composeFileName    = "compose.yaml"
	labelManagedBy     = "managed_by"
	labelApplicationID = "orca-cd.application-id"
	managedByValue     = "orca-cd"
)

func orcaLabels(appID string) map[string]string {
	return map[string]string{
		labelManagedBy:     managedByValue,
		labelApplicationID: appID,
	}
}

func applyOrcaLabels(project *composetypes.Project, appID string) {
	for name, service := range project.Services {
		if service.Labels == nil {
			service.Labels = make(composetypes.Labels)
		}
		maps.Copy(service.Labels, orcaLabels(appID))
		project.Services[name] = service
	}
}

type DeployRequest struct {
	ApplicationID   string
	ApplicationName string
	ComposeFile     string
	DeleteOldImages bool
}

var loadProject = func(ctx context.Context, composeService api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
	return composeService.LoadProject(ctx, options)
}

var upProject = func(ctx context.Context, composeService api.Compose, project *composetypes.Project, options api.UpOptions) error {
	return composeService.Up(ctx, project, options)
}

var downProject = func(ctx context.Context, composeService api.Compose, projectName string, options api.DownOptions) error {
	return composeService.Down(ctx, projectName, options)
}

var listProjectContainers = func(ctx context.Context, cli client.APIClient, projectName string) ([]container.Summary, error) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", api.ProjectLabel+"="+projectName),
	})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// ensureProjectManaged refuses to touch a compose project that already exists on
// the host but was not created by OrcaCD, so an application named like a foreign
// project can neither take it over nor tear it down.
func (c *Client) ensureProjectManaged(ctx context.Context, projectName string) error {
	containers, err := listProjectContainers(ctx, c.cli.Client(), projectName)
	if err != nil {
		return fmt.Errorf("list containers of project %q: %w", projectName, err)
	}
	for _, item := range containers {
		if item.Labels[labelManagedBy] != managedByValue {
			return fmt.Errorf("compose project %q already exists and is not managed by OrcaCD", projectName)
		}
	}
	return nil
}

type DeleteRequest struct {
	ApplicationID   string
	ApplicationName string
}

// converts an application name to a valid Docker Compose project name
func (c *Client) Deploy(ctx context.Context, req DeployRequest) error {
	defer c.observeApplicationHealth(req.ApplicationID)

	if c.compose == nil {
		return errors.New("docker compose service is not initialized")
	}
	if c.deploymentsDir == "" {
		return errors.New("deployments directory is not configured")
	}
	if !c.Ready() {
		return errors.New("docker daemon is not ready")
	}
	if err := c.resolveHostDeploymentsDir(ctx); err != nil {
		return fmt.Errorf("resolve host deployments directory: %w", err)
	}

	if req.ComposeFile == "" {
		return errors.New("compose file is empty")
	}

	// Validate application name to prevent path traversal
	if err := utils.DoesNotLookLikeFilePath(req.ApplicationName); err != nil {
		return fmt.Errorf("invalid application name: %w", err)
	}

	projectName, err := utils.NormalizeProjectName(req.ApplicationName)
	if err != nil {
		return err
	}

	applicationDir := filepath.Join(c.deploymentsDir, projectName)
	composePath := filepath.Join(applicationDir, composeFileName)

	// Verify the final path stays within the deployments directory
	if err := utils.IsPathWithinBase(c.deploymentsDir, composePath); err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	if err := c.ensureProjectManaged(ctx, projectName); err != nil {
		return err
	}

	if err := ensureDeploymentDir(applicationDir); err != nil {
		return fmt.Errorf("create deployment directory: %w", err)
	}
	previousComposeFile, hadPreviousComposeFile := readDeploymentFile(applicationDir, composeFileName)
	if err := writeFileAtomic(composePath, []byte(req.ComposeFile)); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}

	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName:       projectName,
		ConfigPaths:       []string{composePath},
		WorkingDir:        applicationDir,
		ProjectOptionsFns: []cli.ProjectOptionsFn{cli.WithoutEnvironmentResolution},
	})
	if err != nil {
		err = fmt.Errorf("load compose project: %w", err)
	} else if err = c.enforceDeployPolicy(req.ApplicationID, project, applicationDir); err == nil {
		project, err = resolveEnvFiles(applicationDir, project)
	}
	if err != nil {
		// Image pulls redeploy the compose file on disk, so a rejected file must
		// not stay there.
		c.restoreComposeFile(composePath, previousComposeFile, hadPreviousComposeFile)
		return err
	}

	if err := translateBindMountSources(project, c.deploymentsDir, c.hostDeploymentsBase()); err != nil {
		return fmt.Errorf("translate bind mount sources: %w", err)
	}

	applyOrcaLabels(project, req.ApplicationID)
	if err := writeComposeOverride(applicationDir, orcaOverride(project, req.ApplicationID)); err != nil {
		return err
	}

	var previousImages map[string]struct{}
	if req.DeleteOldImages {
		previousImages = c.applicationImages(ctx, req.ApplicationID)
	}

	// Do not wait for healthchecks here: deployment is considered complete once
	// the containers are started. Runtime health is observed afterwards via
	// daemon events (see healthWatcher) and reported separately, so a slow or
	// failing healthcheck doesn't hold the application in a deploying state.
	if err := upProject(ctx, c.compose, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        true,
			Recreate:             api.RecreateDiverged,
			RecreateDependencies: api.RecreateDiverged,
			// Second line of defense next to the policy, which rejects provider services.
			SkipProviders: !c.isPrivilegedApp(req.ApplicationID),
		},
	}); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	if req.DeleteOldImages {
		c.removeReplacedImages(ctx, req.ApplicationID, previousImages)
	}

	c.log.Info().
		Str("application_id", req.ApplicationID).
		Str("application_name", req.ApplicationName).
		Str("compose_path", composePath).
		Msg("deployment completed")

	return nil
}

// enforceDeployPolicy runs the deployment security policy unless the
// application is allowlisted. It runs on agent-side paths, before bind mounts
// are translated to host paths, so mounts and env_file are checked against the
// same directory.
func (c *Client) enforceDeployPolicy(appID string, project *composetypes.Project, applicationDir string) error {
	if c.isPrivilegedApp(appID) {
		return nil
	}
	rules := pathRules{
		deploymentsDir:     c.deploymentsDir,
		applicationDir:     applicationDir,
		hostDeploymentsDir: c.hostDeploymentsBase(),
	}
	if c.restrictMountsToDeployDir {
		rules.restrictMountsDir = c.deploymentsDir
	}
	if err := checkDeployPolicy(project, rules); err != nil {
		return fmt.Errorf("deployment rejected by security policy: %w (add application id %q to ALLOWED_PRIVILEGED_APPS to bypass all security policy checks)", err, appID)
	}
	return nil
}

// isPrivilegedApp reports whether appID is listed in ALLOWED_PRIVILEGED_APPS
// and bypasses the security policy.
func (c *Client) isPrivilegedApp(appID string) bool {
	_, allowed := c.allowedPrivilegedApps[appID]
	return allowed
}

// restoreComposeFile puts back the compose file of the last deployment, or
// removes the file if there was none.
func (c *Client) restoreComposeFile(composePath string, previous []byte, hadPrevious bool) {
	var err error
	if hadPrevious {
		err = writeFileAtomic(composePath, previous)
	} else if err = os.Remove(composePath); errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		c.log.Error().Err(err).Str("compose_path", composePath).Msg("failed to restore previous compose file")
	}
}

// Remove tears down an application: compose down (containers + networks) then
// delete its deployment directory.
func (c *Client) Remove(ctx context.Context, req DeleteRequest) error {
	if c.compose == nil {
		return errors.New("docker compose service is not initialized")
	}
	if c.deploymentsDir == "" {
		return errors.New("deployments directory is not configured")
	}
	if !c.Ready() {
		return errors.New("docker daemon is not ready")
	}

	if err := utils.DoesNotLookLikeFilePath(req.ApplicationName); err != nil {
		return fmt.Errorf("invalid application name: %w", err)
	}

	projectName, err := utils.NormalizeProjectName(req.ApplicationName)
	if err != nil {
		return err
	}

	applicationDir := filepath.Join(c.deploymentsDir, projectName)
	if err := utils.IsPathWithinBase(c.deploymentsDir, applicationDir); err != nil {
		return fmt.Errorf("invalid deployment directory: %w", err)
	}

	if err := c.ensureProjectManaged(ctx, projectName); err != nil {
		return err
	}

	if err := downProject(ctx, c.compose, projectName, api.DownOptions{RemoveOrphans: true}); err != nil {
		return fmt.Errorf("compose down: %w", err)
	}

	if err := os.RemoveAll(applicationDir); err != nil {
		return fmt.Errorf("remove deployment directory: %w", err)
	}

	c.log.Info().
		Str("application_id", req.ApplicationID).
		Str("application_name", req.ApplicationName).
		Msg("removal completed")

	c.healthWatcher.forget(req.ApplicationID)

	return nil
}
