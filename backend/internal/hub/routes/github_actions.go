package routes

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	hubApplications "github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
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
	SyncRepo   bool `json:"syncRepo"`
	PullImages bool `json:"pullImages"`
}

// https://docs.github.com/en/actions/concepts/security/openid-connect#understanding-the-oidc-token
type githubActionsClaims struct {
	Repository  string `json:"repository"`
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

// GitHubActionsDeployHandler handles POST /api/v1/github-actions.
// It verifies a GitHub Actions OIDC token and dispatches deployments for all
// applications linked to the matching OrcaCD repository and branch.
func GitHubActionsDeployHandler(c *gin.Context) {
	ctx := c.Request.Context()

	rawToken := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}

	oidcProvider, err := getGitHubActionsProvider(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	verifier := oidcProvider.Verifier(&gooidc.Config{ClientID: githubActionsAppURL})
	idToken, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	var claims githubActionsClaims
	if err := idToken.Claims(&claims); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token claims"})
		return
	}

	if claims.Repository == "" || claims.Ref == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing required token claims"})
		return
	}

	// Reject pull_request and pull_request_target events: these can be triggered
	// by untrusted fork contributors and should never trigger deployments.
	if claims.EventName == "pull_request" || claims.EventName == "pull_request_target" {
		c.JSON(http.StatusForbidden, gin.H{"error": "deployments cannot be triggered by pull_request or pull_request_target events"})
		return
	}

	repos, err := gorm.G[models.Repository](db.DB).
		Where("github_actions_oidc_enabled = ? AND provider = ?", true, models.GitHub).
		Find(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	repoProvider, err := repositories.Get(models.GitHub)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
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
		c.JSON(http.StatusForbidden, gin.H{"error": "no repository with GitHub Actions OIDC enabled matches this repository. Add the repository in OrcaCD and enable GitHub Actions OIDC in its sync settings"})
		return
	}

	if len(matchedRepo.GitHubActionsOIDCAllowedWorkflows) > 0 {
		workflowFile := extractWorkflowFilename(claims.WorkflowRef, claims.Repository)
		if workflowFile == "" || !slices.Contains(matchedRepo.GitHubActionsOIDCAllowedWorkflows, workflowFile) {
			c.JSON(http.StatusForbidden, gin.H{"error": "workflow is not permitted by GitHub Actions OIDC settings for this repository"})
			return
		}
	}

	var branches []string
	switch claims.RefType {
	case "branch":
		branches = []string{strings.TrimPrefix(claims.Ref, "refs/heads/")}
	case "tag":
		branchResolver, ok := repoProvider.(repositories.CommitBranchResolver)
		if !ok {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "tag refs are not supported for this repository provider"})
			return
		}
		resolved, err := branchResolver.GetBranchesForCommit(ctx, matchedRepo, claims.Sha)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		branches = resolved
	default:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "unsupported ref type"})
		return
	}

	if len(matchedRepo.GitHubActionsOIDCAllowedBranches) > 0 {
		branches = slices.DeleteFunc(branches, func(b string) bool {
			return !slices.Contains(matchedRepo.GitHubActionsOIDCAllowedBranches, b)
		})
		if len(branches) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": "branch is not permitted by GitHub Actions OIDC settings for this repository"})
			return
		}
	}

	var apps []models.Application
	for _, branch := range branches {
		branchApps, err := hubApplications.GetMatchingApplications(ctx, matchedRepo, branch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		apps = append(apps, branchApps...)
	}

	if len(apps) == 0 {
		// No applications configured for this branch — nothing to do.
		c.JSON(http.StatusAccepted, gin.H{"message": "no applications found for branch"})
		return
	}

	var req githubActionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if !req.SyncRepo && !req.PullImages {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one of syncRepo or pullImages must be true"})
		return
	}

	if req.SyncRepo && !matchedRepo.GitHubActionsOIDCAllowRepoSync {
		c.JSON(http.StatusForbidden, gin.H{"error": "repository sync is not permitted by GitHub Actions OIDC settings for this repository"})
		return
	}
	if req.PullImages && !matchedRepo.GitHubActionsOIDCAllowImageSync {
		c.JSON(http.StatusForbidden, gin.H{"error": "image sync is not permitted by GitHub Actions OIDC settings for this repository"})
		return
	}

	if req.SyncRepo {
		if hubApplications.DefaultQueue == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "sync queue not initialized"})
			return
		}

		release, ok := tryLockRepositorySync(matchedRepo.Id)
		if !ok {
			c.JSON(http.StatusConflict, gin.H{"error": "repository sync already in progress"})
			return
		}
		defer release()

		hubApplications.SyncApplications(ctx, matchedRepo, repoProvider, apps, hubApplications.StaticCommit(claims.Sha, ""), hubApplications.SyncOrigin{Source: models.ApplicationEventSourceGitHubActions}, &hubApplications.Log)
	}

	// Todo
	// This runs before the sync, refactor so the queue can also handle image pulling
	// (even if compose file did not change)
	if req.PullImages {
		for i := range apps {
			if !hubApplications.TriggerImagePull(&apps[i], models.ApplicationEventSourceGitHubActions) {
				c.JSON(http.StatusConflict, gin.H{"error": "agent is not connected"})
				return
			}
		}
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "deployment triggered"})
}
