package routes

import (
	"context"
	"fmt"
	"net/http"
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
	Environment string `json:"environment"`
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
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
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
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return
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

	if req.SyncRepo {
		if hubApplications.DefaultQueue == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "sync queue not initialized"})
			return
		}

		// The workflow run already carries the deployed commit, so resolve it
		// statically. Routing through SyncApplications keeps the repository's
		// sync-status bookkeeping identical to the polling and webhook paths.
		hubApplications.SyncApplications(ctx, matchedRepo, repoProvider, apps, hubApplications.StaticCommit(claims.Sha, ""), hubApplications.QueueLogger())
	}

	// Todo
	// This runs before the sync, refactor so the queue can also handle image pulling
	// (even if compose file did not change)
	if req.PullImages {
		for i := range apps {
			if !hubApplications.TriggerImagePull(&apps[i]) {
				c.JSON(http.StatusConflict, gin.H{"error": "agent is not connected"})
				return
			}
		}
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "deployment triggered"})
}
