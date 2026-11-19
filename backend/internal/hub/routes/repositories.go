package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/commitsigning"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"gorm.io/gorm"
)

const (
	DefaultPollingIntervalSeconds int64 = 60
	MinPollingIntervalSeconds     int64 = 30

	maxOIDCListItems  = 50
	maxOIDCItemLength = 255
)

var appUrl string

func SetRepositoriesConfig(url string) {
	appUrl = url
}

// The values of provider, authMethod and syncType are only documented, not
// enums: the handlers check them and answer with their own messages.
type createRepositoryRequest struct {
	Url                               string                      `json:"url" required:"true" minLength:"1"`
	Provider                          models.RepositoryProvider   `json:"provider" required:"true" minLength:"1" doc:"One of github, gitlab, generic, bitbucket, azure_devops or gitea"`
	AuthMethod                        models.RepositoryAuthMethod `json:"authMethod" required:"true" minLength:"1" doc:"One of none, token, basic or ssh, which the provider must support"`
	AuthUser                          *string                     `json:"authUser"`
	AuthToken                         *string                     `json:"authToken"`
	SyncType                          models.RepositorySyncType   `json:"syncType" required:"true" minLength:"1" doc:"One of polling, webhook or manual"`
	PollingInterval                   *int64                      `json:"pollingIntervalSeconds" doc:"At least 30. Polling defaults to 60."`
	GitHubActionsOIDCEnabled          bool                        `json:"githubActionsOIDCEnabled" doc:"Lets GitHub Actions of the repository trigger syncs with an OIDC token"`
	GitHubActionsOIDCAllowRepoSync    *bool                       `json:"githubActionsOIDCAllowRepoSync" doc:"Defaults to true"`
	GitHubActionsOIDCAllowImageSync   *bool                       `json:"githubActionsOIDCAllowImageSync" doc:"Defaults to true"`
	GitHubActionsOIDCAllowedBranches  []string                    `json:"githubActionsOIDCAllowedBranches" doc:"Branches the workflow may run on. Empty allows any."`
	GitHubActionsOIDCAllowedWorkflows []string                    `json:"githubActionsOIDCAllowedWorkflows" doc:"Workflow file names (.yml or .yaml) that may trigger a sync. Empty allows any."`
}

