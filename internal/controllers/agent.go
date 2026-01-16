package controllers

import (
	"net/http"

	hubgrpc "github.com/OrcaCD/orca-cd/internal/hub/grpc"
	"github.com/gin-gonic/gin"
)

// AgentResponse represents an agent in API responses
type AgentResponse struct {
	ID           string            `json:"id"`
	Version      string            `json:"version"`
	Capabilities []string          `json:"capabilities"`
	Labels       map[string]string `json:"labels"`
	Connected    bool              `json:"connected"`
}

// GetAgents returns a list of all connected agents
func GetAgents(c *gin.Context) {
	agents := hubgrpc.DefaultHubService.GetConnectedAgents()
	
	response := make([]AgentResponse, 0, len(agents))
	for _, agent := range agents {
		response = append(response, AgentResponse{
			ID:           agent.ID,
			Version:      agent.Version,
			Capabilities: agent.Capabilities,
			Labels:       agent.Labels,
			Connected:    true,
		})
	}
	
	c.JSON(http.StatusOK, response)
}

// GetAgent returns a specific agent by ID
func GetAgent(c *gin.Context) {
	id := c.Param("id")
	
	agent, found := hubgrpc.DefaultHubService.GetAgent(id)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
		return
	}
	
	response := AgentResponse{
		ID:           agent.ID,
		Version:      agent.Version,
		Capabilities: agent.Capabilities,
		Labels:       agent.Labels,
		Connected:    true,
	}
	
	c.JSON(http.StatusOK, response)
}
