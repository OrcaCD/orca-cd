package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"

	hubApplications "github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/githubactions"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"

	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const githubActionsIssuer = "https://token.actions.githubusercontent.com"

var (
	githubActionsAppURL string

	ghProviderMu sync.Mutex
	ghProvider   *gooidc.Provider
)

// SetGitHubActionsConfig stores the hub's app URL used as the OIDC audience.
func SetGitHubActionsConfig(url string) {
	githubActionsAppURL = url
}

// getGitHubActionsProvider returns the cached OIDC provider for GitHub Actions,
// initialising it on first use. Errors are not cached so transient network
// failures can be retried on the next request.
func getGitHubActionsProvider(ctx context.Context) (*gooidc.Provider, error) {
	ghProviderMu.Lock()
	defer ghProviderMu.Unlock()

	if ghProvider != nil {
		return ghProvider, nil
	}

	ctx = gooidc.ClientContext(ctx, httpclient.Default)
	p, err := gooidc.NewProvider(ctx, githubActionsIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover GitHub Actions OIDC provider: %w", err)
	}

	ghProvider = p
	return p, nil
}

type githubActionsRequest struct {
	SyncRepo      bool `json:"syncRepo" doc:"Sync the applications to the commit of the workflow run"`
	PullImages    bool `json:"pullImages" doc:"Pull the images of the applications and redeploy them"`
	WaitForResult bool `json:"waitForResult" doc:"The workflow waits for the result with the trigger status route, which then also reports applications whose agent isn't connected instead of answering 409"`
}

type githubActionsInput struct {
	Authorization string `header:"Authorization" doc:"Bearer followed by the OIDC token of the workflow run, with the URL of the hub as audience"`
	// Read as raw bytes, as the body is only parsed once the token was checked
	// and applications matched.
	RawBody []byte
}

type githubActionsOutput struct {
	Body struct {
		Message      string                      `json:"message" required:"true" example:"deployment triggered"`
		TriggerId    string                      `json:"triggerId,omitempty" doc:"The ID to get the result of the deployment from the trigger status route"`
		Applications []githubactions.Application `json:"applications,omitempty" doc:"The applications the deployment was triggered for"`
	}
}

func githubActionsResult(message, triggerID string, applications []githubactions.Application) *githubActionsOutput {
	out := &githubActionsOutput{}
	out.Body.Message = message
	out.Body.TriggerId = triggerID
	out.Body.Applications = applications
	return out
}

// https://docs.github.com/en/actions/concepts/security/openid-connect#understanding-the-oidc-token
type githubActionsClaims struct {
	Repository  string `json:"repository"`
	RunId       string `json:"run_id"`
	Ref         string `json:"ref"`
	RefType     string `json:"ref_type"`
	Sha         string `json:"sha"`
	EventName   string `json:"event_name"`
	Workflow    string `json:"workflow"`
	WorkflowRef string `json:"workflow_ref"`
	Environment string `json:"environment"`
}

// extractWorkflowFilename returns the workflow file name (e.g. "deploy.yml") from a
// workflow_ref claim of the form "owner/repo/.github/workflows/file.yml@ref", after
// verifying the owner/repo prefix matches the token's repository claim. Returns "" if
// the ref is missing, malformed, or refers to a different repository.
func extractWorkflowFilename(workflowRef, expectedRepo string) string {
	path, _, _ := strings.Cut(workflowRef, "@")
	prefix, file, found := strings.Cut(path, "/.github/workflows/")
	if !found || !strings.EqualFold(prefix, expectedRepo) {
		return ""
	}
	return file
}

