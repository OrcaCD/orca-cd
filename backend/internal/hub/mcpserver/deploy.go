package mcpserver

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	application_deployer "github.com/OrcaCD/orca-cd/internal/hub/deployer"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/aymanbagabas/go-udiff"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

// previewTTL is how long a preview can be deployed, so an old diff isn't
// approved by accident.
const previewTTL = 10 * time.Minute

var errPreviewMismatch = errors.New("the preview is expired, was already deployed, or the application changed since it was made; call preview_deploy again and show the user the new diff")

// previewKey signs the preview IDs. It isn't stored, previews end when the
// hub restarts.
var previewKey = rand.Text()

// now is replaced in tests.
var now = time.Now

// usedPreviews makes each preview deployable once, so a retrying client can't
// deploy again without the user seeing a new preview. The application state a
// preview covers doesn't change until a deploy succeeds.
var usedPreviews = previewSet{used: map[string]time.Time{}}

type previewSet struct {
	mu   sync.Mutex
	used map[string]time.Time
}

// use marks a preview as used until it expires. It reports false if it
// already was or has expired. Both are checked under the lock, so an entry is
// never dropped while its preview can still be deployed.
func (s *previewSet) use(previewID string, expiresAt time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := now()
	if current.After(expiresAt) {
		return false
	}
	if _, ok := s.used[previewID]; ok {
		return false
	}
	for id, expiry := range s.used {
		if current.After(expiry) {
			delete(s.used, id)
		}
	}
	s.used[previewID] = expiresAt
	return true
}

var previewDeployTool = &mcp.Tool{
	Name: "preview_deploy",
	Description: "Shows what deploying an application would change: the diff between the compose file the agent runs and the one that will be deployed, and warnings like an offline agent. " +
		"Returns the previewId that deploy_application needs. Show the diff to the user before deploying.",
	Annotations: &mcp.ToolAnnotations{Title: "Preview deploy", ReadOnlyHint: true, OpenWorldHint: new(false)},
}

var deployApplicationTool = &mcp.Tool{
	Name: "deploy_application",
	Description: "Deploys the compose file of the application's commit to its agent, like the deploy button in OrcaCD. " +
		"Needs the previewId of preview_deploy and is refused when the application changed since the preview. The deploy runs in the background, check its result with get_application.",
	Annotations: &mcp.ToolAnnotations{Title: "Deploy application", DestructiveHint: new(true), OpenWorldHint: new(false)},
}

type previewDeployOutput struct {
	PreviewId       string    `json:"previewId" jsonschema:"pass to deploy_application to deploy exactly this preview"`
	ExpiresAt       time.Time `json:"expiresAt"`
	ApplicationId   string    `json:"applicationId"`
	ApplicationName string    `json:"applicationName"`
	AgentName       string    `json:"agentName"`
	AgentStatus     string    `json:"agentStatus"`
	Commit          string    `json:"commit" jsonschema:"the commit whose compose file is deployed"`
	CommitMessage   string    `json:"commitMessage"`
	RunningCommit   string    `json:"runningCommit,omitempty" jsonschema:"the commit of the deployment the agent runs"`
	Changed         bool      `json:"changed" jsonschema:"whether the compose file differs from the running one"`
	Diff            string    `json:"diff,omitempty" jsonschema:"unified diff from the running compose file to the deployed one"`
	Warnings        []string  `json:"warnings"`
}

type deployApplicationInput struct {
	ApplicationId string `json:"applicationId" jsonschema:"the id of the application"`
	PreviewId     string `json:"previewId" jsonschema:"the previewId returned by preview_deploy"`
}

type deployApplicationOutput struct {
	Message string `json:"message"`
}

func (c *caller) previewDeploy(ctx context.Context, _ *mcp.CallToolRequest, in applicationInput) (*mcp.CallToolResult, previewDeployOutput, error) {
	application, err := c.loadApplication(ctx, in.ApplicationId, models.ActionDeploy)
	if err != nil {
		return nil, previewDeployOutput{}, err
	}
	running, err := runningDeployment(ctx, application)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to load running deployment")
		return nil, previewDeployOutput{}, errInternal
	}

	issuedAt := now()
	output := previewDeployOutput{
		PreviewId:       c.previewID(application, issuedAt),
		ExpiresAt:       issuedAt.Add(previewTTL).UTC().Truncate(time.Second),
		ApplicationId:   application.Id,
		ApplicationName: application.Name.String(),
		AgentName:       application.Agent.Name.String(),
		AgentStatus:     agentStatus(application.Agent.Status),
		Commit:          application.Commit,
		CommitMessage:   application.CommitMessage,
		Warnings:        []string{},
	}

	composeFile := application.ComposeFile.String()
	oldLabel, oldCompose := "/dev/null", ""
	if running == nil {
		output.Warnings = append(output.Warnings, "OrcaCD has no record of the deployment the agent runs, the diff shows the whole compose file.")
	} else {
		output.RunningCommit = running.Commit
		oldLabel, oldCompose = application.Path+"@"+running.Commit, running.ComposeFile.String()
	}
	// udiff.Unified would stop the hub with log.Fatalf if it failed.
	output.Diff, err = udiff.ToUnified(oldLabel, application.Path+"@"+application.Commit, oldCompose, udiff.Lines(oldCompose, composeFile), udiff.DefaultContextLines)
	if err != nil {
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to diff compose files")
		return nil, previewDeployOutput{}, errInternal
	}
	output.Changed = output.Diff != ""
	if running != nil && !output.Changed {
		output.Warnings = append(output.Warnings, "The compose file is the same as the running one, the deploy applies it again.")
	}

	if application.Agent.Status != models.AgentStatusOnline {
		output.Warnings = append(output.Warnings, "The agent "+application.Agent.Name.String()+" is not online, the deploy will fail.")
	}
	if application.SyncStatus == models.Syncing {
		output.Warnings = append(output.Warnings, "The application is being deployed right now.")
	}
	if application.RolledBackAt != nil {
		output.Warnings = append(output.Warnings, "The application was rolled back to an earlier deployment. Deploying runs the commit from Git again and resumes automatic syncs.")
	}
	if len(composeWarnings(application)) > 0 {
		output.Warnings = append(output.Warnings, "The compose file has warnings, get_application lists them.")
	}

	return nil, output, nil
}

