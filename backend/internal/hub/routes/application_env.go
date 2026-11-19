package routes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/dotenv"
	"github.com/OrcaCD/orca-cd/internal/shared/envrules"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var applicationEnvLog = logger.New("application-env", false)

const (
	envListTimeout   = 30 * time.Second
	envUpdateTimeout = envrules.LockWaitTimeout + envrules.WriteTimeout + envrules.RedeployTimeout + 30*time.Second
	// envWriteGrace is the time left to write the response after the wait ended.
	envWriteGrace = 10 * time.Second

	maxEnvPathBytes = 4096
	maxEnvFiles     = 100
)

type envEntryResponse struct {
	Key      string  `json:"key" required:"true"`
	IsSecret bool    `json:"isSecret" required:"true"`
	Value    *string `json:"value,omitempty" doc:"Left out for secrets, whose values the hub never returns"`
	IsSet    bool    `json:"isSet" required:"true" doc:"Whether the variable has a value"`
}

type envFileResponse struct {
	Path     string             `json:"path" required:"true" doc:"The path of the env file, relative to the compose file"`
	Services []string           `json:"services" required:"true" doc:"The services that use the file"`
	Revision string             `json:"revision" required:"true" doc:"Pass it as expectedRevision when changing the file"`
	Entries  []envEntryResponse `json:"entries" required:"true"`
}

type envListResponse struct {
	Files []envFileResponse `json:"files" required:"true"`
}

type envListOutput struct {
	Body envListResponse
}

type envChangeRequest struct {
	Key      string `json:"key"`
	Delete   bool   `json:"delete" doc:"Removes the variable, value is ignored"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

type envFileChangesRequest struct {
	Path             string             `json:"path"`
	ExpectedRevision string             `json:"expectedRevision" doc:"The revision the changes are based on. The update fails with 409 if the file changed since."`
	Changes          []envChangeRequest `json:"changes"`
}

type updateEnvRequest struct {
	Files []envFileChangesRequest `json:"files"`
}

// envError is an error answer with a flag the web UI reacts to, which Huma's
// errors can't carry.
type envError struct {
	status                 int
	Message                string `json:"error"`
	Conflict               bool   `json:"conflict,omitempty"`
	SavedButRedeployFailed bool   `json:"savedButRedeployFailed,omitempty"`
}

func (e *envError) Error() string {
	return e.Message
}

func (e *envError) GetStatus() int {
	return e.status
}

const envServerErrorsDoc = "Answers 503 while the agent is offline, 504 when it doesn't respond in time and 502 when it fails."

var ListApplicationEnvHandler = restapi.Handler(huma.Operation{
	OperationID: "list-application-env",
	Method:      http.MethodGet,
	Path:        "/applications/{id}/env",
	Summary:     "List the environment variables of an application",
	Description: "Loads the env files of the application from its agent. Values of secrets are left out. " +
		envServerErrorsDoc + " " + requires(models.ResourceApplication, models.ActionRead),
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusNotFound, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *idInput) (*envListOutput, error) {
	if DemoMode {
		return listDemoApplicationEnv(ctx, input.ID)
	}

	application, agent, err := loadEnvTarget(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, envListTimeout)
	defer cancel()

	result, err := websocket.DefaultHub.ListEnv(ctx, application.AgentId, application.Id, application.Name.String())
	if err != nil {
		return nil, envTransportError(agent, err)
	}
	if !result.Success {
		return nil, huma.Error502BadGateway(envAgentError(result.ErrorMessage, "The agent failed to load the environment files."))
	}

	response := envListResponse{Files: make([]envFileResponse, 0, len(result.EnvFiles))}
	for _, file := range result.EnvFiles {
		if file == nil {
			continue
		}
		response.Files = append(response.Files, toEnvFileResponse(file))
	}
	return &envListOutput{Body: response}, nil
})

var UpdateApplicationEnvHandler = restapi.Handler(huma.Operation{
	OperationID: "update-application-env",
	Method:      http.MethodPut,
	Path:        "/applications/{id}/env",
	Summary:     "Change the environment variables of an application",
	Description: "Writes the changes to the env files on the agent and redeploys the application, which can take a while. " +
		"A 409 answer has `conflict: true` when a file changed since expectedRevision. " +
		"A 502 answer has `savedButRedeployFailed: true` when the changes were saved but the redeploy failed. " +
		envServerErrorsDoc + " Needs the `application:update` and `application:deploy` permissions.",
	Tags:     []string{tagApplications},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the application"`
	Body updateEnvRequest
}) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	files, err := validateEnvUpdate(input.Body.Files)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	application, agent, err := loadEnvTarget(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	// The request context carries the global 30s timeout, which is shorter than
	// a redeploy. Detach from it so the audit entry is still written afterwards.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), envUpdateTimeout)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)

	// The server's write timeout is shorter than a redeploy, which would drop the response.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(envUpdateTimeout + envWriteGrace))

	result, err := websocket.DefaultHub.UpdateEnv(ctx, application.AgentId, application.Id, application.Name.String(), files)
	if err != nil {
		return nil, envTransportError(agent, err)
	}

	if result.Conflict {
		return nil, &envError{
			status:   http.StatusConflict,
			Message:  "The environment settings were changed by someone else in the meantime.",
			Conflict: true,
		}
	}

	if result.Success {
		utils.RecordAuditLog(c, "updated-environment", "application", application.Id)
		return restapi.Message("environment updated"), nil
	}

	// The values were persisted, so the change is audited even though the
	// redeploy failed.
	if strings.HasPrefix(result.ErrorMessage, envrules.SavedRedeployFailedPrefix) {
		utils.RecordAuditLog(c, "updated-environment", "application", application.Id)
		return nil, &envError{
			status:                 http.StatusBadGateway,
			Message:                utils.CapitalizeFirst(result.ErrorMessage),
			SavedButRedeployFailed: true,
		}
	}
	return nil, huma.Error502BadGateway(envAgentError(result.ErrorMessage, "The agent failed to update the environment files."))
})

