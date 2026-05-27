package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
)

const (
	composeFileName   = "compose.yaml"
	deployWaitTimeout = 2 * time.Minute
)

type DeployRequest struct {
	ApplicationID   string
	ApplicationName string
	ComposeFile     string
}

var loadProject = func(ctx context.Context, composeService api.Compose, options api.ProjectLoadOptions) (*composetypes.Project, error) {
	return composeService.LoadProject(ctx, options)
}

var upProject = func(ctx context.Context, composeService api.Compose, project *composetypes.Project, options api.UpOptions) error {
	return composeService.Up(ctx, project, options)
}

// converts an application name to a valid Docker Compose project name
func normalizeProjectName(name string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	// Docker project names must start with a letter or digit, not an underscore.
	result = strings.TrimLeft(result, "_")
	if result == "" {
		return "", fmt.Errorf("application name %q cannot be normalized to a valid project name", name)
	}
	return result, nil
}

func (c *Client) Deploy(ctx context.Context, req DeployRequest) error {
	if c.compose == nil {
		return errors.New("docker compose service is not initialized")
	}
	if c.deploymentsDir == "" {
		return errors.New("deployments directory is not configured")
	}
	if !c.Ready() {
		return errors.New("docker daemon is not ready")
	}

	if req.ComposeFile == "" {
		return errors.New("compose file is empty")
	}

	// Validate application name to prevent path traversal
	if err := utils.DoesNotLookLikeFilePath(req.ApplicationName); err != nil {
		return fmt.Errorf("invalid application name: %w", err)
	}

	projectName, err := normalizeProjectName(req.ApplicationName)
	if err != nil {
		return err
	}

	applicationDir := filepath.Join(c.deploymentsDir, projectName)
	composePath := filepath.Join(applicationDir, composeFileName)

	// Verify the final path stays within the deployments directory
	if err := utils.IsPathWithinBase(c.deploymentsDir, composePath); err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	if err := os.MkdirAll(applicationDir, 0o750); err != nil {
		return fmt.Errorf("create deployment directory: %w", err)
	}
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0o600); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}

	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  applicationDir,
	})
	if err != nil {
		return fmt.Errorf("load compose project: %w", err)
	}

	// Add OrcaCD managed label to all services
	for name, service := range project.Services {
		if service.Labels == nil {
			service.Labels = make(composetypes.Labels)
		}
		service.Labels["managed_by"] = "orca-cd"
		service.Labels["orca-cd.application-id"] = req.ApplicationID
		project.Services[name] = service
	}

	if err := upProject(ctx, c.compose, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        true,
			Recreate:             api.RecreateDiverged,
			RecreateDependencies: api.RecreateDiverged,
		},
		Start: api.StartOptions{
			Wait:        true,
			WaitTimeout: deployWaitTimeout,
		},
	}); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	c.log.Info().
		Str("application_id", req.ApplicationID).
		Str("application_name", req.ApplicationName).
		Str("compose_path", composePath).
		Msg("deployment completed")

	return nil
}
