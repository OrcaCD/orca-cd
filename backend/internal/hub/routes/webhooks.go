package routes

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/seclog"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type genericWebhookPayload struct {
	Ref    string `json:"ref"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

// maxWebhookBodySize caps the request body to guard against memory exhaustion from oversized payloads.
const maxWebhookBodySize = 10 * 1024 * 1024 // 10 MB

type webhookPushDetails struct {
	Branch string
	Commit string
}

type pushPayload struct {
	Ref   string `json:"ref"`
	After string `json:"after"`
}

// webhookInput is the input of the repository webhook. The providers sign the
// raw body, so it's verified against exactly the bytes that were sent.
type webhookInput struct {
	ID              string `path:"id" doc:"The ID of the repository"`
	GitHubEvent     string `header:"X-GitHub-Event" doc:"Set by GitHub. Only push events trigger a sync."`
	GitHubSignature string `header:"X-Hub-Signature-256" doc:"Set by GitHub: sha256= followed by the hex HMAC-SHA256 of the body, keyed with the webhook secret"`
	GiteaEvent      string `header:"X-Gitea-Event" doc:"Set by Gitea. Only push events trigger a sync."`
	GiteaSignature  string `header:"X-Gitea-Signature" doc:"Set by Gitea: the hex HMAC-SHA256 of the body, keyed with the webhook secret"`
	GitLabEvent     string `header:"X-Gitlab-Event" doc:"Set by GitLab. Only Push Hook events trigger a sync."`
	GitLabToken     string `header:"X-Gitlab-Token" doc:"Set by GitLab: the webhook secret"`
	Authorization   string `header:"Authorization" doc:"Generic webhooks without a provider event header send Bearer followed by the webhook secret"`
	ContentType     string `header:"Content-Type" hidden:"true"`
	RawBody         []byte
}

var webhookRequestBody = &huma.RequestBody{
	Description: "The push event of the provider, as JSON or form encoded with the JSON in the payload field. " +
		"A generic webhook may send `{\"ref\": \"...\", \"branch\": \"...\", \"commit\": \"...\"}` or no body.",
	Content: map[string]*huma.MediaType{
		"application/json":                  {Schema: &huma.Schema{Type: huma.TypeObject}},
		"application/x-www-form-urlencoded": {Schema: &huma.Schema{Type: huma.TypeObject}},
	},
}

// webhookHandler turns a webhook operation into a Gin handler. A webhook takes
// the body as RawBody, which Huma then requires and documents as
// application/octet-stream. Callers may send no body at all, e.g. a generic
// webhook with only the token, and Huma never parses the bytes, so the body is
// made optional and documented with body after registering.
func webhookHandler[I, O any](op huma.Operation, body *huma.RequestBody, handler func(context.Context, *I) (*O, error)) gin.HandlerFunc {
	op.MaxBodyBytes = maxWebhookBodySize
	op.RequestBody = &huma.RequestBody{}
	h := restapi.Handler(op, handler)
	*op.RequestBody = *body
	return func(c *gin.Context) {
		h(c)
		// Huma only sets the status of a response without body. Write it now like
		// c.AbortWithStatus did, instead of leaving it to the end of the chain.
		c.Writer.WriteHeaderNow()
	}
}

func rejectWebhook(c *gin.Context, reason string) error {
	seclog.Warn(c, seclog.WebhookAuthFailed, "reason", reason)
	return huma.Error401Unauthorized("unauthorized")
}

var WebhookHandler = webhookHandler(huma.Operation{
	OperationID: "receive-repository-webhook",
	Method:      http.MethodPost,
	Path:        "/webhooks/repositories/{id}",
	Summary:     "Receive a repository webhook",
	Description: "Called by GitHub, GitLab and Gitea on a push to sync the applications of a repository whose sync type is webhook. " +
		"GitHub and Gitea sign the body with the webhook secret, GitLab sends it as token. Other events are ignored. " +
		"Without a provider event header the webhook is generic: it authenticates with `Authorization: Bearer <webhook secret>` " +
		"and syncs the given branch and commit, or every application of the repository at its latest commit.",
	Tags:          []string{tagWebhooks},
	DefaultStatus: http.StatusNoContent,
	Errors:        []int{http.StatusBadRequest, http.StatusUnauthorized},
}, webhookRequestBody, func(ctx context.Context, input *webhookInput) (*struct{}, error) {
	c := restapi.GinContext(ctx)

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, rejectWebhook(c, "unknown_id")
		}
		return nil, internalError()
	}

	if repo.SyncType != models.SyncTypeWebhook || repo.WebhookSecret == nil {
		return nil, rejectWebhook(c, "webhook_disabled")
	}

	if !hasProviderEventHeader(input) {
		return handleGenericWebhook(ctx, input, repo)
	}

	secret := repo.WebhookSecret.String()

	if !validateSignature(input, repo.Provider, secret) {
		return nil, rejectWebhook(c, "invalid_signature")
	}

	if !isPushEvent(input, repo.Provider) {
		return &struct{}{}, nil
	}

	pushDetails, err := parseWebhookPushDetails(input.ContentType, input.RawBody)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid webhook payload")
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		return nil, internalError()
	}

	apps, err := applications.GetMatchingApplications(ctx, &repo, pushDetails.Branch)
	if err != nil {
		return nil, internalError()
	}

	release, ok := tryLockRepositorySync(repo.Id)
	if !ok {
		// A sync for this repository is already in progress, most likely this
		// exact push redelivered by the provider's webhook retry logic. Skip
		// silently instead of dispatching a duplicate deploy.
		return &struct{}{}, nil
	}
	defer release()

	// TODO: get commit message for push event and pass it to the queue.
	// Might be simpler to get it via API instead of parsing it from the webhook payload, since the relevant field names differ between providers.
	// In case we get it from the API we need to add a GetCommitDetails method to the Provider interface which does not pull the latest commit.
	applications.SyncApplications(ctx, &repo, provider, apps, applications.StaticCommit(pushDetails.Commit, ""), applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	return &struct{}{}, nil
})

