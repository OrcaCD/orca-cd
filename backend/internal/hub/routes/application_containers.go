package routes

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var applicationContainersLog = logger.New("application-containers", false)

const containerRequestTimeout = 30 * time.Second

const agentContainerNotFound = "container not found"

// containerIDPattern matches the full ID or a prefix of at least 12 characters.
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

type portBindingResponse struct {
	Ip          string `json:"ip"`
	PublicPort  uint32 `json:"publicPort"`
	PrivatePort uint32 `json:"privatePort"`
	Protocol    string `json:"protocol"`
}

type containerNetworkResponse struct {
	Name      string   `json:"name"`
	IpAddress string   `json:"ipAddress"`
	Aliases   []string `json:"aliases"`
}

type containerMountResponse struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

type containerResponse struct {
	Id                  string                     `json:"id"`
	Name                string                     `json:"name"`
	Service             string                     `json:"service"`
	ContainerNumber     int32                      `json:"containerNumber"`
	State               string                     `json:"state"`
	Health              models.HealthStatus        `json:"health"`
	HealthRaw           string                     `json:"healthRaw"`
	HealthFailingStreak int32                      `json:"healthFailingStreak"`
	Image               string                     `json:"image"`
	ImageId             string                     `json:"imageId"`
	ImageDigest         string                     `json:"imageDigest"`
	RestartCount        int32                      `json:"restartCount"`
	RestartPolicy       string                     `json:"restartPolicy"`
	ExitCode            int32                      `json:"exitCode"`
	OomKilled           bool                       `json:"oomKilled"`
	Error               string                     `json:"error"`
	LogDriver           string                     `json:"logDriver"`
	CpuLimit            float64                    `json:"cpuLimit"`
	CreatedAt           *time.Time                 `json:"createdAt"`
	StartedAt           *time.Time                 `json:"startedAt"`
	FinishedAt          *time.Time                 `json:"finishedAt"`
	Ports               []portBindingResponse      `json:"ports"`
	Networks            []containerNetworkResponse `json:"networks"`
	Mounts              []containerMountResponse   `json:"mounts"`
}

func ListApplicationContainersHandler(c *gin.Context) {
	application, ok := loadContainerTarget(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), containerRequestTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.ListContainers(ctx, application.AgentId, application.Id)
	if err != nil {
		respondContainerTransportError(c, application, err)
		return
	}
	if !result.Success {
		c.JSON(http.StatusBadGateway, gin.H{"error": containerAgentError(result.ErrorMessage, "The agent failed to list the containers.")})
		return
	}

	containers := make([]containerResponse, 0, len(result.Containers))
	for _, container := range result.Containers {
		if container != nil {
			containers = append(containers, toContainerResponse(container))
		}
	}
	c.JSON(http.StatusOK, gin.H{"containers": containers})
}

func InspectApplicationContainerHandler(c *gin.Context) {
	containerID := c.Param("containerId")
	if !containerIDPattern.MatchString(containerID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid container id"})
		return
	}

	application, ok := loadContainerTarget(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), containerRequestTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.InspectContainer(ctx, application.AgentId, application.Id, containerID)
	if err != nil {
		respondContainerTransportError(c, application, err)
		return
	}
	if !result.Success {
		if result.ErrorMessage == agentContainerNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": containerAgentError(result.ErrorMessage, "The agent failed to inspect the container.")})
		return
	}

	c.JSON(http.StatusOK, gin.H{"inspectJson": result.InspectJson})
}

func loadContainerTarget(c *gin.Context) (models.Application, bool) {
	if websocket.DefaultHub == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hub not initialized"})
		return models.Application{}, false
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", c.Param("id")).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return application, false
	}

	if _, online := websocket.DefaultHub.GetClient(application.AgentId); !online {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "The agent is offline. Containers can only be monitored while it is connected."})
		return application, false
	}

	if !websocket.DefaultHub.HasCapability(application.AgentId, websocket.CapabilityContainerMonitoring) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "This agent does not support container monitoring. Please update it."})
		return application, false
	}

	return application, true
}

func respondContainerTransportError(c *gin.Context, application models.Application, err error) {
	switch {
	case errors.Is(err, websocket.ErrAgentOffline):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "The agent is offline. Containers can only be monitored while it is connected."})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "The agent did not respond in time. Please try again."})
	case errors.Is(err, context.Canceled):
		c.AbortWithStatus(499)
	default:
		applicationContainersLog.Error().Err(err).Str("agent_id", application.AgentId).Msg("container request failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
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