type parseEnvPayload struct {
	Content string `json:"content" doc:"The content of an env file, at most 1 MiB"`
}

type parsedEnvEntry struct {
	Key      string `json:"key" required:"true"`
	Value    string `json:"value" required:"true"`
	IsSecret bool   `json:"isSecret" required:"true" doc:"False for keys marked with a # orcacd:public comment"`
}

type parseEnvResponse struct {
	Entries      []parsedEnvEntry `json:"entries" required:"true"`
	IgnoredLines int              `json:"ignoredLines" required:"true" doc:"Number of lines that are no valid assignment"`
}

// maxParseEnvBodyBytes leaves room for JSON escaping, which grows a control
// character to six bytes.
const maxParseEnvBodyBytes = 6*envrules.MaxFileBytes + 1024

var ParseEnvHandler = limitParseEnvBody(restapi.Handler(huma.Operation{
	OperationID:  "parse-env-file",
	Method:       http.MethodPost,
	Path:         "/env/parse",
	Summary:      "Parse an env file",
	Description:  "Parses the content of an env file with the Docker Compose syntax, e.g. to import it. Nothing is saved.",
	Tags:         []string{tagApplications},
	Security:     restapi.Authenticated,
	Errors:       []int{http.StatusBadRequest},
	MaxBodyBytes: maxParseEnvBodyBytes,
}, func(_ context.Context, input *struct{ Body parseEnvPayload }) (*struct{ Body parseEnvResponse }, error) {
	req := input.Body
	if len(req.Content) > envrules.MaxFileBytes {
		return nil, huma.Error400BadRequest(fmt.Sprintf("the file is larger than %d bytes", envrules.MaxFileBytes))
	}

	doc := dotenv.Parse(req.Content)
	entries := doc.Entries()
	response := parseEnvResponse{
		Entries:      make([]parsedEnvEntry, 0, len(entries)),
		IgnoredLines: doc.IgnoredCount(),
	}
	for _, entry := range entries {
		response.Entries = append(response.Entries, parsedEnvEntry{Key: entry.Key, Value: entry.Value, IsSecret: !entry.Public})
	}
	return &struct{ Body parseEnvResponse }{Body: response}, nil
}))

// limitParseEnvBody answers a body over maxParseEnvBodyBytes with 400 like
// before, where Huma's MaxBodyBytes answers 413.
func limitParseEnvBody(next gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxParseEnvBodyBytes))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		next(c)
	}
}

func loadEnvTarget(ctx context.Context, id string) (models.Application, models.Agent, error) {
	var application models.Application
	var agent models.Agent

	if websocket.DefaultHub == nil {
		return application, agent, huma.Error500InternalServerError("hub not initialized")
	}

	application, err := gorm.G[models.Application](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return application, agent, huma.Error404NotFound("application not found")
		}
		return application, agent, internalError()
	}

	agent, err = gorm.G[models.Agent](db.DB).Where("id = ?", application.AgentId).First(ctx)
	if err != nil {
		return application, agent, internalError()
	}

	if _, online := websocket.DefaultHub.GetClient(agent.Id); !online {
		return application, agent, huma.Error503ServiceUnavailable("The agent is offline. Environment variables can only be viewed or changed while it is connected.")
	}

	if agent.Version == "" || version.CheckAgentCompatibility(version.Version, agent.Version) == version.AgentIncompatible {
		return application, agent, huma.Error422UnprocessableEntity(fmt.Sprintf("This agent is too old to manage environment variables. Please update it to version %s or newer.", strings.TrimSuffix(version.MinAgentVersion, "-0")))
	}

	return application, agent, nil
}

func envTransportError(agent models.Agent, err error) error {
	switch {
	case errors.Is(err, websocket.ErrAgentOffline):
		return huma.Error503ServiceUnavailable("The agent is offline. Environment variables can only be viewed or changed while it is connected.")
	case errors.Is(err, context.DeadlineExceeded):
		return huma.Error504GatewayTimeout("The agent did not respond in time. Please try again.")
	default:
		applicationEnvLog.Error().Err(err).Str("agent_id", agent.Id).Msg("environment request failed")
		return internalError()
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
