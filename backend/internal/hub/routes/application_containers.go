package routes

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

var applicationContainersLog = logger.New("application-containers", false)

const containerRequestTimeout = 30 * time.Second

const agentContainerNotFound = "container not found"

// containerIDPattern matches the full ID or a prefix of at least 12 characters.
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

type portBindingResponse struct {
	Ip          string `json:"ip" required:"true"`
	PublicPort  uint32 `json:"publicPort" required:"true" doc:"0 when the port is not published"`
	PrivatePort uint32 `json:"privatePort" required:"true"`
	Protocol    string `json:"protocol" required:"true"`
}

type containerNetworkResponse struct {
	Name      string   `json:"name" required:"true"`
	IpAddress string   `json:"ipAddress" required:"true"`
	Aliases   []string `json:"aliases" required:"true"`
}

type containerMountResponse struct {
	Type        string `json:"type" required:"true"`
	Name        string `json:"name" required:"true"`
	Destination string `json:"destination" required:"true"`
	ReadOnly    bool   `json:"readOnly" required:"true"`
}

type containerResponse struct {
	Id                  string                     `json:"id" required:"true"`
	Name                string                     `json:"name" required:"true"`
	Service             string                     `json:"service" required:"true"`
	ContainerNumber     int32                      `json:"containerNumber" required:"true" doc:"The compose replica number, 0 when unknown"`
	State               string                     `json:"state" required:"true" doc:"The Docker state, like running or exited"`
	Health              models.HealthStatus        `json:"health" required:"true"`
	HealthRaw           string                     `json:"healthRaw" required:"true"`
	HealthFailingStreak int32                      `json:"healthFailingStreak" required:"true"`
	Image               string                     `json:"image" required:"true"`
	ImageId             string                     `json:"imageId" required:"true"`
	ImageDigest         string                     `json:"imageDigest" required:"true" doc:"Empty for images that did not come from a registry"`
	RestartCount        int32                      `json:"restartCount" required:"true"`
	RestartPolicy       string                     `json:"restartPolicy" required:"true"`
	ExitCode            int32                      `json:"exitCode" required:"true"`
	OomKilled           bool                       `json:"oomKilled" required:"true"`
	Error               string                     `json:"error" required:"true" doc:"Why the container could not start"`
	LogDriver           string                     `json:"logDriver" required:"true"`
	CpuLimit            float64                    `json:"cpuLimit" required:"true" doc:"The CPUs the container may use, 0 without a limit"`
	CreatedAt           *time.Time                 `json:"createdAt" required:"true" nullable:"true"`
	StartedAt           *time.Time                 `json:"startedAt" required:"true" nullable:"true"`
	FinishedAt          *time.Time                 `json:"finishedAt" required:"true" nullable:"true"`
	Ports               []portBindingResponse      `json:"ports" required:"true"`
	Networks            []containerNetworkResponse `json:"networks" required:"true"`
	Mounts              []containerMountResponse   `json:"mounts" required:"true"`
}

type containerListResponse struct {
	Containers []containerResponse `json:"containers" required:"true"`
}

type containerListOutput struct {
	Body containerListResponse
}

type containerInspectResponse struct {
	InspectJson string `json:"inspectJson" required:"true" doc:"The output of docker inspect as JSON, without environment variables"`
}

type containerInspectOutput struct {
	Body containerInspectResponse
}

const containerServerErrorsDoc = "Answers 422 when the agent is too old for container monitoring, 503 while it is offline, " +
	"504 when it doesn't respond in time and 502 when it fails. "

