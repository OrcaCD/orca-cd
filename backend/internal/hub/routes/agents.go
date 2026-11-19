package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"github.com/OrcaCD/orca-cd/internal/shared/iplist"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const (
	defaultAgentIcon   = "server"
	maxAgentAllowedIPs = 50
)

func normalizeAgentAllowedIPs(entries []string) ([]string, error) {
	seen := make(map[string]struct{}, len(entries))
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		normalized, err := iplist.Normalize(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid IP address or CIDR range %q", strings.TrimSpace(entry))
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) > maxAgentAllowedIPs {
		return nil, fmt.Errorf("list exceeds maximum of %d entries", maxAgentAllowedIPs)
	}
	return result, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

type agentResponse struct {
	Id            string                     `json:"id" required:"true"`
	Icon          string                     `json:"icon" required:"true"`
	Name          string                     `json:"name" required:"true"`
	Status        string                     `json:"status" required:"true" enum:"online,offline,error"`
	AppsCount     int64                      `json:"appsCount" required:"true" doc:"Number of applications deployed by the agent"`
	LastSeen      *string                    `json:"lastSeen" required:"true" nullable:"true" format:"date-time"`
	Version       *string                    `json:"version" required:"true" nullable:"true"`
	DockerVersion *string                    `json:"dockerVersion" required:"true" nullable:"true"`
	VersionStatus version.AgentCompatibility `json:"versionStatus" required:"true"`
	AllowedIPs    []string                   `json:"allowedIps" required:"true" doc:"IP addresses and CIDR ranges the agent may connect from. Empty allows any."`
	LastIP        *string                    `json:"lastIp" required:"true" nullable:"true"`
	CreatedAt     string                     `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt     string                     `json:"updatedAt" required:"true" format:"date-time"`
}

type agentWithTokenResponse struct {
	agentResponse
	AuthToken  string `json:"authToken,omitempty" doc:"The token the agent connects with. Only returned once."`
	AgentImage string `json:"agentImage,omitempty" doc:"The agent image matching the hub version"`
}

type createAgentRequest struct {
	Name       string   `json:"name" required:"true" minLength:"1" maxLength:"128"`
	Icon       string   `json:"icon,omitempty" maxLength:"128" doc:"Defaults to server"`
	AllowedIPs []string `json:"allowedIps,omitempty" doc:"IP addresses and CIDR ranges the agent may connect from"`
}

type updateAgentRequest struct {
	Name string `json:"name" required:"true" minLength:"1" maxLength:"128"`
	Icon string `json:"icon,omitempty" maxLength:"128" doc:"Leave it empty to keep the icon"`
	// nil leaves the whitelist unchanged, an empty list clears it.
	AllowedIPs *[]string `json:"allowedIps,omitempty" doc:"Leave it out to keep the list, an empty list allows any IP"`
}

type agentOutput struct {
	Body agentResponse
}

type agentWithTokenOutput struct {
	Body agentWithTokenResponse
}

const agentStatusOffline = "offline"

func toAgentStatus(status models.AgentStatus) string {
	switch status {
	case models.AgentStatusOnline:
		return "online"
	case models.AgentStatusError:
		return "error"
	default:
		return agentStatusOffline
	}
}

func toAgentResponse(agent *models.Agent, appsCount int64) agentResponse {
	response := agentResponse{
		Id:            agent.Id,
		Icon:          agent.Icon,
		Name:          agent.Name.String(),
		Status:        toAgentStatus(agent.Status),
		AppsCount:     appsCount,
		VersionStatus: version.CheckAgentCompatibility(version.Version, agent.Version),
		AllowedIPs:    append([]string{}, agent.AllowedIPs...),
		CreatedAt:     agent.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     agent.UpdatedAt.Format(time.RFC3339),
	}

	if agent.LastSeen != nil {
		lastSeen := agent.LastSeen.Format(time.RFC3339)
		response.LastSeen = &lastSeen
	}
	if agent.Version != "" {
		response.Version = &agent.Version
	}
	if agent.DockerVersion != "" {
		response.DockerVersion = &agent.DockerVersion
	}
	if agent.LastIP != "" {
		response.LastIP = &agent.LastIP
	}

	return response
}

type appCountByAgentRow struct {
	AgentId   string `gorm:"column:agent_id"`
	AppsCount int64  `gorm:"column:apps_count"`
}

func countApplicationsByAgentID(ctx context.Context, agentId string) (int64, error) {
	return gorm.G[models.Application](db.DB).Where("agent_id = ?", agentId).Count(ctx, "*")
}

func countApplicationsByAgentIDs(ctx context.Context, agentIds []string) (map[string]int64, error) {
	counts := make(map[string]int64, len(agentIds))
	if len(agentIds) == 0 {
		return counts, nil
	}

	rows := []appCountByAgentRow{}
	if err := db.DB.WithContext(ctx).
		Model(&models.Application{}).
		Select("agent_id, COUNT(*) AS apps_count").
		Where("agent_id IN ?", agentIds).
		Group("agent_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for i := range rows {
		counts[rows[i].AgentId] = rows[i].AppsCount
	}

	return counts, nil
}

var ListAgentsHandler = restapi.Handler(huma.Operation{
	OperationID: "list-agents",
	Method:      http.MethodGet,
	Path:        "/agents",
	Summary:     "List agents",
	Description: "Lists the agents the user may view.",
	Tags:        []string{tagAgents},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []agentResponse }, error) {
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}

	agents, err := gorm.G[models.Agent](db.DB).
		Scopes(perms.Scope(models.ResourceAgent, models.ActionRead, "id")).
		Order("created_at ASC").
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	agentIds := make([]string, 0, len(agents))
	for i := range agents {
		agentIds = append(agentIds, agents[i].Id)
	}

	appsCountByAgentId, err := countApplicationsByAgentIDs(ctx, agentIds)
	if err != nil {
		return nil, internalError()
	}

	response := make([]agentResponse, 0, len(agents))
	for i := range agents {
		response = append(response, toAgentResponse(&agents[i], appsCountByAgentId[agents[i].Id]))
	}

	return &struct{ Body []agentResponse }{Body: response}, nil
})

var GetAgentHandler = restapi.Handler(huma.Operation{
	OperationID: "get-agent",
	Method:      http.MethodGet,
	Path:        "/agents/{id}",
	Summary:     "Get an agent",
	Description: requires(models.ResourceAgent, models.ActionRead),
	Tags:        []string{tagAgents},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*agentOutput, error) {
	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("agent not found")
		}
		return nil, internalError()
	}

	appsCount, err := countApplicationsByAgentID(ctx, agent.Id)
	if err != nil {
		return nil, internalError()
	}

	return &agentOutput{Body: toAgentResponse(&agent, appsCount)}, nil
})

var CreateAgentHandler = restapi.Handler(huma.Operation{
	OperationID:   "create-agent",
	Method:        http.MethodPost,
	Path:          "/agents",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create an agent",
	Description:   "Returns the token the agent connects with, which is only shown once. " + requires(models.ResourceAgent, models.ActionCreate),
	Tags:          []string{tagAgents},
	Security:      restapi.Authenticated,
	Errors:        []int{http.StatusBadRequest},
}, func(ctx context.Context, input *struct{ Body createAgentRequest }) (*agentWithTokenOutput, error) {
	req := input.Body
	allowedIPs, err := normalizeAgentAllowedIPs(req.AllowedIPs)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid allowed IPs: " + err.Error())
	}

	var (
		agent     models.Agent
		authToken string
	)

	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		agent = models.Agent{
			Name:       crypto.EncryptedString(req.Name),
			Icon:       defaultString(req.Icon, defaultAgentIcon),
			Status:     models.AgentStatusOffline,
			AllowedIPs: allowedIPs,
		}

		if err := gorm.G[models.Agent](tx).Create(ctx, &agent); err != nil {
			return err
		}

		token, err := auth.GenerateAgentToken(&agent)
		if err != nil {
			return err
		}

		authToken = token

		if _, err := gorm.G[models.Agent](tx).Where("id = ?", agent.Id).Updates(ctx, models.Agent{
			KeyId:            agent.KeyId,
			SigningPublicKey: agent.SigningPublicKey,
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, internalError()
	}

	agent, err = gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).First(ctx)
	if err != nil {
		return nil, internalError()
	}

	appsCount, err := countApplicationsByAgentID(ctx, agent.Id)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "created", "agent", agent.Id)
	sse.PublishUpdate(sse.TopicAgents.For(agent.Id))

	return &agentWithTokenOutput{Body: agentWithTokenResponse{
		agentResponse: toAgentResponse(&agent, appsCount),
		AuthToken:     authToken,
		AgentImage:    version.AgentImage(),
	}}, nil
})

var UpdateAgentHandler = restapi.Handler(huma.Operation{
	OperationID: "update-agent",
	Method:      http.MethodPut,
	Path:        "/agents/{id}",
	Summary:     "Update an agent",
	Description: "Changing the allowed IPs disconnects the agent if it is connected from an IP that is no longer allowed. " +
		requires(models.ResourceAgent, models.ActionUpdate),
	Tags:     []string{tagAgents},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the agent"`
	Body updateAgentRequest
}) (*agentOutput, error) {
	id, req := input.ID, input.Body

	updates := models.Agent{Name: crypto.EncryptedString(req.Name)}
	columns := []any{}
	if req.Icon != "" {
		updates.Icon = req.Icon
		columns = append(columns, "icon")
	}
	if req.AllowedIPs != nil {
		allowedIPs, err := normalizeAgentAllowedIPs(*req.AllowedIPs)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid allowed IPs: " + err.Error())
		}
		updates.AllowedIPs = allowedIPs
		columns = append(columns, "allowed_ips")
	}
	rowsAffected, err := gorm.G[models.Agent](db.DB).Where("id = ?", id).Select("name", columns...).Updates(ctx, updates)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("agent not found")
	}

	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("agent not found")
		}
		return nil, internalError()
	}

	appsCount, err := countApplicationsByAgentID(ctx, agent.Id)
	if err != nil {
		return nil, internalError()
	}

	if req.AllowedIPs != nil {
		if client, ok := websocket.DefaultHub.GetClient(id); ok && !iplist.Allows(agent.AllowedIPs, client.RemoteIP()) {
			websocket.DefaultHub.BeginDisconnect(client)
		}
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "updated", "agent", id)
	sse.PublishUpdate(sse.TopicAgents.For(id))

	return &agentOutput{Body: toAgentResponse(&agent, appsCount)}, nil
})