type repositoryResponse struct {
	Id                                string   `json:"id" required:"true"`
	Name                              string   `json:"name" required:"true" doc:"owner/name, taken from the URL"`
	Url                               string   `json:"url" required:"true"`
	Provider                          string   `json:"provider" required:"true"`
	AuthMethod                        string   `json:"authMethod" required:"true"`
	SyncType                          string   `json:"syncType" required:"true"`
	SyncStatus                        string   `json:"syncStatus" required:"true" enum:"unknown,syncing,failed,success"`
	LastSyncError                     *string  `json:"lastSyncError" required:"true" nullable:"true"`
	PollingIntervalSeconds            *int64   `json:"pollingIntervalSeconds" required:"true" nullable:"true"`
	LastSyncedAt                      *string  `json:"lastSyncedAt" required:"true" nullable:"true" format:"date-time"`
	CreatedBy                         string   `json:"createdBy" required:"true" doc:"The ID of the user who added the repository"`
	CreatedAt                         string   `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt                         string   `json:"updatedAt" required:"true" format:"date-time"`
	WebhookSecret                     *string  `json:"webhookSecret,omitempty" doc:"Only returned once, when webhook sync is turned on"`
	WebhookUrl                        *string  `json:"webhookUrl,omitempty" doc:"Only returned together with webhookSecret"`
	AppCount                          int      `json:"appCount" required:"true" doc:"Number of applications deployed from the repository"`
	GitHubActionsOIDCEnabled          bool     `json:"githubActionsOIDCEnabled" required:"true"`
	GitHubActionsOIDCAllowRepoSync    bool     `json:"githubActionsOIDCAllowRepoSync" required:"true"`
	GitHubActionsOIDCAllowImageSync   bool     `json:"githubActionsOIDCAllowImageSync" required:"true"`
	GitHubActionsOIDCAllowedBranches  []string `json:"githubActionsOIDCAllowedBranches" required:"true"`
	GitHubActionsOIDCAllowedWorkflows []string `json:"githubActionsOIDCAllowedWorkflows" required:"true"`
	CommitSigningSupported            bool     `json:"commitSigningSupported" required:"true" doc:"Whether the provider can verify commit signatures"`
	RequireSignedCommits              bool     `json:"requireSignedCommits" required:"true"`
	// Only set by the single repository endpoints, the list does not need the keys.
	CommitSigningKeys []commitSigningKeyResponse `json:"commitSigningKeys,omitempty" doc:"The trusted signing keys. Not returned by the list."`
}

type commitSigningKeyResponse struct {
	Type        commitsigning.KeyType `json:"type" required:"true" enum:"pgp,ssh"`
	Fingerprint string                `json:"fingerprint" required:"true"`
	Identity    string                `json:"identity" required:"true"`
	Key         string                `json:"key" required:"true"`
}

type repositoryOutput struct {
	Body repositoryResponse
}

// repositoryProviderError is a 422 answer for a request the Git provider
// rejected. huma.NewError turns 422 into 400, as Huma uses it for invalid input.
type repositoryProviderError struct {
	Message string `json:"error"`
}

func (e *repositoryProviderError) Error() string {
	return e.Message
}

func (e *repositoryProviderError) GetStatus() int {
	return http.StatusUnprocessableEntity
}

// normalizeOIDCStringList trims, drops empty entries, and deduplicates the given list
// while preserving order. Returns an error if any entry or the list itself exceeds the
// configured size limits.
func normalizeOIDCStringList(items []string) ([]string, error) {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > maxOIDCItemLength {
			return nil, fmt.Errorf("entry exceeds maximum length of %d characters", maxOIDCItemLength)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) > maxOIDCListItems {
		return nil, fmt.Errorf("list exceeds maximum of %d entries", maxOIDCListItems)
	}
	return result, nil
}

// normalizeOIDCAllowedWorkflows normalizes the given list like normalizeOIDCStringList and
// additionally requires every entry to end with .yml or .yaml
func normalizeOIDCAllowedWorkflows(items []string) ([]string, error) {
	normalized, err := normalizeOIDCStringList(items)
	if err != nil {
		return nil, err
	}
	for _, item := range normalized {
		lower := strings.ToLower(item)
		if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
			return nil, fmt.Errorf("workflow file name %q must end with .yml or .yaml", item)
		}
	}
	return normalized, nil
}

// validateCommitSigning checks the trusted signing keys and that the provider
// can verify signatures when signed commits are required.
func validateCommitSigning(repo *models.Repository) string {
	keys, err := commitsigning.ParseTrustedKeys(repo.CommitSigningKeys)
	if err != nil {
		return fmt.Sprintf("invalid commit signing keys: %v", err)
	}
	if !repo.RequireSignedCommits {
		return ""
	}
	provider, err := repositories.Get(repo.Provider)
	if err != nil || !repositories.SupportsCommitSignatures(provider) {
		return "commit signature verification is not supported for this provider"
	}
	if keys.Len() == 0 {
		return "at least one trusted signing key is required when signed commits are required"
	}
	return ""
}

// toRepositoryDetailResponse adds the trusted signing keys, which are parsed
// for display and therefore left out of the repository list.
func toRepositoryDetailResponse(r *models.Repository, includeWebhook bool, appCount int) repositoryResponse {
	resp := toRepositoryResponse(r, includeWebhook, appCount)
	resp.CommitSigningKeys = []commitSigningKeyResponse{}
	// Stored keys were validated on save; a parse failure leaves the list empty.
	if keys, err := commitsigning.ParseTrustedKeys(r.CommitSigningKeys); err == nil {
		for i, signer := range keys.Signers() {
			resp.CommitSigningKeys = append(resp.CommitSigningKeys, commitSigningKeyResponse{
				Type:        signer.Type,
				Fingerprint: signer.Fingerprint,
				Identity:    signer.Identity,
				Key:         r.CommitSigningKeys[i],
			})
		}
	}
	return resp
}

func toRepositoryResponse(r *models.Repository, includeWebhook bool, appCount int) repositoryResponse {
	allowedBranches := r.GitHubActionsOIDCAllowedBranches
	if allowedBranches == nil {
		allowedBranches = []string{}
	}
	allowedWorkflows := r.GitHubActionsOIDCAllowedWorkflows
	if allowedWorkflows == nil {
		allowedWorkflows = []string{}
	}

	provider, err := repositories.Get(r.Provider)
	commitSigningSupported := err == nil && repositories.SupportsCommitSignatures(provider)

	resp := repositoryResponse{
		Id:                                r.Id,
		Name:                              r.Name,
		Url:                               r.Url,
		Provider:                          string(r.Provider),
		AuthMethod:                        string(r.AuthMethod),
		SyncType:                          string(r.SyncType),
		SyncStatus:                        string(r.SyncStatus),
		LastSyncError:                     r.LastSyncError,
		CreatedBy:                         r.CreatedBy,
		CreatedAt:                         r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                         r.UpdatedAt.Format(time.RFC3339),
		AppCount:                          appCount,
		GitHubActionsOIDCEnabled:          r.GitHubActionsOIDCEnabled,
		GitHubActionsOIDCAllowRepoSync:    r.GitHubActionsOIDCAllowRepoSync,
		GitHubActionsOIDCAllowImageSync:   r.GitHubActionsOIDCAllowImageSync,
		GitHubActionsOIDCAllowedBranches:  allowedBranches,
		GitHubActionsOIDCAllowedWorkflows: allowedWorkflows,
		CommitSigningSupported:            commitSigningSupported,
		RequireSignedCommits:              r.RequireSignedCommits,
	}

	if r.PollingInterval != nil {
		secs := int64(*r.PollingInterval / time.Second)
		resp.PollingIntervalSeconds = &secs
	}

	if r.LastSyncedAt != nil {
		s := r.LastSyncedAt.Format(time.RFC3339)
		resp.LastSyncedAt = &s
	}

	if includeWebhook && r.WebhookSecret != nil {
		s := r.WebhookSecret.String()
		resp.WebhookSecret = &s

		webhookUrl := fmt.Sprintf("%s/api/v1/webhooks/repositories/%s", appUrl, r.Id)
		resp.WebhookUrl = &webhookUrl
	}

	return resp
}

type appCountByRepositoryRow struct {
	RepositoryId string `gorm:"column:repository_id"`
	AppCount     int64  `gorm:"column:app_count"`
}

func countApplicationsByRepositoryID(ctx context.Context, repositoryId string) (int, error) {
	count, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", repositoryId).Count(ctx, "*")
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func countApplicationsByRepositoryIDs(ctx context.Context, repositoryIds []string) (map[string]int, error) {
	counts := make(map[string]int, len(repositoryIds))
	if len(repositoryIds) == 0 {
		return counts, nil
	}

	rows := []appCountByRepositoryRow{}
	if err := db.DB.WithContext(ctx).
		Model(&models.Application{}).
		Select("repository_id, COUNT(*) AS app_count").
		Where("repository_id IN ?", repositoryIds).
		Group("repository_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	for i := range rows {
		counts[rows[i].RepositoryId] = int(rows[i].AppCount)
	}

	return counts, nil
}

// normalizeRepositoryURL strips surrounding whitespace, trailing slashes and a ".git"
// suffix so the same repository is always stored (and deduplicated) under one URL.
func normalizeRepositoryURL(rawURL string) string {
	u := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	u = strings.TrimSuffix(u, ".git")

	return strings.TrimRight(u, "/")
}

var ListRepositoriesHandler = restapi.Handler(huma.Operation{
	OperationID: "list-repositories",
	Method:      http.MethodGet,
	Path:        "/repositories",
	Summary:     "List repositories",
	Description: "Lists the repositories the user may view, without their trusted signing keys.",
	Tags:        []string{tagRepositories},
	Security:    restapi.Authenticated,
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []repositoryResponse }, error) {
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}

	repos, err := gorm.G[models.Repository](db.DB).
		Scopes(perms.Scope(models.ResourceRepository, models.ActionRead, "id")).
		Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	repositoryIds := make([]string, 0, len(repos))
	for i := range repos {
		repositoryIds = append(repositoryIds, repos[i].Id)
	}

	appCountsByRepositoryId, err := countApplicationsByRepositoryIDs(ctx, repositoryIds)
	if err != nil {
		return nil, internalError()
	}

	result := make([]repositoryResponse, 0, len(repos))
	for i := range repos {
		result = append(result, toRepositoryResponse(&repos[i], false, appCountsByRepositoryId[repos[i].Id]))
	}

	return &struct{ Body []repositoryResponse }{Body: result}, nil
})

var CreateRepositoryHandler = restapi.Handler(huma.Operation{
	OperationID:   "create-repository",
	Method:        http.MethodPost,
	Path:          "/repositories",
	DefaultStatus: http.StatusCreated,
	Summary:       "Add a repository",
	Description: "With webhook sync, the response contains the webhook secret and URL, which are only shown once. " +
		requires(models.ResourceRepository, models.ActionCreate),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusConflict},
}, func(ctx context.Context, input *struct{ Body createRepositoryRequest }) (*repositoryOutput, error) {
	c := restapi.GinContext(ctx)
	user, ok := auth.GetUser(c)
	if !ok {
		return nil, huma.Error401Unauthorized("missing authentication")
	}

	req := input.Body

	// Validate syncType value
	switch req.SyncType {
	case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
	default:
		return nil, huma.Error400BadRequest("invalid syncType: must be polling, webhook, or manual")
	}

	if req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
		// Default to 60 seconds if polling is selected but no interval is provided
		var defaultInterval = DefaultPollingIntervalSeconds
		req.PollingInterval = &defaultInterval
	}

	if req.SyncType == models.SyncTypePolling && req.PollingInterval != nil && *req.PollingInterval < MinPollingIntervalSeconds {
		return nil, huma.Error400BadRequest(fmt.Sprintf("pollingIntervalSeconds must be at least %d", MinPollingIntervalSeconds))
	}

	provider, err := resolveProvider(req.Provider, req.AuthMethod)
	if err != nil {
		return nil, err
	}

	repoOwner, repoName, err := provider.ParseURL(req.Url)
	if err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("invalid repository URL: %v", err))
	}

	allowRepoSync := true
	if req.GitHubActionsOIDCAllowRepoSync != nil {
		allowRepoSync = *req.GitHubActionsOIDCAllowRepoSync
	}
	allowImageSync := true
	if req.GitHubActionsOIDCAllowImageSync != nil {
		allowImageSync = *req.GitHubActionsOIDCAllowImageSync
	}

	if req.GitHubActionsOIDCEnabled && !allowRepoSync && !allowImageSync {
		return nil, huma.Error400BadRequest("at least one of allow repo sync or allow image sync must be true when GitHub Actions OIDC is enabled")
	}

	allowedBranches, err := normalizeOIDCStringList(req.GitHubActionsOIDCAllowedBranches)
	if err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("invalid Github Actions OIDC allowed branches: %v", err))
	}
	allowedWorkflows, err := normalizeOIDCAllowedWorkflows(req.GitHubActionsOIDCAllowedWorkflows)
	if err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("invalid Github Actions OIDC allowed workflows: %v", err))
	}

	repo := models.Repository{
		Name:                              fmt.Sprintf("%s/%s", repoOwner, repoName),
		Url:                               normalizeRepositoryURL(req.Url),
		Provider:                          req.Provider,
		AuthMethod:                        req.AuthMethod,
		SyncType:                          req.SyncType,
		SyncStatus:                        models.SyncStatusUnknown,
		CreatedBy:                         user.Id,
		GitHubActionsOIDCEnabled:          req.GitHubActionsOIDCEnabled,
		GitHubActionsOIDCAllowRepoSync:    allowRepoSync,
		GitHubActionsOIDCAllowImageSync:   allowImageSync,
		GitHubActionsOIDCAllowedBranches:  allowedBranches,
		GitHubActionsOIDCAllowedWorkflows: allowedWorkflows,
		CommitSigningKeys:                 []string{},
	}

	if req.AuthUser != nil && *req.AuthUser != "" {
		enc := crypto.EncryptedString(*req.AuthUser)
		repo.AuthUser = &enc
	}

	if req.AuthToken != nil && *req.AuthToken != "" {
		enc := crypto.EncryptedString(*req.AuthToken)
		repo.AuthToken = &enc
	}

	if req.SyncType == models.SyncTypeWebhook {
		secret, err := auth.GenerateRandomString(32)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to generate webhook secret")
		}
		enc := crypto.EncryptedString(secret)
		repo.WebhookSecret = &enc
	}

	if req.PollingInterval != nil {
		d := time.Duration(*req.PollingInterval) * time.Second
		repo.PollingInterval = &d
	}

	if err := db.DB.WithContext(ctx).Select("*").Create(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("repository already exists")
		}
		return nil, internalError()
	}

	appCount, err := countApplicationsByRepositoryID(ctx, repo.Id)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(c, "created", "repository", repo.Id)
	sse.PublishUpdate(sse.TopicRepositories.For(repo.Id))

	return &repositoryOutput{Body: toRepositoryResponse(&repo, true, appCount)}, nil
})

type testConnectionRequest struct {
	Url        string                      `json:"url" required:"true" minLength:"1"`
	Provider   models.RepositoryProvider   `json:"provider" required:"true" minLength:"1" doc:"One of github, gitlab, generic, bitbucket, azure_devops or gitea"`
	AuthMethod models.RepositoryAuthMethod `json:"authMethod" required:"true" minLength:"1" doc:"One of none, token, basic or ssh, which the provider must support"`
	AuthUser   *string                     `json:"authUser"`
	AuthToken  *string                     `json:"authToken"`
}

var TestConnectionHandler = restapi.Handler(huma.Operation{
	OperationID: "test-repository-connection",
	Method:      http.MethodPost,
	Path:        "/repositories/test-connection",
	Summary:     "Test the connection to a repository",
	Description: "Checks that the hub can access the repository with the given credentials, without saving anything. " +
		"Answers 422 with the provider's error when it can't. " +
		"Needs the `repository:create` permission, or `repository:update` on any repository.",
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *struct{ Body testConnectionRequest }) (*restapi.MessageOutput, error) {
	// Testing a connection is part of adding a repository or changing its credentials.
	perms, err := permissionsFor(restapi.GinContext(ctx))
	if err != nil {
		return nil, err
	}
	if !perms.CanAll(models.ResourceRepository, models.ActionCreate) && !perms.CanAny(models.ResourceRepository, models.ActionUpdate) {
		return nil, huma.Error403Forbidden("insufficient permissions")
	}

	req := input.Body

	provider, err := resolveProvider(req.Provider, req.AuthMethod)
	if err != nil {
		return nil, err
	}

	if _, _, err := provider.ParseURL(req.Url); err != nil {
		return nil, huma.Error400BadRequest(fmt.Sprintf("invalid repository URL: %v", err))
	}

	repo := &models.Repository{
		Url:        req.Url,
		Provider:   req.Provider,
		AuthMethod: req.AuthMethod,
	}

	if req.AuthUser != nil && *req.AuthUser != "" {
		enc := crypto.EncryptedString(*req.AuthUser)
		repo.AuthUser = &enc
	}

	if req.AuthToken != nil && *req.AuthToken != "" {
		enc := crypto.EncryptedString(*req.AuthToken)
		repo.AuthToken = &enc
	}

	if err := provider.TestConnection(ctx, repo); err != nil {
		return nil, &repositoryProviderError{Message: err.Error()}
	}

	return restapi.Message("connection successful"), nil
})

var GetRepositoryHandler = restapi.Handler(huma.Operation{
	OperationID: "get-repository",
	Method:      http.MethodGet,
	Path:        "/repositories/{id}",
	Summary:     "Get a repository",
	Description: "Includes the trusted signing keys. " + requires(models.ResourceRepository, models.ActionRead),
	Tags:        []string{tagRepositories},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound},
}, func(ctx context.Context, input *idInput) (*repositoryOutput, error) {
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("repository not found")
		}
		return nil, internalError()
	}

	appCount, err := countApplicationsByRepositoryID(ctx, repo.Id)
	if err != nil {
		return nil, internalError()
	}

	return &repositoryOutput{Body: toRepositoryDetailResponse(&repo, false, appCount)}, nil
})

var ListRepositoryBranchesHandler = restapi.Handler(huma.Operation{
	OperationID: "list-repository-branches",
	Method:      http.MethodGet,
	Path:        "/repositories/{id}/branches",
	Summary:     "List the branches of a repository",
	Description: "Asks the Git provider, which answers 422 with the provider's error when it fails. " +
		requires(models.ResourceRepository, models.ActionRead),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *idInput) (*struct{ Body []string }, error) {
	repo, provider, err := resolveRepository(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	branches, err := provider.ListBranches(ctx, &repo)
	if err != nil {
		return nil, &repositoryProviderError{Message: err.Error()}
	}

	return &struct{ Body []string }{Body: branches}, nil
})

var ListRepositoryTreeHandler = restapi.Handler(huma.Operation{
	OperationID: "list-repository-tree",
	Method:      http.MethodGet,
	Path:        "/repositories/{id}/tree",
	Summary:     "List the files of a repository",
	Description: "Lists the files and directories on a branch, from the Git provider, which answers 422 with the provider's error when it fails. " +
		requires(models.ResourceRepository, models.ActionRead),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity},
}, func(ctx context.Context, input *struct {
	ID string `path:"id" doc:"The ID of the repository"`
	// Checked by the handler, which answers with its own message
	Branch string `query:"branch" doc:"The branch to list. Required."`
}) (*struct{ Body []repositories.TreeEntry }, error) {
	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		return nil, huma.Error400BadRequest("branch query parameter is required")
	}

	repo, provider, err := resolveRepository(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	entries, err := provider.ListTree(ctx, &repo, branch)
	if err != nil {
		return nil, &repositoryProviderError{Message: err.Error()}
	}

	return &struct{ Body []repositories.TreeEntry }{Body: entries}, nil
})

var DeleteRepositoryHandler = restapi.Handler(huma.Operation{
	OperationID: "delete-repository",
	Method:      http.MethodDelete,
	Path:        "/repositories/{id}",
	Summary:     "Delete a repository",
	Description: "Removes its applications from their agents and deletes them too, so it also needs `application:delete` on each of them. " +
		"Stops at the first application its agent can't remove. " +
		requires(models.ResourceRepository, models.ActionDelete),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	id := input.ID
	if err := checkDeleteApplicationsOf(c, "repository_id", id); err != nil {
		return nil, err
	}

	// Applications cascade with the repository, so undeploy them first. Rows are
	// dropped one by one after agent confirmation to stay in sync on partial failure.
	apps, err := gorm.G[models.Application](db.DB).Where("repository_id = ?", id).Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	agentIDs := map[string]struct{}{}
	removedApps, removedRepo := false, false
	// Also runs when a later application fails to undeploy, for the ones removed before
	defer func() {
		if removedApps {
			sse.PublishUpdate(sse.TopicApplications)
		}
		// After the updates, so clients still get them before losing access
		if removedApps || removedRepo {
			pruneGrants(ctx)
		}
		for agentID := range agentIDs {
			sendAgentSettings(c, agentID)
		}
	}()

	for i := range apps {
		if status, msg := undeployApplication(ctx, &apps[i], "delete"); status != 0 {
			return nil, huma.NewError(status, fmt.Sprintf("failed to remove application %q: %s", apps[i].Name.String(), msg))
		}
		if _, err := gorm.G[models.Application](db.DB).Where("id = ?", apps[i].Id).Delete(ctx); err != nil {
			return nil, internalError()
		}
		removedApps = true
		agentIDs[apps[i].AgentId] = struct{}{}
		utils.RecordAuditLog(c, "deleted", "application", apps[i].Id)
	}

	result := db.DB.WithContext(ctx).Where("id = ?", id).Delete(&models.Repository{})
	if result.Error != nil {
		return nil, internalError()
	}
	if result.RowsAffected == 0 {
		return nil, huma.Error404NotFound("repository not found")
	}

	removedRepo = true
	utils.RecordAuditLog(c, "deleted", "repository", id)
	sse.PublishUpdate(sse.TopicRepositories)

	return restapi.Message("repository deleted"), nil
})

type updateRepositoryRequest struct {
	AuthMethod                        *models.RepositoryAuthMethod `json:"authMethod" doc:"One of none, token, basic or ssh. Replaces the credentials with authUser and authToken."`
	AuthUser                          *string                      `json:"authUser" doc:"Only used together with authMethod"`
	AuthToken                         *string                      `json:"authToken" doc:"Only used together with authMethod"`
	SyncType                          *models.RepositorySyncType   `json:"syncType" doc:"One of polling, webhook or manual. Switching to webhook returns a new webhook secret."`
	PollingInterval                   *int64                       `json:"pollingIntervalSeconds" doc:"Only used together with syncType. At least 30, polling defaults to 60."`
	GitHubActionsOIDCEnabled          *bool                        `json:"githubActionsOIDCEnabled"`
	GitHubActionsOIDCAllowRepoSync    *bool                        `json:"githubActionsOIDCAllowRepoSync"`
	GitHubActionsOIDCAllowImageSync   *bool                        `json:"githubActionsOIDCAllowImageSync"`
	GitHubActionsOIDCAllowedBranches  *[]string                    `json:"githubActionsOIDCAllowedBranches"`
	GitHubActionsOIDCAllowedWorkflows *[]string                    `json:"githubActionsOIDCAllowedWorkflows"`
	RequireSignedCommits              *bool                        `json:"requireSignedCommits" doc:"Needs a provider that can verify signatures and at least one trusted key"`
	CommitSigningKeys                 *[]string                    `json:"commitSigningKeys" doc:"Trusted PGP or SSH public keys. Replaces the stored keys."`
}

var UpdateRepositoryHandler = restapi.Handler(huma.Operation{
	OperationID: "update-repository",
	Method:      http.MethodPut,
	Path:        "/repositories/{id}",
	Summary:     "Update a repository",
	Description: "Only changes the fields that are given, at least one is needed. " +
		"Switching to webhook sync returns the new webhook secret and URL, which are only shown once. " +
		requires(models.ResourceRepository, models.ActionUpdate),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the repository"`
	Body updateRepositoryRequest
}) (*repositoryOutput, error) {
	id, req := input.ID, input.Body

	if req.AuthMethod == nil && req.SyncType == nil && req.PollingInterval == nil &&
		req.GitHubActionsOIDCEnabled == nil && req.GitHubActionsOIDCAllowRepoSync == nil &&
		req.GitHubActionsOIDCAllowImageSync == nil && req.GitHubActionsOIDCAllowedBranches == nil &&
		req.GitHubActionsOIDCAllowedWorkflows == nil && req.RequireSignedCommits == nil &&
		req.CommitSigningKeys == nil {
		return nil, huma.Error400BadRequest("at least one field must be provided")
	}

	if req.SyncType != nil {
		switch *req.SyncType {
		case models.SyncTypePolling, models.SyncTypeWebhook, models.SyncTypeManual:
		default:
			return nil, huma.Error400BadRequest("invalid syncType: must be polling, webhook, or manual")
		}

		if *req.SyncType == models.SyncTypePolling && req.PollingInterval == nil {
			var defaultInterval = DefaultPollingIntervalSeconds
			req.PollingInterval = &defaultInterval
		}

		if *req.SyncType == models.SyncTypePolling && req.PollingInterval != nil && *req.PollingInterval < MinPollingIntervalSeconds {
			return nil, huma.Error400BadRequest(fmt.Sprintf("pollingIntervalSeconds must be at least %d", MinPollingIntervalSeconds))
		}
	}

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("repository not found")
		}
		return nil, internalError()
	}

	if req.AuthMethod != nil {
		authMethod := *req.AuthMethod
		if _, err := resolveProvider(repo.Provider, authMethod); err != nil {
			return nil, err
		}

		repo.AuthMethod = authMethod

		if req.AuthUser != nil && *req.AuthUser != "" {
			enc := crypto.EncryptedString(*req.AuthUser)
			repo.AuthUser = &enc
		} else {
			repo.AuthUser = nil
		}

		if req.AuthToken != nil && *req.AuthToken != "" {
			enc := crypto.EncryptedString(*req.AuthToken)
			repo.AuthToken = &enc
		} else {
			repo.AuthToken = nil
		}
	}

	prevSyncType := repo.SyncType
	if req.SyncType != nil {
		repo.SyncType = *req.SyncType

		switch {
		case *req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook:
			secret, err := auth.GenerateRandomString(32)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to generate webhook secret")
			}
			enc := crypto.EncryptedString(secret)
			repo.WebhookSecret = &enc
		case *req.SyncType != models.SyncTypeWebhook:
			repo.WebhookSecret = nil
		}

		if req.PollingInterval != nil {
			d := time.Duration(*req.PollingInterval) * time.Second
			repo.PollingInterval = &d
		} else {
			repo.PollingInterval = nil
		}
	}

	if req.GitHubActionsOIDCEnabled != nil {
		repo.GitHubActionsOIDCEnabled = *req.GitHubActionsOIDCEnabled
	}

	if req.GitHubActionsOIDCAllowRepoSync != nil {
		repo.GitHubActionsOIDCAllowRepoSync = *req.GitHubActionsOIDCAllowRepoSync
	}
	if req.GitHubActionsOIDCAllowImageSync != nil {
		repo.GitHubActionsOIDCAllowImageSync = *req.GitHubActionsOIDCAllowImageSync
	}
	if req.GitHubActionsOIDCAllowedBranches != nil {
		normalized, err := normalizeOIDCStringList(*req.GitHubActionsOIDCAllowedBranches)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid Github Actions OIDC allowed branches: %v", err))
		}
		repo.GitHubActionsOIDCAllowedBranches = normalized
	}
	if req.GitHubActionsOIDCAllowedWorkflows != nil {
		normalized, err := normalizeOIDCAllowedWorkflows(*req.GitHubActionsOIDCAllowedWorkflows)
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("invalid Github Actions OIDC workflows: %v", err))
		}
		repo.GitHubActionsOIDCAllowedWorkflows = normalized
	}

	if repo.GitHubActionsOIDCEnabled && !repo.GitHubActionsOIDCAllowRepoSync && !repo.GitHubActionsOIDCAllowImageSync {
		return nil, huma.Error400BadRequest("at least one of repo sync or image sync must be true when GitHub Actions OIDC is enabled")
	}

	if req.RequireSignedCommits != nil {
		repo.RequireSignedCommits = *req.RequireSignedCommits
	}
	if req.CommitSigningKeys != nil {
		repo.CommitSigningKeys = make([]string, len(*req.CommitSigningKeys))
		for i, key := range *req.CommitSigningKeys {
			repo.CommitSigningKeys[i] = strings.TrimSpace(key)
		}
	}
	if errMsg := validateCommitSigning(&repo); errMsg != "" {
		return nil, huma.Error400BadRequest(errMsg)
	}

	if err := db.DB.WithContext(ctx).Save(&repo).Error; err != nil {
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("repository already exists")
		}
		return nil, internalError()
	}

	appCount, err := countApplicationsByRepositoryID(ctx, repo.Id)
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "updated", "repository", id)
	sse.PublishUpdate(sse.TopicRepositories.For(id))

	newWebhookSecret := req.SyncType != nil && *req.SyncType == models.SyncTypeWebhook && prevSyncType != models.SyncTypeWebhook
	return &repositoryOutput{Body: toRepositoryDetailResponse(&repo, newWebhookSecret, appCount)}, nil
})

