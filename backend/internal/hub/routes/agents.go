package routes

import (
	"errors"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type agentResponse struct {
	Id        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	LastSeen  *string `json:"lastSeen"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

type agentWithTokenResponse struct {
	agentResponse
	AuthToken string `json:"authToken,omitempty"`
}

type createAgentRequest struct {
	Name string `json:"name" binding:"required,min=1,max=128"`
}

type updateAgentRequest struct {
	Name string `json:"name" binding:"required,min=1,max=128"`
}

func toAgentStatus(status models.AgentStatus) string {
	switch status {
	case models.AgentStatusOnline:
		return "online"
	case models.AgentStatusError:
		return "error"
	default:
		return "offline"
	}
}

func toAgentResponse(agent *models.Agent) agentResponse {
	response := agentResponse{
		Id:        agent.Id,
		Name:      agent.Name.String(),
		Status:    toAgentStatus(agent.Status),
		CreatedAt: agent.CreatedAt.Format(time.RFC3339),
		UpdatedAt: agent.UpdatedAt.Format(time.RFC3339),
	}

	if agent.LastSeen != nil {
		lastSeen := agent.LastSeen.Format(time.RFC3339)
		response.LastSeen = &lastSeen
	}

	return response
}

func ListAgentsHandler(c *gin.Context) {
	var agents []models.Agent
	if err := db.DB.WithContext(c.Request.Context()).Order("created_at ASC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]agentResponse, 0, len(agents))
	for i := range agents {
		response = append(response, toAgentResponse(&agents[i]))
	}

	c.JSON(http.StatusOK, response)
}

func GetAgentHandler(c *gin.Context) {
	id := c.Param("id")

	var agent models.Agent
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toAgentResponse(&agent))
}

func CreateAgentHandler(c *gin.Context) {
	var req createAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name is required and must be valid"})
		return
	}

	var (
		agent     models.Agent
		authToken string
	)

	err := db.DB.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		agent = models.Agent{
			Name:   crypto.EncryptedString(req.Name),
			Status: models.AgentStatusOffline,
		}

		if err := tx.Create(&agent).Error; err != nil {
			return err
		}

		token, err := auth.GenerateAgentToken(&agent)
		if err != nil {
			return err
		}

		authToken = token

		if err := tx.Save(&agent).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, agentWithTokenResponse{
		agentResponse: toAgentResponse(&agent),
		AuthToken:     authToken,
	})
}

func UpdateAgentHandler(c *gin.Context) {
	id := c.Param("id")

	var req updateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name is required and must be valid"})
		return
	}

	var agent models.Agent
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	agent.Name = crypto.EncryptedString(req.Name)

	if err := db.DB.WithContext(c.Request.Context()).Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, toAgentResponse(&agent))
}

func DeleteAgentHandler(c *gin.Context) {
	id := c.Param("id")

	result := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).Delete(&models.Agent{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agent deleted"})
}
