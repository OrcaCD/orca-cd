package repositories

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

type giteaProvider struct{}

const httpsScheme = "https"

func init() {
	Register(models.Gitea, giteaProvider{})
}

// parseGiteaURL validates a Gitea repository URL (including self-hosted instances)
// and returns the owner and repository name.
func parseGiteaURL(rawURL string) (owner, repo string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", errors.New("invalid URL")
	}
	if u.Scheme != httpsScheme {
		return "", "", fmt.Errorf("URL must use %s", httpsScheme)
	}
	if u.Host == "" {
		return "", "", errors.New("URL must have a valid host")
	}

	// Allow URLs ending with .git
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("URL must be in the format %s://{host}/{owner}/{repo}", httpsScheme)
	}

	owner, repo = parts[0], parts[1]
	if !ownerRe.MatchString(owner) {
		return "", "", errors.New("invalid Gitea owner name")
	}
	if !repoRe.MatchString(repo) {
		return "", "", errors.New("invalid Gitea repository name")
	}

	return owner, repo, nil
}

func (giteaProvider) ParseURL(rawURL string) (string, string, error) {
	return parseGiteaURL(rawURL)
}

func (giteaProvider) SupportedAuthMethods() []models.RepositoryAuthMethod {
	return []models.RepositoryAuthMethod{
		models.AuthMethodNone,
		models.AuthMethodToken,
	}
}

func (giteaProvider) TestConnection(ctx context.Context, repo *models.Repository) error {
	if repo == nil {
		return errors.New("repository is required")
	}

	owner, repoName, err := parseGiteaURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	u, _ := url.Parse(repo.Url)
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	apiURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", baseURL, url.PathEscape(owner), url.PathEscape(repoName))
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build Gitea request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}
	}

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Gitea: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Gitea response body: %v\n", err)
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
		return fmt.Errorf("gitea API returned unexpected status: %d", resp.StatusCode)
	}
}