func (c *caller) deployApplication(ctx context.Context, _ *mcp.CallToolRequest, in deployApplicationInput) (*mcp.CallToolResult, deployApplicationOutput, error) {
	application, err := c.loadApplication(ctx, in.ApplicationId, models.ActionDeploy)
	if err != nil {
		return nil, deployApplicationOutput{}, err
	}
	expiresAt, ok := c.validPreview(application, in.PreviewId)
	if !ok || !usedPreviews.use(in.PreviewId, expiresAt) {
		return nil, deployApplicationOutput{}, errPreviewMismatch
	}

	err = applications.Deploy(ctx, application, applications.SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: &c.user.Id,
		ActorName:   &c.user.Name,
	})
	if err != nil {
		if errors.Is(err, application_deployer.ErrAgentOffline) {
			return nil, deployApplicationOutput{}, errors.New("agent is offline; cannot deploy application. Call preview_deploy again to retry once it is online")
		}
		Log.Error().Err(err).Str("applicationId", application.Id).Msg("failed to trigger application deploy")
		return nil, deployApplicationOutput{}, errInternal
	}

	return nil, deployApplicationOutput{Message: "deployment started"}, nil
}

// runningDeployment returns the deployment the agent runs, or nil if it isn't known.
func runningDeployment(ctx context.Context, application *models.Application) (*models.ApplicationDeployment, error) {
	if application.CurrentDeploymentId == nil {
		return nil, nil
	}
	deployment, err := gorm.G[models.ApplicationDeployment](db.DB).
		Where("id = ? AND application_id = ?", *application.CurrentDeploymentId, application.Id).
		First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// previewID signs everything a preview shows, so a deploy is refused when any
// of it changed. It is not a security boundary, the token's scopes are: it
// keeps an agent from deploying something the user never saw.
func (c *caller) previewID(application *models.Application, issuedAt time.Time) string {
	issued := strconv.FormatInt(issuedAt.Unix(), 10)
	return issued + "." + base64.RawURLEncoding.EncodeToString(c.previewMAC(application, issued))
}

// validPreview checks a preview against the current state of the application
// and returns when it expires.
func (c *caller) validPreview(application *models.Application, previewID string) (time.Time, bool) {
	issued, signature, ok := strings.Cut(previewID, ".")
	if !ok {
		return time.Time{}, false
	}
	issuedUnix, err := strconv.ParseInt(issued, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	expiresAt := time.Unix(issuedUnix, 0).Add(previewTTL)
	if now().After(expiresAt) {
		return time.Time{}, false
	}
	// Strict, so each preview has one spelling and can't be used again in another.
	mac, err := base64.RawURLEncoding.Strict().DecodeString(signature)
	if err != nil || !hmac.Equal(mac, c.previewMAC(application, issued)) {
		return time.Time{}, false
	}
	return expiresAt, true
}

func (c *caller) previewMAC(application *models.Application, issued string) []byte {
	composeHash := sha256.Sum256([]byte(application.ComposeFile.String()))
	rolledBackAt := ""
	if application.RolledBackAt != nil {
		rolledBackAt = application.RolledBackAt.UTC().Format(time.RFC3339Nano)
	}
	fields := []string{
		issued,
		c.user.Id,
		application.Id,
		// The name is the compose project on the agent.
		application.Name.String(),
		application.AgentId,
		strconv.FormatBool(application.DeleteOldImages),
		application.Commit,
		base64.RawURLEncoding.EncodeToString(composeHash[:]),
		deref(application.CurrentDeploymentId),
		rolledBackAt,
	}

	mac := hmac.New(sha256.New, []byte(previewKey))
	// Length-prefixed, as the name can contain any character, so no fields can
	// be shifted into each other.
	for _, field := range fields {
		mac.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return mac.Sum(nil)
}
