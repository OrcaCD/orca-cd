package repositories

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const githubAPIBase = "https://api.github.com"

// ownerRe matches GitHub usernames/org names: 1–39 alphanumeric chars or hyphens,
// not starting or ending with a hyphen
var ownerRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// repoRe matches GitHub repository names: 1–100 chars, alphanumeric, hyphens,
// underscores, or dots
var repoRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,100}$`)

type githubProvider struct{}

func init() {
	Register(models.GitHub, githubProvider{})
}

// parseGitHubURL validates a GitHub repository URL and returns the owner and repo name.
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", errors.New("invalid URL")
	}
	if u.Scheme != "https" {
		return "", "", errors.New("URL must use https")
	}
	if u.Host != "github.com" {
		return "", "", errors.New("URL host must be github.com")
	}

	// Also allow URLs ending with .git
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("URL must be in the format https://github.com/{owner}/{repo}")
	}

	owner, repo = parts[0], parts[1]
	if !ownerRe.MatchString(owner) {
		return "", "", errors.New("invalid GitHub owner name")
	}
	if !repoRe.MatchString(repo) {
		return "", "", errors.New("invalid GitHub repository name")
	}

	return owner, repo, nil
}

func (githubProvider) ValidateURL(rawURL string) error {
	_, _, err := parseGitHubURL(rawURL)
	return err
}

func (githubProvider) SupportedAuthMethods() []models.RepositoryAuthMethod {
	return []models.RepositoryAuthMethod{
		models.AuthMethodNone,
		models.AuthMethodToken,
	}
}

func (githubProvider) TestConnection(ctx context.Context, repo *models.Repository) error {
	if repo == nil {
		return errors.New("repository is required")
	}

	owner, repoName, err := parseGitHubURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, owner, repoName)
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to GitHub: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitHub response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return errors.New("repository not found or access denied")
	default:
		return fmt.Errorf("GitHub API returned unexpected status: %d", resp.StatusCode)
	}
}
