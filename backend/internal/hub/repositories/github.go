package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const githubAPIBase = "https://api.github.com"

type githubProvider struct{}

type githubBranch struct {
	Name string `json:"name"`
}

type githubTreeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

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

func (githubProvider) ParseURL(rawURL string) (string, string, error) {
	return parseGitHubURL(rawURL)
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
	addGitHubHeaders(req, repo)

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

func (githubProvider) ListBranches(ctx context.Context, repo *models.Repository) ([]string, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	owner, repoName, err := parseGitHubURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=100", githubAPIBase, owner, repoName)
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build GitHub request: %w", err)
	}
	addGitHubHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub branches: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitHub response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed []githubBranch
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("failed to decode GitHub branches response: %w", err)
		}

		branches := make([]string, 0, len(parsed))
		for _, branch := range parsed {
			if branch.Name != "" {
				branches = append(branches, branch.Name)
			}
		}
		sort.Strings(branches)
		return branches, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return nil, errors.New("repository not found or access denied")
	default:
		return nil, fmt.Errorf("GitHub API returned unexpected status: %d", resp.StatusCode)
	}
}

func (githubProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	if strings.TrimSpace(branch) == "" {
		return nil, errors.New("branch is required")
	}

	owner, repoName, err := parseGitHubURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/git/trees/%s?recursive=1",
		githubAPIBase,
		owner,
		repoName,
		url.PathEscape(branch),
	)
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build GitHub request: %w", err)
	}
	addGitHubHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub repository tree: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitHub response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed githubTreeResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("failed to decode GitHub tree response: %w", err)
		}

		entries := make([]TreeEntry, 0, len(parsed.Tree))
		for _, entry := range parsed.Tree {
			if entry.Path == "" {
				continue
			}

			switch entry.Type {
			case "blob":
				entries = append(entries, TreeEntry{Path: entry.Path, Type: TreeEntryTypeFile})
			case "tree":
				entries = append(entries, TreeEntry{Path: entry.Path, Type: TreeEntryTypeDir})
			}
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Path < entries[j].Path
		})

		return entries, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return nil, errors.New("repository not found or access denied")
	default:
		return nil, fmt.Errorf("GitHub API returned unexpected status: %d", resp.StatusCode)
	}
}

func addGitHubHeaders(req *http.Request, repo *models.Repository) {
	req.Header.Set("Accept", "application/vnd.github+json")

	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}