var SyncRepositoryHandler = restapi.Handler(huma.Operation{
	OperationID:   "sync-repository",
	Method:        http.MethodPost,
	Path:          "/repositories/{id}/sync",
	DefaultStatus: http.StatusAccepted,
	Summary:       "Sync a repository",
	Description: "Starts a sync in the background. It redeploys the applications whose files changed, " +
		"so it also needs `application:deploy` on each application of the repository. " +
		requires(models.ResourceRepository, models.ActionSync),
	Tags:     []string{tagRepositories},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	c := restapi.GinContext(ctx)
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("repository not found")
		}
		return nil, internalError()
	}

	if _, err := repositories.Get(repo.Provider); err != nil {
		return nil, huma.Error400BadRequest("unsupported provider")
	}

	// A sync redeploys every application of the repository that changed
	if err := checkApplicationsOf(c, models.ActionDeploy, "repository_id", repo.Id, "insufficient permissions to deploy its applications"); err != nil {
		return nil, err
	}

	if applications.DefaultPoller == nil {
		return nil, huma.Error500InternalServerError("application poller not initialized")
	}

	actorUserID, actorName := eventActor(c)
	accepted := applications.DefaultPoller.TriggerSync(&repo, applications.SyncOrigin{
		Source:      models.ApplicationEventSourceManual,
		ActorUserID: actorUserID,
		ActorName:   actorName,
	})
	if !accepted {
		return nil, huma.Error409Conflict("repository sync already in progress")
	}

	return restapi.Message("sync triggered"), nil
})

