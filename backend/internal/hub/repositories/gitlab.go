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
	"strconv"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

type gitlabProvider struct{}

type parsedGitLabRepositoryURL struct {
	baseURL   string
	namespace string
	project   string
}

type gitlabBranch struct {
	Name string `json:"name"`
}

type gitlabTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type gitlabFileResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type gitlabCommit struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Title   string `json:"title"`
}

func init() {
	Register(models.GitLab, gitlabProvider{})
}

// parseGitLabURL validates a GitLab repository URL (including self-hosted instances)
// and returns the namespace and project name. The host is not constrained to gitlab.com
// to support self-hosted deployments.
func parseGitLabURL(rawURL string) (namespace, project string, err error) {
	parsedRepoURL, err := parseGitLabRepositoryURL(rawURL)
	if err != nil {
		return "", "", err
	}

	return parsedRepoURL.namespace, parsedRepoURL.project, nil
}

func parseGitLabRepositoryURL(rawURL string) (parsedGitLabRepositoryURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return parsedGitLabRepositoryURL{}, errors.New("invalid URL")
	}
	if u.Scheme != httpsScheme {
		return parsedGitLabRepositoryURL{}, fmt.Errorf("URL must use %s", httpsScheme)
	}
	if u.Host == "" {
		return parsedGitLabRepositoryURL{}, errors.New("URL must have a valid host")
	}
	if u.Host == "github.com" {
		return parsedGitLabRepositoryURL{}, errors.New("github.com is not a valid GitLab host")
	}

	// Allow URLs ending with .git
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	// Need at least namespace/project (two path segments)
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return parsedGitLabRepositoryURL{}, fmt.Errorf("URL must be in the format %s://{host}/{namespace}/{project}", httpsScheme)
	}

	// Validate each path segment
	for _, seg := range parts {
		if !repoRe.MatchString(seg) {
			return parsedGitLabRepositoryURL{}, fmt.Errorf("invalid path segment %q in URL", seg)
		}
	}

	project := parts[len(parts)-1]
	namespace := strings.Join(parts[:len(parts)-1], "/")

	return parsedGitLabRepositoryURL{
		baseURL:   fmt.Sprintf("%s://%s", u.Scheme, u.Host),
		namespace: namespace,
		project:   project,
	}, nil
}

func (gitlabProvider) ParseURL(rawURL string) (string, string, error) {
	return parseGitLabURL(rawURL)
}

func (gitlabProvider) SupportedAuthMethods() []models.RepositoryAuthMethod {
	return []models.RepositoryAuthMethod{
		models.AuthMethodNone,
		models.AuthMethodToken,
	}
}

func (gitlabProvider) TestConnection(ctx context.Context, repo *models.Repository) error {
	if repo == nil {
		return errors.New("repository is required")
	}

	parsedRepoURL, err := parseGitLabRepositoryURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	// GitLab API: GET /api/v4/projects/:id where :id is the URL-encoded namespace/project path
	projectPath := url.PathEscape(parsedRepoURL.namespace + "/" + parsedRepoURL.project)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", parsedRepoURL.baseURL, projectPath)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build GitLab request: %w", err)
	}
	addGitLabAuthHeader(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to GitLab: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitLab response body: %v\n", err)
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
		return fmt.Errorf("GitLab API returned unexpected status: %d", resp.StatusCode)
	}
}

func (gitlabProvider) ListBranches(ctx context.Context, repo *models.Repository) ([]string, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	parsedRepoURL, err := parseGitLabRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}
	projectPath := url.PathEscape(parsedRepoURL.namespace + "/" + parsedRepoURL.project)

	branches := make([]string, 0)
	page := 1

	for {
		apiURL := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/branches?per_page=%d&page=%d",
			parsedRepoURL.baseURL,
			projectPath,
			providerPageSize,
			page,
		)

		req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build GitLab request: %w", err)
		}
		addGitLabAuthHeader(req, repo)

		resp, err := httpclient.Default.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch GitLab branches: %w", err)
		}

		nextPage := strings.TrimSpace(resp.Header.Get("X-Next-Page"))
		responseCount := 0

		switch resp.StatusCode {
		case http.StatusOK:
			var parsed []gitlabBranch
			if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
				}
				return nil, fmt.Errorf("failed to decode GitLab branches response: %w", err)
			}

			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}

			for _, branch := range parsed {
				if branch.Name != "" {
					branches = append(branches, branch.Name)
				}
			}

			responseCount = len(parsed)
		case http.StatusUnauthorized, http.StatusForbidden:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, errors.New("authentication failed or access denied")
		case http.StatusNotFound:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, errors.New("repository not found or access denied")
		default:
			statusCode := resp.StatusCode
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, fmt.Errorf("GitLab API returned unexpected status: %d", statusCode)
		}

		if nextPage == "" {
			if responseCount < providerPageSize {
				break
			}

			page++
			continue
		}

		nextPageNumber, err := strconv.Atoi(nextPage)
		if err != nil || nextPageNumber <= 0 {
			if responseCount < providerPageSize {
				break
			}

			page++
			continue
		}

		page = nextPageNumber
	}

	sortBranches(branches)
	return branches, nil
}

