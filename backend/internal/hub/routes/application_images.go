package routes

import (
	"context"
	"errors"
	"net/http"
	"time"

	hubApplications "github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// agentImageStatusTimeout bounds how long the hub waits for an agent to report
// image versions. Checking for newer images queries every service's registry.
const agentImageStatusTimeout = 30 * time.Second

type applicationImageResponse struct {
	Service         string `json:"service"`
	Image           string `json:"image"`
	CurrentDigest   string `json:"currentDigest,omitempty"`
	CurrentVersion  string `json:"currentVersion,omitempty"`
	LatestDigest    string `json:"latestDigest,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Error           string `json:"error,omitempty"`
}

type applicationImagesResponse struct {
	Images []applicationImageResponse `json:"images"`
}

// ListApplicationImagesHandler reports the image version every service of an
// application runs. With ?latest=true it also reports the digest a pull would
// fetch, which requires the agent to query the registries.
func ListApplicationImagesHandler(c *gin.Context) {
	application, ok := findApplication(c)
	if !ok {
		return
	}
	if websocket.DefaultHub == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hub not initialized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), agentImageStatusTimeout)
	defer cancel()

	checkLatest := c.Query("latest") == "true"
	result, err := websocket.DefaultHub.RequestImageStatus(ctx, application.AgentId, application.Id, application.Name.String(), checkLatest)
	if err != nil {
		if errors.Is(err, websocket.ErrAgentOffline) {
			c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot read image versions"})
			return
		}
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "agent did not respond to the image status request"})
		return
	}
	if result.ErrorMessage != "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent failed to read image versions: " + result.ErrorMessage})
		return
	}

	resp := applicationImagesResponse{Images: make([]applicationImageResponse, 0, len(result.Images))}
	for _, img := range result.Images {
		resp.Images = append(resp.Images, applicationImageResponse{
			Service:         img.Service,
			Image:           img.Image,
			CurrentDigest:   img.CurrentDigest,
			CurrentVersion:  img.CurrentVersion,
			LatestDigest:    img.LatestDigest,
			UpdateAvailable: img.UpdateAvailable,
			Error:           img.ErrorMessage,
		})
	}
	c.JSON(http.StatusOK, resp)
}

// PullApplicationImagesHandler asks the agent to pull the latest images of an
// application and redeploy the services whose image changed.
func PullApplicationImagesHandler(c *gin.Context) {
	application, ok := findApplication(c)
	if !ok {
		return
	}

	actorUserID, actorName := eventActor(c)
	if !hubApplications.TriggerManualImagePull(&application, actorUserID, actorName) {
		c.JSON(http.StatusConflict, gin.H{"error": "agent is offline; cannot pull images"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "image pull started"})
}

func findApplication(c *gin.Context) (models.Application, bool) {
	application, err := gorm.G[models.Application](db.DB).Where("id = ?", c.Param("id")).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
			return application, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return application, false
	}
	return application, true
}