var ListApplicationContainersHandler = restapi.Handler(huma.Operation{
	OperationID: "list-application-containers",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/containers",
	Summary:     "List the containers of an application",
	Description: "Loads the containers from the agent of the application. " +
		containerServerErrorsDoc + requires(models.ResourceApplication, models.ActionRead),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusForbidden, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
}, func(ctx context.Context, input *idInput) (*containerListOutput, error) {
	application, err := loadContainerTarget(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, containerRequestTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.ListContainers(ctx, application.AgentId, application.Id)
	if err != nil {
		return nil, containerTransportError(application, err)
	}
	if !result.Success {
		return nil, huma.Error502BadGateway(containerAgentError(result.ErrorMessage, "The agent failed to list the containers."))
	}

	response := containerListResponse{Containers: make([]containerResponse, 0, len(result.Containers))}
	for _, container := range result.Containers {
		if container != nil {
			response.Containers = append(response.Containers, toContainerResponse(container))
		}
	}
	return &containerListOutput{Body: response}, nil
})

var InspectApplicationContainerHandler = restapi.Handler(huma.Operation{
	OperationID: "inspect-application-container",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/containers/{containerId}/inspect",
	Summary:     "Inspect a container of an application",
	Description: "Loads the inspect output of the container from the agent, which removes the environment variables. " +
		containerServerErrorsDoc + requires(models.ResourceApplication, models.ActionInspect),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusForbidden,
		http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout},
}, func(ctx context.Context, input *struct {
	ID          string `path:"id" doc:"The ID of the application"`
	ContainerID string `path:"containerId" doc:"The ID of the container, or at least its first 12 characters"`
}) (*containerInspectOutput, error) {
	if !containerIDPattern.MatchString(input.ContainerID) {
		return nil, huma.Error400BadRequest("invalid container id")
	}

	application, err := loadContainerTarget(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, containerRequestTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.InspectContainer(ctx, application.AgentId, application.Id, input.ContainerID)
	if err != nil {
		return nil, containerTransportError(application, err)
	}
	if !result.Success {
		if result.ErrorMessage == agentContainerNotFound {
			return nil, huma.Error404NotFound("container not found")
		}
		return nil, huma.Error502BadGateway(containerAgentError(result.ErrorMessage, "The agent failed to inspect the container."))
	}

	return &containerInspectOutput{Body: containerInspectResponse{InspectJson: result.InspectJson}}, nil
})

const containerAgentOfflineMessage = "The agent is offline. Containers can only be monitored while it is connected."

func loadContainerTarget(ctx context.Context, id string) (models.Application, error) {
	var application models.Application

	if websocket.DefaultHub == nil {
		return application, huma.Error500InternalServerError("hub not initialized")
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return application, huma.Error404NotFound("application not found")
		}
		return application, internalError()
	}

	if _, online := websocket.DefaultHub.GetClient(application.AgentId); !online {
		return application, huma.Error503ServiceUnavailable(containerAgentOfflineMessage)
	}

	if !websocket.DefaultHub.HasCapability(application.AgentId, websocket.CapabilityContainerMonitoring) {
		return application, huma.Error422UnprocessableEntity("This agent does not support container monitoring. Please update it.")
	}

	return application, nil
}

func containerTransportError(application models.Application, err error) error {
	switch {
	case errors.Is(err, websocket.ErrAgentOffline):
		return huma.Error503ServiceUnavailable(containerAgentOfflineMessage)
	case errors.Is(err, context.DeadlineExceeded):
		return huma.Error504GatewayTimeout("The agent did not respond in time. Please try again.")
	case errors.Is(err, context.Canceled):
		return huma.NewError(499, "request canceled")
	default:
		applicationContainersLog.Error().Err(err).Str("agent_id", application.AgentId).Msg("container request failed")
		return internalError()
	}
}

func containerAgentError(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return utils.CapitalizeFirst(message)
}

func toContainerResponse(container *messages.ContainerInfo) containerResponse {
	response := containerResponse{
		Id:                  container.Id,
		Name:                container.Name,
		Service:             container.Service,
		ContainerNumber:     container.ContainerNumber,
		State:               container.State,
		Health:              containerHealth(container.Health),
		HealthRaw:           container.HealthRaw,
		HealthFailingStreak: container.HealthFailingStreak,
		Image:               container.Image,
		ImageId:             container.ImageId,
		ImageDigest:         container.ImageDigest,
		RestartCount:        container.RestartCount,
		RestartPolicy:       container.RestartPolicy,
		ExitCode:            container.ExitCode,
		OomKilled:           container.OomKilled,
		Error:               container.Error,
		LogDriver:           container.LogDriver,
		CpuLimit:            container.CpuLimit,
		CreatedAt:           millisToTime(container.CreatedAtMs),
		StartedAt:           millisToTime(container.StartedAtMs),
		FinishedAt:          millisToTime(container.FinishedAtMs),
		Ports:               make([]portBindingResponse, 0, len(container.Ports)),
		Networks:            make([]containerNetworkResponse, 0, len(container.Networks)),
		Mounts:              make([]containerMountResponse, 0, len(container.Mounts)),
	}
	for _, port := range container.Ports {
		if port != nil {
			response.Ports = append(response.Ports, portBindingResponse{
				Ip:          port.Ip,
				PublicPort:  port.PublicPort,
				PrivatePort: port.PrivatePort,
				Protocol:    port.Protocol,
			})
		}
	}
	for _, network := range container.Networks {
		if network != nil {
			aliases := network.Aliases
			if aliases == nil {
				aliases = []string{}
			}
			response.Networks = append(response.Networks, containerNetworkResponse{
				Name:      network.Name,
				IpAddress: network.IpAddress,
				Aliases:   aliases,
			})
		}
	}
	for _, mount := range container.Mounts {
		if mount != nil {
			response.Mounts = append(response.Mounts, containerMountResponse{
				Type:        mount.Type,
				Name:        mount.Name,
				Destination: mount.Destination,
				ReadOnly:    mount.ReadOnly,
			})
		}
	}
	return response
}

func containerHealth(health messages.HealthStatus) models.HealthStatus {
	switch health {
	case messages.HealthStatus_HEALTH_STATUS_HEALTHY:
		return models.Healthy
	case messages.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return models.Unhealthy
	case messages.HealthStatus_HEALTH_STATUS_STOPPED:
		return models.Stopped
	default:
		return models.UnknownHealth
	}
}

// millisToTime returns nil for the zero value the agent sends for unset times.
func millisToTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}
