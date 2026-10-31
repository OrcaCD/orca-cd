package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var applicationEnvLog = logger.New("application-env", false)

const (
	envListTimeout   = 30 * time.Second
	envUpdateTimeout = 2 * time.Minute
	// envWriteGrace is the time left to write the response after the wait ended.
	envWriteGrace = 10 * time.Second

	maxEnvPathBytes = 4096
	maxEnvFiles     = 100

	envSavedRedeployFailedPrefix = "the changes were saved, but redeploying the application failed"
)

type envEntryResponse struct {
	Key      string  `json:"key"`
	IsSecret bool    `json:"isSecret"`
	Value    *string `json:"value,omitempty"`
	IsSet    bool    `json:"isSet"`
}

type envFileResponse struct {
	Path     string             `json:"path"`
	Services []string           `json:"services"`
	Revision string             `json:"revision"`
	Entries  []envEntryResponse `json:"entries"`
}

type envListResponse struct {
	Files []envFileResponse `json:"files"`
}

type envChangeRequest struct {
	Key      string `json:"key"`
	Delete   bool   `json:"delete"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

type envFileChangesRequest struct {
	Path             string             `json:"path"`
	ExpectedRevision string             `json:"expectedRevision"`
	Changes          []envChangeRequest `json:"changes"`
}

type updateEnvRequest struct {
	Files []envFileChangesRequest `json:"files"`
}

func ListApplicationEnvHandler(c *gin.Context) {
	application, agent, ok := loadEnvTarget(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), envListTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.ListEnv(ctx, application.AgentId, application.Id, application.Name.String())
	if err != nil {
		respondEnvTransportError(c, agent, err)
		return
	}
	if !result.Success {
		c.JSON(http.StatusBadGateway, gin.H{"error": envAgentError(result.ErrorMessage, "agent failed to list environment files")})
		return
	}

	response := envListResponse{Files: make([]envFileResponse, 0, len(result.EnvFiles))}
	for _, file := range result.EnvFiles {
		if file == nil {
			continue
		}
		response.Files = append(response.Files, toEnvFileResponse(file))
	}
	c.JSON(http.StatusOK, response)
}

func UpdateApplicationEnvHandler(c *gin.Context) {
	var req updateEnvRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	files, err := validateEnvUpdate(req.Files)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	application, agent, ok := loadEnvTarget(c)
	if !ok {
		return
	}

	// The request context carries the global 30s timeout, which is shorter than
	// a redeploy. Detach from it so the audit entry is still written afterwards.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), envUpdateTimeout)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)

	// The server's write timeout is shorter than a redeploy, which would drop the response.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(envUpdateTimeout + envWriteGrace))

	result, err := websocket.DefaultHub.UpdateEnv(ctx, application.AgentId, application.Id, application.Name.String(), files)
	if err != nil {
		respondEnvTransportError(c, agent, err)
		return
	}

	if result.Conflict {
		c.JSON(http.StatusConflict, gin.H{
			"error":    "the environment settings changed in the meantime",
			"conflict": true,
		})
		return
	}

	if result.Success {
		utils.RecordAuditLog(c, "updated-environment", "application", application.Id)
		c.JSON(http.StatusOK, gin.H{"message": "environment updated"})
		return
	}

	// The values were persisted, so the change is audited even though the
	// redeploy failed.
	if strings.HasPrefix(result.ErrorMessage, envSavedRedeployFailedPrefix) {
		utils.RecordAuditLog(c, "updated-environment", "application", application.Id)
	}
	c.JSON(http.StatusBadGateway, gin.H{"error": envAgentError(result.ErrorMessage, "agent failed to update environment files")})
}

func loadEnvTarget(c *gin.Context) (models.Application, models.Agent, bool) {
	var application models.Application
	var agent models.Agent

	if websocket.DefaultHub == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "hub not initialized"})
		return application, agent, false
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", c.Param("id")).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "application not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return application, agent, false
	}

	agent, err = gorm.G[models.Agent](db.DB).Where("id = ?", application.AgentId).First(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return application, agent, false
	}

	if _, online := websocket.DefaultHub.GetClient(agent.Id); !online {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent is offline; environment variables cannot be viewed or changed"})
		return application, agent, false
	}

	if !version.SupportsEnvManagement(agent.Version) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": fmt.Sprintf("the agent does not support managing environment variables, please update it to %s or newer", strings.TrimSuffix(version.MinEnvAgentVersion, "-0"))})
		return application, agent, false
	}

	return application, agent, true
}

func respondEnvTransportError(c *gin.Context, agent models.Agent, err error) {
	switch {
	case errors.Is(err, websocket.ErrAgentOffline):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent is offline; environment variables cannot be viewed or changed"})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "agent did not respond in time"})
	default:
		applicationEnvLog.Error().Err(err).Str("agent_id", agent.Id).Msg("environment request failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func envAgentError(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

func toEnvFileResponse(file *messages.EnvFile) envFileResponse {
	response := envFileResponse{
		Path:     file.Path,
		Services: file.Services,
		Revision: file.Revision,
		Entries:  make([]envEntryResponse, 0, len(file.Entries)),
	}
	if response.Services == nil {
		response.Services = []string{}
	}
	for _, entry := range file.Entries {
		if entry == nil {
			continue
		}
		item := envEntryResponse{Key: entry.Key, IsSecret: entry.IsSecret}
		if entry.IsSecret {
			item.IsSet = entry.IsSet
		} else {
			value := entry.Value
			item.Value = &value
			item.IsSet = true
		}
		response.Entries = append(response.Entries, item)
	}
	return response
}

func validateEnvUpdate(files []envFileChangesRequest) ([]*messages.EnvFileChanges, error) {
	if len(files) == 0 {
		return nil, errors.New("no changes")
	}
	if len(files) > maxEnvFiles {
		return nil, fmt.Errorf("at most %d env files can be changed at once", maxEnvFiles)
	}

	result := make([]*messages.EnvFileChanges, 0, len(files))
	seenPaths := make(map[string]struct{}, len(files))

	for _, file := range files {
		if file.Path == "" || len(file.Path) > maxEnvPathBytes || !utf8.ValidString(file.Path) || strings.ContainsRune(file.Path, 0) {
			return nil, errors.New("invalid env file path")
		}
		if _, dup := seenPaths[file.Path]; dup {
			return nil, fmt.Errorf("env file %q is listed more than once", file.Path)
		}
		seenPaths[file.Path] = struct{}{}

		if file.ExpectedRevision == "" {
			return nil, fmt.Errorf("env file %q: expectedRevision is required", file.Path)
		}
		if len(file.Changes) == 0 {
			return nil, fmt.Errorf("env file %q: no changes", file.Path)
		}
		if len(file.Changes) > envrules.MaxKeysPerFile {
			return nil, fmt.Errorf("env file %q: at most %d changes per file", file.Path, envrules.MaxKeysPerFile)
		}

		changes := make([]*messages.EnvChange, 0, len(file.Changes))
		seenKeys := make(map[string]struct{}, len(file.Changes))

		for _, change := range file.Changes {
			if !utf8.ValidString(change.Key) {
				return nil, fmt.Errorf("env file %q: key is not valid UTF-8", file.Path)
			}
			if err := envrules.ValidateKey(change.Key); err != nil {
				return nil, fmt.Errorf("env file %q: key %q %s", file.Path, change.Key, err)
			}
			if _, dup := seenKeys[change.Key]; dup {
				return nil, fmt.Errorf("env file %q: key %q is listed more than once", file.Path, change.Key)
			}
			seenKeys[change.Key] = struct{}{}

			value := change.Value
			if change.Delete {
				value = ""
			} else if err := envrules.ValidateValue(value); err != nil {
				return nil, fmt.Errorf("env file %q: value of key %q %s", file.Path, change.Key, err)
			}

			changes = append(changes, &messages.EnvChange{
				Key:      change.Key,
				Delete:   change.Delete,
				Value:    value,
				IsSecret: change.IsSecret,
			})
		}

		result = append(result, &messages.EnvFileChanges{
			Path:             file.Path,
			ExpectedRevision: file.ExpectedRevision,
			Changes:          changes,
		})
	}

	return result, nil
}
