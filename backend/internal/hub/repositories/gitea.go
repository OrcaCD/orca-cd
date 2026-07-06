package repositories

import (
	"context"
	"encoding/base64"
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

type giteaProvider struct{}

type parsedGiteaRepositoryURL struct {
	baseURL string
	owner   string
	repo    string
}

type giteaBranch struct {
	Name string `json:"name"`
}

type giteaTreeResponse struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

type giteaFileResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type giteaCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

func init() {
	Register(models.Gitea, giteaProvider{})
}

// parseGiteaURL validates a Gitea repository URL (including self-hosted instances)
// and returns the owner and repository name.
func parseGiteaURL(rawURL string) (owner, repo string, err error) {
	parsedRepoURL, err := parseGiteaRepositoryURL(rawURL)
	if err != nil {
		return "", "", err
	}

	return parsedRepoURL.owner, parsedRepoURL.repo, nil
}

func parseGiteaRepositoryURL(rawURL string) (parsedGiteaRepositoryURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return parsedGiteaRepositoryURL{}, errors.New("invalid URL")
	}
	if u.Scheme != httpsScheme {
		return parsedGiteaRepositoryURL{}, fmt.Errorf("URL must use %s", httpsScheme)
	}
	if u.Host == "" {
		return parsedGiteaRepositoryURL{}, errors.New("URL must have a valid host")
	}

	// Allow URLs ending with .git
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return parsedGiteaRepositoryURL{}, fmt.Errorf("URL must be in the format %s://{host}/{owner}/{repo}", httpsScheme)
	}

	owner, repo := parts[0], parts[1]
	if !ownerRe.MatchString(owner) {
		return parsedGiteaRepositoryURL{}, errors.New("invalid Gitea owner name")
	}
	if !repoRe.MatchString(repo) {
		return parsedGiteaRepositoryURL{}, errors.New("invalid Gitea repository name")
	}

	return parsedGiteaRepositoryURL{
		baseURL: fmt.Sprintf("%s://%s", u.Scheme, u.Host),
		owner:   owner,
		repo:    repo,
	}, nil
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

	parsedRepoURL, err := parseGiteaRepositoryURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s",
		parsedRepoURL.baseURL,
		url.PathEscape(parsedRepoURL.owner),
		url.PathEscape(parsedRepoURL.repo),
	)
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build Gitea request: %w", err)
	}
	addGiteaHeaders(req, repo)

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

func (giteaProvider) ListBranches(ctx context.Context, repo *models.Repository) ([]string, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	parsedRepoURL, err := parseGiteaRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	branches := make([]string, 0)

	for page := 1; ; page++ {
		apiURL := fmt.Sprintf(
			"%s/api/v1/repos/%s/%s/branches?page=%d&limit=%d",
			parsedRepoURL.baseURL,
			url.PathEscape(parsedRepoURL.owner),
			url.PathEscape(parsedRepoURL.repo),
			page,
			providerPageSize,
		)

		req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build Gitea request: %w", err)
		}
		addGiteaHeaders(req, repo)

		resp, err := httpclient.Default.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Gitea branches: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			var parsed []giteaBranch
			if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					fmt.Printf("warning: failed to close Gitea response body: %v\n", closeErr)
				}
				return nil, fmt.Errorf("failed to decode Gitea branches response: %w", err)
			}

			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Gitea response body: %v\n", closeErr)
			}

			for _, branch := range parsed {
				if branch.Name != "" {
					branches = append(branches, branch.Name)
				}
			}

			if len(parsed) < providerPageSize {
				sortBranches(branches)
				return branches, nil
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Gitea response body: %v\n", closeErr)
			}
			return nil, errors.New("authentication failed or access denied")
		case http.StatusNotFound:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Gitea response body: %v\n", closeErr)
			}
			return nil, errors.New("repository not found or access denied")
		default:
			statusCode := resp.StatusCode
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Gitea response body: %v\n", closeErr)
			}
			return nil, fmt.Errorf("gitea API returned unexpected status: %d", statusCode)
		}
	}
}

func (giteaProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	if strings.TrimSpace(branch) == "" {
		return nil, errors.New("branch is required")
	}

	parsedRepoURL, err := parseGiteaRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/git/trees/%s?recursive=true",
		parsedRepoURL.baseURL,
		url.PathEscape(parsedRepoURL.owner),
		url.PathEscape(parsedRepoURL.repo),
		url.PathEscape(branch),
	)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build Gitea request: %w", err)
	}
	addGiteaHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Gitea repository tree: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Gitea response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed giteaTreeResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("failed to decode Gitea tree response: %w", err)
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
		return nil, fmt.Errorf("gitea API returned unexpected status: %d", resp.StatusCode)
	}
}

func (giteaProvider) GetFileContent(ctx context.Context, repo *models.Repository, ref string, path string) (string, error) {
	if repo == nil {
		return "", errors.New("repository is required")
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("ref is required")
	}

	normalizedPath := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if normalizedPath == "" {
		return "", errors.New("path is required")
	}

	parsedRepoURL, err := parseGiteaRepositoryURL(repo.Url)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/contents/%s?ref=%s",
		parsedRepoURL.baseURL,
		url.PathEscape(parsedRepoURL.owner),
		url.PathEscape(parsedRepoURL.repo),
		url.PathEscape(normalizedPath),
		url.QueryEscape(ref),
	)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build Gitea request: %w", err)
	}
	addGiteaHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Gitea file content: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Gitea response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed giteaFileResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return "", fmt.Errorf("failed to decode Gitea file response: %w", err)
		}

		if !strings.EqualFold(parsed.Encoding, "base64") {
			return "", fmt.Errorf("unsupported Gitea file encoding: %q", parsed.Encoding)
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parsed.Content, "\n", ""))
		if err != nil {
			return "", fmt.Errorf("failed to decode Gitea file content: %w", err)
		}

		return string(decoded), nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return "", errors.New("file not found in repository branch or access denied")
	default:
		return "", fmt.Errorf("gitea API returned unexpected status: %d", resp.StatusCode)
	}
}

func (giteaProvider) GetLatestCommit(ctx context.Context, repo *models.Repository, branch string) (CommitInfo, error) {
	if repo == nil {
		return CommitInfo{}, errors.New("repository is required")
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return CommitInfo{}, errors.New("branch is required")
	}

	parsedRepoURL, err := parseGiteaRepositoryURL(repo.Url)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := fmt.Sprintf(
		"%s/api/v1/repos/%s/%s/commits?sha=%s&page=1&limit=1",
		parsedRepoURL.baseURL,
		url.PathEscape(parsedRepoURL.owner),
		url.PathEscape(parsedRepoURL.repo),
		url.QueryEscape(branch),
	)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to build Gitea request: %w", err)
	}
	addGiteaHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to fetch Gitea commit: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Gitea response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed []giteaCommit
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return CommitInfo{}, fmt.Errorf("failed to decode Gitea commit response: %w", err)
		}

		if len(parsed) == 0 {
			return CommitInfo{}, errors.New("no commits found for branch")
		}

		commit := parsed[0]
		if commit.SHA == "" {
			return CommitInfo{}, errors.New("missing commit hash in Gitea response")
		}

		return CommitInfo{Hash: commit.SHA, Message: commit.Commit.Message}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return CommitInfo{}, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return CommitInfo{}, errors.New("repository or branch not found or access denied")
	default:
		return CommitInfo{}, fmt.Errorf("gitea API returned unexpected status: %d", resp.StatusCode)
	}
}

func addGiteaHeaders(req *http.Request, repo *models.Repository) {
	req.Header.Set("Accept", "application/json")

	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}
	}
}
