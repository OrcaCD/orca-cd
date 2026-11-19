package mcpserver

import (
	"context"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

var listAgentsTool = &mcp.Tool{
	Name:        "list_agents",
	Description: "Lists the agents the token can read. An agent runs on a Docker host and deploys the applications assigned to it, it can only deploy while it is online.",
	Annotations: &mcp.ToolAnnotations{Title: "List agents", ReadOnlyHint: true, OpenWorldHint: new(false)},
}

type agentSummary struct {
	Id            string     `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status" jsonschema:"online, offline or error"`
	LastSeen      *time.Time `json:"lastSeen,omitempty"`
	Version       string     `json:"version,omitempty"`
	DockerVersion string     `json:"dockerVersion,omitempty"`
}

type listAgentsOutput struct {
	Agents []agentSummary `json:"agents"`
}

func (c *caller) listAgents(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listAgentsOutput, error) {
	agents, err := gorm.G[models.Agent](db.DB).
		Scopes(c.perms.Scope(models.ResourceAgent, models.ActionRead, "id")).
		Order("created_at ASC").
		Find(ctx)
	if err != nil {
		Log.Error().Err(err).Msg("failed to list agents")
		return nil, listAgentsOutput{}, errInternal
	}

	output := listAgentsOutput{Agents: make([]agentSummary, 0, len(agents))}
	for i := range agents {
		agent := &agents[i]
		output.Agents = append(output.Agents, agentSummary{
			Id:            agent.Id,
			Name:          agent.Name.String(),
			Status:        agentStatus(agent.Status),
			LastSeen:      agent.LastSeen,
			Version:       agent.Version,
			DockerVersion: agent.DockerVersion,
		})
	}
	return nil, output, nil
}

func agentStatus(status models.AgentStatus) string {
	switch status {
	case models.AgentStatusOnline:
		return "online"
	case models.AgentStatusError:
		return "error"
	default:
		return "offline"
	}
}
