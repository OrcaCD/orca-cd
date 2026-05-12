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

	applicationDir := filepath.Join(c.deploymentsDir, req.ApplicationName)
	composePath := filepath.Join(applicationDir, composeFileName)

	if err := utils.DoesNotLookLikeFilePath(composePath); err != nil {
		return fmt.Errorf("invalid compose file path: %w", err)
	}

	if err := os.MkdirAll(applicationDir, 0o750); err != nil {
		return fmt.Errorf("create deployment directory: %w", err)
	}
	if err := os.WriteFile(composePath, []byte(req.ComposeFile), 0o600); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}

	project, err := loadProject(ctx, c.compose, api.ProjectLoadOptions{
		ProjectName: strings.ToLower(req.ApplicationName),
		ConfigPaths: []string{composePath},
		WorkingDir:  applicationDir,
	})
	if err != nil {
		return fmt.Errorf("load compose project: %w", err)
	}

	// Add OrcaCD managed label to all services
	for _, service := range project.Services {
		if service.Labels == nil {
			service.Labels = make(map[string]string)
		}
		service.Labels["managed_by"] = "orca-cd"
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