// resolveProvider validates the provider enum and authMethod, returning the
// registered provider on success, or a 400 error on failure.
func resolveProvider(
	prov models.RepositoryProvider,
	authMethod models.RepositoryAuthMethod,
) (repositories.Provider, error) {
	switch prov {
	case models.GitHub, models.GitLab, models.Generic, models.Bitbucket, models.AzureDevOps, models.Gitea:
	default:
		return nil, huma.Error400BadRequest("invalid provider: must be github, gitlab, or generic")
	}

	switch authMethod {
	case models.AuthMethodNone, models.AuthMethodToken, models.AuthMethodBasic, models.AuthMethodSSH:
	default:
		return nil, huma.Error400BadRequest("invalid authMethod: must be none, token, basic, or ssh")
	}

	p, err := repositories.Get(prov)
	if err != nil {
		return nil, huma.Error400BadRequest("unsupported provider")
	}

	if !isAuthMethodSupported(authMethod, p.SupportedAuthMethods()) {
		return nil, huma.Error400BadRequest("authMethod is not supported by this provider")
	}

	return p, nil
}

func isAuthMethodSupported(method models.RepositoryAuthMethod, supported []models.RepositoryAuthMethod) bool {
	return slices.Contains(supported, method)
}

func resolveRepository(ctx context.Context, id string) (models.Repository, repositories.Provider, error) {
	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Repository{}, nil, huma.Error404NotFound("repository not found")
		}
		return models.Repository{}, nil, internalError()
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		return models.Repository{}, nil, huma.Error400BadRequest("unsupported provider")
	}

	return repo, provider, nil
}