func (gitlabProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	if strings.TrimSpace(branch) == "" {
		return nil, errors.New("branch is required")
	}

	parsedRepoURL, err := parseGitLabRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}
	projectPath := url.PathEscape(parsedRepoURL.namespace + "/" + parsedRepoURL.project)

	entries := make([]TreeEntry, 0)
	page := 1

	for {
		apiURL := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/tree?ref=%s&recursive=true&per_page=%d&page=%d",
			parsedRepoURL.baseURL,
			projectPath,
			url.QueryEscape(branch),
			providerPageSize,
			page,
		)

		req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build GitLab request: %w", err)
		}
		addGitLabAuthHeader(req, repo)

		resp, err := httpclient.Default.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch GitLab repository tree: %w", err)
		}

		nextPage := strings.TrimSpace(resp.Header.Get("X-Next-Page"))

		switch resp.StatusCode {
		case http.StatusOK:
			var parsed []gitlabTreeEntry
			if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
				}
				return nil, fmt.Errorf("failed to decode GitLab tree response: %w", err)
			}

			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}

			for _, entry := range parsed {
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
		case http.StatusUnauthorized, http.StatusForbidden:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, errors.New("authentication failed or access denied")
		case http.StatusNotFound:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, errors.New("repository not found or access denied")
		default:
			statusCode := resp.StatusCode
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close GitLab response body: %v\n", closeErr)
			}
			return nil, fmt.Errorf("GitLab API returned unexpected status: %d", statusCode)
		}

		if nextPage == "" {
			break
		}

		nextPageNumber, err := strconv.Atoi(nextPage)
		if err != nil || nextPageNumber <= 0 {
			break
		}

		page = nextPageNumber
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

func (gitlabProvider) GetFileContent(ctx context.Context, repo *models.Repository, branch, path string) (string, error) {
	if repo == nil {
		return "", errors.New("repository is required")
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", errors.New("branch is required")
	}

	normalizedPath := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if normalizedPath == "" {
		return "", errors.New("path is required")
	}

	parsedRepoURL, err := parseGitLabRepositoryURL(repo.Url)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	projectPath := url.PathEscape(parsedRepoURL.namespace + "/" + parsedRepoURL.project)
	apiURL := fmt.Sprintf(
		"%s/api/v4/projects/%s/repository/files/%s?ref=%s",
		parsedRepoURL.baseURL,
		projectPath,
		url.PathEscape(normalizedPath),
		url.QueryEscape(branch),
	)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build GitLab request: %w", err)
	}
	addGitLabAuthHeader(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch GitLab file content: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitLab response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed gitlabFileResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return "", fmt.Errorf("failed to decode GitLab file response: %w", err)
		}

		if !strings.EqualFold(parsed.Encoding, "base64") {
			return "", fmt.Errorf("unsupported GitLab file encoding: %q", parsed.Encoding)
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(parsed.Content, "\n", ""))
		if err != nil {
			return "", fmt.Errorf("failed to decode GitLab file content: %w", err)
		}

		return string(decoded), nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return "", errors.New("file not found in repository branch or access denied")
	default:
		return "", fmt.Errorf("GitLab API returned unexpected status: %d", resp.StatusCode)
	}
}

func (gitlabProvider) GetLatestCommit(ctx context.Context, repo *models.Repository, branch string) (CommitInfo, error) {
	if repo == nil {
		return CommitInfo{}, errors.New("repository is required")
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return CommitInfo{}, errors.New("branch is required")
	}

	parsedRepoURL, err := parseGitLabRepositoryURL(repo.Url)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("invalid repository URL: %w", err)
	}

	projectPath := url.PathEscape(parsedRepoURL.namespace + "/" + parsedRepoURL.project)
	apiURL := fmt.Sprintf(
		"%s/api/v4/projects/%s/repository/commits?ref_name=%s&per_page=1",
		parsedRepoURL.baseURL,
		projectPath,
		url.QueryEscape(branch),
	)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to build GitLab request: %w", err)
	}
	addGitLabAuthHeader(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to fetch GitLab commit: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close GitLab response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed []gitlabCommit
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return CommitInfo{}, fmt.Errorf("failed to decode GitLab commit response: %w", err)
		}

		if len(parsed) == 0 {
			return CommitInfo{}, errors.New("no commits found for branch")
		}

		commit := parsed[0]
		if commit.ID == "" {
			return CommitInfo{}, errors.New("missing commit hash in GitLab response")
		}

		message := commit.Message
		if message == "" {
			message = commit.Title
		}

		return CommitInfo{Hash: commit.ID, Message: message}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return CommitInfo{}, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return CommitInfo{}, errors.New("repository or branch not found or access denied")
	default:
		return CommitInfo{}, fmt.Errorf("GitLab API returned unexpected status: %d", resp.StatusCode)
	}
}

func addGitLabAuthHeader(req *http.Request, repo *models.Repository) {
	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}
