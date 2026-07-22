package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/repositories"
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

func WebhookHandler(c *gin.Context) {
	id := c.Param("id")

	repo, err := gorm.G[models.Repository](db.DB).Where("id = ?", id).First(c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if repo.SyncType != models.SyncTypeWebhook {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repository is not configured for webhook sync"})
		return
	}

	if repo.WebhookSecret == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if !hasProviderEventHeader(c) {
		handleGenericWebhook(c, repo)
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	secret := repo.WebhookSecret.String()

	if !validateSignature(c, repo.Provider, secret, body) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook signature"})
		return
	}

	if !isPushEvent(c, repo.Provider) {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	pushDetails, err := parseWebhookPushDetails(c, body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook payload"})
		return
	}

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	apps, err := applications.GetMatchingApplications(c.Request.Context(), &repo, pushDetails.Branch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	release, ok := tryLockRepositorySync(repo.Id)
	if !ok {
		// A sync for this repository is already in progress, most likely this
		// exact push redelivered by the provider's webhook retry logic. Skip
		// silently instead of dispatching a duplicate deploy.
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	defer release()

	// TODO: get commit message for push event and pass it to the queue.
	// Might be simpler to get it via API instead of parsing it from the webhook payload, since the relevant field names differ between providers.
	// In case we get it from the API we need to add a GetCommitDetails method to the Provider interface which does not pull the latest commit.
	applications.SyncApplications(c.Request.Context(), &repo, provider, apps, applications.StaticCommit(pushDetails.Commit, ""), applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	c.AbortWithStatus(http.StatusNoContent)
}

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

func handleGenericWebhook(c *gin.Context, repo models.Repository) {
	secret := repo.WebhookSecret.String()
	token := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxWebhookBodySize))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	branch, commit := parseGenericWebhookBody(body)

	provider, err := repositories.Get(repo.Provider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	var apps []models.Application
	if branch != "" {
		apps, err = applications.GetMatchingApplications(c.Request.Context(), &repo, branch)
	} else {
		apps, err = applications.GetAllApplicationsForRepo(c.Request.Context(), &repo)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	release, ok := tryLockRepositorySync(repo.Id)
	if !ok {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	defer release()

	// When the payload omits the commit, resolve the latest commit per branch from
	// the provider; otherwise deploy the commit the caller supplied.
	resolve := applications.LatestCommit(provider, &repo)
	if commit != "" {
		resolve = applications.StaticCommit(commit, "")
	}
	applications.SyncApplications(c.Request.Context(), &repo, provider, apps, resolve, applications.SyncOrigin{Source: models.ApplicationEventSourceRepositoryWebhook}, &applications.Log)

	c.AbortWithStatus(http.StatusNoContent)
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

func hasProviderEventHeader(c *gin.Context) bool {
	return c.GetHeader("X-GitHub-Event") != "" ||
		c.GetHeader("X-Gitea-Event") != "" ||
		c.GetHeader("X-Gitlab-Event") != ""
}

func validateSignature(c *gin.Context, provider models.RepositoryProvider, secret string, body []byte) bool {
	switch provider {
	case models.GitHub:
		return validateHMACSHA256(secret, body, strings.TrimPrefix(c.GetHeader("X-Hub-Signature-256"), "sha256="))
	case models.Gitea:
		return validateHMACSHA256(secret, body, c.GetHeader("X-Gitea-Signature"))
	case models.GitLab:
		token := c.GetHeader("X-Gitlab-Token")
		return subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
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

func isPushEvent(c *gin.Context, provider models.RepositoryProvider) bool {
	switch provider {
	case models.GitHub:
		return c.GetHeader("X-GitHub-Event") == "push"
	case models.Gitea:
		return c.GetHeader("X-Gitea-Event") == "push"
	case models.GitLab:
		return c.GetHeader("X-Gitlab-Event") == "Push Hook"
	default:
		return false
	}
}

func parseWebhookPushDetails(c *gin.Context, body []byte) (webhookPushDetails, error) {
	decodedBody, form, err := decodeWebhookBody(c, body)
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

func decodeWebhookBody(c *gin.Context, body []byte) ([]byte, url.Values, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
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