var RotateAgentTokenHandler = restapi.Handler(huma.Operation{
	OperationID: "rotate-agent-token",
	Method:      http.MethodPost,
	Path:        "/agents/{id}/rotate-token",
	Summary:     "Rotate the token of an agent",
	Description: "Returns a new token, which is only shown once. The old token stops working and a connected agent is disconnected. " +
		requires(models.ResourceAgent, models.ActionUpdate),
	Tags:     []string{tagAgents},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*agentWithTokenOutput, error) {
	agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("agent not found")
		}
		return nil, internalError()
	}

	authToken, err := auth.GenerateAgentToken(&agent)
	if err != nil {
		return nil, internalError()
	}

	if _, err := gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).Updates(ctx, models.Agent{
		KeyId:            agent.KeyId,
		SigningPublicKey: agent.SigningPublicKey,
	}); err != nil {
		return nil, internalError()
	}
	if client, ok := websocket.DefaultHub.GetClient(agent.Id); ok {
		// The socket is closed immediately. WsHandler then drains accepted messages
		// within a bounded window, cleans up state, and releases the registration.
		websocket.DefaultHub.BeginDisconnect(client)
	}

	agent, err = gorm.G[models.Agent](db.DB).Where("id = ?", agent.Id).First(ctx)
	if err != nil {
		return nil, internalError()
	}

	appsCount, err := countApplicationsByAgentID(ctx, agent.Id)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "rotated-token", "agent", agent.Id)
	sse.PublishUpdate(sse.TopicAgents.For(agent.Id))

	return &agentWithTokenOutput{Body: agentWithTokenResponse{
		agentResponse: toAgentResponse(&agent, appsCount),
		AuthToken:     authToken,
		AgentImage:    version.AgentImage(),
	}}, nil
})