// GitHubActionsDeployHandler verifies a GitHub Actions OIDC token and
// dispatches deployments for all applications linked to the matching OrcaCD
// repository and branch.
var GitHubActionsDeployHandler = webhookHandler(huma.Operation{
	OperationID: "deploy-from-github-actions",
	Method:      http.MethodPost,
	Path:        "/github-actions",
	Summary:     "Deploy from a GitHub Actions workflow",
	Description: "Called by a GitHub Actions workflow to sync or pull the images of the applications of the repository and branch it runs for. " +
		"It authenticates with the OIDC token of the run, whose audience must be the URL of the hub, and needs a repository with GitHub Actions OIDC enabled, " +
		"whose settings can limit the workflows, branches and actions. Pull request events are refused.",
	Tags:          []string{tagWebhooks},
	DefaultStatus: http.StatusAccepted,
	Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
}, &huma.RequestBody{
	Description: "Required once applications match the branch.",
	Content: map[string]*huma.MediaType{
		"application/json": {Schema: restapi.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[githubActionsRequest](), true, "")},
	},
}, func(ctx context.Context, input *githubActionsInput) (*githubActionsOutput, error) {
	claims, err := verifyGitHubActionsClaims(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}

	if claims.Repository == "" || claims.Ref == "" || claims.RunId == "" {
		return nil, huma.Error401Unauthorized("missing required token claims")
	}

	// Reject pull_request and pull_request_target events: these can be triggered
	// by untrusted fork contributors and should never trigger deployments.
	if claims.EventName == "pull_request" || claims.EventName == "pull_request_target" {
		return nil, huma.Error403Forbidden("deployments cannot be triggered by pull_request or pull_request_target events")
	}

	repos, err := gorm.G[models.Repository](db.DB).
		Where("github_actions_oidc_enabled = ? AND provider = ?", true, models.GitHub).
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	repoProvider, err := repositories.Get(models.GitHub)
	if err != nil {
		return nil, internalError()
	}

	var matchedRepo *models.Repository
	for i := range repos {
		owner, repo, err := repoProvider.ParseURL(repos[i].Url)
		if err != nil {
			continue
		}
		if strings.EqualFold(owner+"/"+repo, claims.Repository) {
			matchedRepo = &repos[i]
			break
		}
	}
	if matchedRepo == nil {
		return nil, huma.Error403Forbidden("no repository with GitHub Actions OIDC enabled matches this repository. Add the repository in OrcaCD and enable GitHub Actions OIDC in its sync settings")
	}

	if len(matchedRepo.GitHubActionsOIDCAllowedWorkflows) > 0 {
		workflowFile := extractWorkflowFilename(claims.WorkflowRef, claims.Repository)
		if workflowFile == "" || !slices.Contains(matchedRepo.GitHubActionsOIDCAllowedWorkflows, workflowFile) {
			return nil, huma.Error403Forbidden("workflow is not permitted by GitHub Actions OIDC settings for this repository")
		}
	}

	var branches []string
	switch claims.RefType {
	case "branch":
		branches = []string{strings.TrimPrefix(claims.Ref, "refs/heads/")}
	case "tag":
		branchResolver, ok := repoProvider.(repositories.CommitBranchResolver)
		if !ok {
			return nil, huma.Error422UnprocessableEntity("tag refs are not supported for this repository provider")
		}
		resolved, err := branchResolver.GetBranchesForCommit(ctx, matchedRepo, claims.Sha)
		if err != nil {
			return nil, internalError()
		}
		branches = resolved
	default:
		return nil, huma.Error422UnprocessableEntity("unsupported ref type")
	}

	if len(matchedRepo.GitHubActionsOIDCAllowedBranches) > 0 {
		branches = slices.DeleteFunc(branches, func(b string) bool {
			return !slices.Contains(matchedRepo.GitHubActionsOIDCAllowedBranches, b)
		})
		if len(branches) == 0 {
			return nil, huma.Error403Forbidden("branch is not permitted by GitHub Actions OIDC settings for this repository")
		}
	}

	var apps []models.Application
	for _, branch := range branches {
		branchApps, err := hubApplications.GetMatchingApplications(ctx, matchedRepo, branch)
		if err != nil {
			return nil, internalError()
		}
		apps = append(apps, branchApps...)
	}

	if len(apps) == 0 {
		// No applications configured for this branch — nothing to do.
		return githubActionsResult("no applications found for branch", "", nil), nil
	}

	// Decoded like Gin's ShouldBindJSON did, which ignores data after the object
	var req githubActionsRequest
	if err := json.NewDecoder(bytes.NewReader(input.RawBody)).Decode(&req); err != nil {
		return nil, huma.Error400BadRequest("invalid request body")
	}

	if !req.SyncRepo && !req.PullImages {
		return nil, huma.Error400BadRequest("at least one of syncRepo or pullImages must be true")
	}

	if req.SyncRepo && !matchedRepo.GitHubActionsOIDCAllowRepoSync {
		return nil, huma.Error403Forbidden("repository sync is not permitted by GitHub Actions OIDC settings for this repository")
	}
	if req.PullImages && !matchedRepo.GitHubActionsOIDCAllowImageSync {
		return nil, huma.Error403Forbidden("image sync is not permitted by GitHub Actions OIDC settings for this repository")
	}

	if req.SyncRepo {
		if hubApplications.DefaultQueue == nil {
			return nil, huma.Error500InternalServerError("sync queue not initialized")
		}

		release, ok := tryLockRepositorySync(matchedRepo.Id)
		if !ok {
			return nil, huma.Error409Conflict("repository sync already in progress")
		}
		defer release()
	}

	trigger := models.GitHubActionsTrigger{
		RepositoryId:   matchedRepo.Id,
		Repository:     strings.ToLower(claims.Repository),
		RunId:          claims.RunId,
		SyncRepo:       req.SyncRepo,
		PullImages:     req.PullImages,
		ApplicationIds: make([]string, 0, len(apps)),
	}
	targets := make([]githubactions.Application, 0, len(apps))
	for i := range apps {
		app := &apps[i]
		trigger.ApplicationIds = append(trigger.ApplicationIds, app.Id)
		targets = append(targets, githubactions.Application{Id: app.Id, Name: app.Name.String()})
	}
	if err := gorm.G[models.GitHubActionsTrigger](db.DB).Create(ctx, &trigger); err != nil {
		hubApplications.Log.Error().Err(err).Msg("failed to record GitHub Actions trigger")
		return nil, internalError()
	}

	origin := hubApplications.SyncOrigin{Source: models.ApplicationEventSourceGitHubActions, TriggerID: &trigger.Id}
	if req.SyncRepo {
		hubApplications.SyncApplications(ctx, matchedRepo, repoProvider, apps, hubApplications.StaticCommit(claims.Sha, ""), origin, &hubApplications.Log)
	}

	// Todo
	// This runs before the sync, refactor so the queue can also handle image pulling
	// (even if compose file did not change)
	if req.PullImages {
		for i := range apps {
			// Waiting workflows receive dispatch failures in their result and
			// still wait for every other application the trigger covers.
			if !hubApplications.TriggerImagePull(&apps[i], origin) && !req.WaitForResult {
				return nil, huma.Error409Conflict("agent is not connected")
			}
		}
	}

	return githubActionsResult("deployment triggered", trigger.Id, targets), nil
})

// verifyGitHubActionsClaims verifies the OIDC token of a workflow run, sent as
// "Bearer <token>", and returns its claims.
func verifyGitHubActionsClaims(ctx context.Context, authorization string) (githubActionsClaims, error) {
	c := restapi.GinContext(ctx)
	rawToken := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if rawToken == "" {
		seclog.Warn(c, seclog.WebhookAuthFailed, "reason", "missing_token")
		return githubActionsClaims{}, huma.Error401Unauthorized("missing bearer token")
	}

	oidcProvider, err := getGitHubActionsProvider(ctx)
	if err != nil {
		return githubActionsClaims{}, internalError()
	}

	verifier := oidcProvider.Verifier(&gooidc.Config{ClientID: githubActionsAppURL})
	idToken, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		seclog.Warn(c, seclog.WebhookAuthFailed, "reason", "invalid_token")
		return githubActionsClaims{}, huma.Error401Unauthorized("invalid token")
	}

	var claims githubActionsClaims
	if err := idToken.Claims(&claims); err != nil {
		return githubActionsClaims{}, huma.Error401Unauthorized("invalid token claims")
	}

	return claims, nil
}
