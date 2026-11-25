package routes

import (
	"context"
	"errors"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/githubactions"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

// GitHubActionsTriggerStatusHandler returns the result to the originating workflow run.
var GitHubActionsTriggerStatusHandler = restapi.Handler(huma.Operation{
	OperationID: "get-github-actions-trigger",
	Method:      http.MethodGet,
	Path:        "/github-actions/triggers/{id}",
	Summary:     "Get the result of a GitHub Actions deployment",
	Description: "Called by the workflow run that triggered a deployment to wait for its result. " +
		"It authenticates with the OIDC token of the same run, results are kept for 24 hours.",
	Tags: []string{tagWebhooks},
	// Authenticates with the OIDC token of the workflow run, not with a token or session
	Security: restapi.Public,
	Errors:   []int{http.StatusUnauthorized, http.StatusNotFound},
}, func(ctx context.Context, input *struct {
	ID            string `path:"id" doc:"The trigger ID the deployment returned"`
	Authorization string `header:"Authorization" doc:"Bearer followed by the OIDC token of the workflow run, with the URL of the hub as audience"`
}) (*struct{ Body githubactions.Result }, error) {
	// Set before any error, so no answer is cached
	restapi.GinContext(ctx).Header("Cache-Control", "no-store")
	claims, err := verifyGitHubActionsClaims(ctx, input.Authorization)
	if err != nil {
		return nil, err
	}
	if claims.Repository == "" || claims.RunId == "" {
		return nil, huma.Error404NotFound("trigger not found")
	}

	result, err := githubactions.GetResult(ctx, input.ID, claims.Repository, claims.RunId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, huma.Error404NotFound("trigger not found")
	}
	if err != nil {
		return nil, internalError()
	}
	return &struct{ Body githubactions.Result }{Body: result}, nil
})