// tryLockRepositorySync claims the in-flight sync slot for repoID via the
// default poller so a duplicated webhook delivery cannot dispatch the same
// sync twice concurrently. Falls back to allowing the sync when the poller
// isn't initialized (e.g. in tests), since the lock is a best-effort guard,
// not a correctness requirement.
func tryLockRepositorySync(repoID string) (release func(), ok bool) {
	if applications.DefaultPoller == nil {
		return func() {}, true
	}
	return applications.DefaultPoller.TryLockRepositorySync(repoID)
}

func handleGenericWebhook(ctx context.Context, input *webhookInput, repo models.Repository) (*struct{}, error) {
	secret := repo.WebhookSecret.String()
	token := strings.TrimSpace(strings.TrimPrefix(input.Authorization, "Bearer "))
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		return nil, rejectWebhook(restapi.GinContext(ctx), "invalid_token")
	}

	branch, commit := parseGenericWebhookBody(input.RawBody)

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		return nil, internalError()
	}

	var apps []models.Application
	if branch != "" {
		apps, err = applications.GetMatchingApplications(ctx, &repo, branch)
	} else {
		apps, err = applications.GetAllApplicationsForRepo(ctx, &repo)
	}
	if err != nil {
		return nil, internalError()
	}

	release, ok := tryLockRepositorySync(repo.Id)
	if !ok {
		return &struct{}{}, nil
	}
	defer release()

	// When the payload omits the commit, resolve the latest commit per branch from
	// the provider; otherwise deploy the commit the caller supplied.
	resolve := applications.LatestCommit(provider, &repo)
	if commit != "" {
		resolve = applications.StaticCommit(commit, "")
	}
	applications.SyncApplications(ctx, &repo, provider, apps, resolve, applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	return &struct{}{}, nil
}

// parseGenericWebhookBody extracts an optional branch and commit from the request body.
// ref takes priority over branch; both are optional. Returns empty strings on empty or invalid JSON.
func parseGenericWebhookBody(body []byte) (branch, commit string) {
	if len(body) == 0 {
		return "", ""
	}
	var payload genericWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	if payload.Ref != "" {
		branch = extractBranchFromRef(payload.Ref)
	} else {
		branch = strings.TrimSpace(payload.Branch)
	}
	return branch, strings.TrimSpace(payload.Commit)
}

func hasProviderEventHeader(input *webhookInput) bool {
	return input.GitHubEvent != "" || input.GiteaEvent != "" || input.GitLabEvent != ""
}

func validateSignature(input *webhookInput, provider models.RepositoryProvider, secret string) bool {
	switch provider {
	case models.GitHub:
		return validateHMACSHA256(secret, input.RawBody, strings.TrimPrefix(input.GitHubSignature, "sha256="))
	case models.Gitea:
		return validateHMACSHA256(secret, input.RawBody, input.GiteaSignature)
	case models.GitLab:
		return subtle.ConstantTimeCompare([]byte(input.GitLabToken), []byte(secret)) == 1
	default:
		return false
	}
}

func validateHMACSHA256(secret string, body []byte, hexSig string) bool {
	if hexSig == "" {
		return false
	}
	sig, err := hex.DecodeString(hexSig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

func isPushEvent(input *webhookInput, provider models.RepositoryProvider) bool {
	switch provider {
	case models.GitHub:
		return input.GitHubEvent == "push"
	case models.Gitea:
		return input.GiteaEvent == "push"
	case models.GitLab:
		return input.GitLabEvent == "Push Hook"
	default:
		return false
	}
}

func parseWebhookPushDetails(contentType string, body []byte) (webhookPushDetails, error) {
	decodedBody, form, err := decodeWebhookBody(contentType, body)
	if err != nil {
		return webhookPushDetails{}, err
	}

	var payload pushPayload
	switch {
	case len(decodedBody) > 0:
		if err := json.Unmarshal(decodedBody, &payload); err != nil {
			return webhookPushDetails{}, err
		}
	case form != nil:
		payload.Ref = form.Get("ref")
		payload.After = form.Get("after")
	default:
		return webhookPushDetails{}, errors.New("empty webhook payload")
	}

	branch := extractBranchFromRef(payload.Ref)
	if branch == "" {
		return webhookPushDetails{}, errors.New("missing branch ref")
	}

	commit := strings.TrimSpace(payload.After)
	if commit == "" {
		return webhookPushDetails{}, errors.New("missing commit hash")
	}

	return webhookPushDetails{
		Branch: branch,
		Commit: commit,
	}, nil
}

func decodeWebhookBody(contentType string, body []byte) ([]byte, url.Values, error) {
	if strings.HasPrefix(strings.ToLower(contentType), "application/x-www-form-urlencoded") {
		form, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, nil, err
		}
		payload := strings.TrimSpace(form.Get("payload"))
		if payload != "" {
			return []byte(payload), form, nil
		}
		return nil, form, nil
	}
	return body, nil, nil
}

func extractBranchFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	const headsPrefix = "refs/heads/"
	if branch, ok := strings.CutPrefix(ref, headsPrefix); ok {
		return strings.TrimSpace(branch)
	}
	return ref
}