var DeleteAgentHandler = restapi.Handler(huma.Operation{
	OperationID: "delete-agent",
	Method:      http.MethodDelete,
	Path:        "/agents/{id}",
	Summary:     "Delete an agent",
	Description: "Deletes its applications too, so it also needs `application:delete` on each of them. " +
		requires(models.ResourceAgent, models.ActionDelete),
	Tags:     []string{tagAgents},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusForbidden, http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	id := input.ID
	// The applications of the agent are deleted with it.
	if err := checkDeleteApplicationsOf(c, "agent_id", id); err != nil {
		return nil, err
	}

	rowsAffected, err := gorm.G[models.Agent](db.DB).Where("id = ?", id).Delete(ctx)
	if err != nil {
		return nil, internalError()
	}
	if rowsAffected == 0 {
		return nil, huma.Error404NotFound("agent not found")
	}

	if client, ok := websocket.DefaultHub.GetClient(id); ok {
		// WsHandler owns the matching FinishDisconnect after its cleanup.
		websocket.DefaultHub.BeginDisconnect(client)
	}

	utils.RecordAuditLog(c, "deleted", "agent", id)

	// Its applications were deleted with it
	sse.PublishUpdate(sse.TopicAgents, sse.TopicApplications)
	// After the update, so clients still get it before losing access
	pruneGrants(ctx)

	return restapi.Message("agent deleted"), nil
})
