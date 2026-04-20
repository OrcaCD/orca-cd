package repositories

import (
	"context"
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

type gitlabBranch struct {
	Name string `json:"name"`
}

type gitlabTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func init() {
	Register(models.GitLab, gitlabProvider{})
}

// parseGitLabURL validates a GitLab repository URL (including self-hosted instances)
// and returns the namespace and project name. The host is not constrained to gitlab.com
// to support self-hosted deployments.
func parseGitLabURL(rawURL string) (namespace, project string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", errors.New("invalid URL")
	}
	if u.Scheme != "https" {
		return "", "", errors.New("URL must use https")
	}
	if u.Host == "" {
		return "", "", errors.New("URL must have a valid host")
	}
	if u.Host == "github.com" {
		return "", "", errors.New("github.com is not a valid GitLab host")
	}

	// Allow URLs ending with .git
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	// Need at least namespace/project (two path segments)
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[len(parts)-1] == "" {
		return "", "", errors.New("URL must be in the format https://{host}/{namespace}/{project}")
	}

	// Validate each path segment
	for _, seg := range parts {
		if !repoRe.MatchString(seg) {
			return "", "", fmt.Errorf("invalid path segment %q in URL", seg)
		}
	}

	project = parts[len(parts)-1]
	namespace = strings.Join(parts[:len(parts)-1], "/")

	return namespace, project, nil
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

	namespace, project, err := parseGitLabURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	u, _ := url.Parse(repo.Url)
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// GitLab API: GET /api/v4/projects/:id where :id is the URL-encoded namespace/project path
	projectPath := url.PathEscape(namespace + "/" + project)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", baseURL, projectPath)

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

	namespace, project, err := parseGitLabURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	u, _ := url.Parse(repo.Url)
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	projectPath := url.PathEscape(namespace + "/" + project)

	branches := make([]string, 0)
	page := 1

	for {
		apiURL := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/branches?per_page=100&page=%d",
			baseURL,
			projectPath,
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

	sort.Strings(branches)
	return branches, nil
}

func (gitlabProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	if strings.TrimSpace(branch) == "" {
		return nil, errors.New("branch is required")
	}

	namespace, project, err := parseGitLabURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	u, _ := url.Parse(repo.Url)
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	projectPath := url.PathEscape(namespace + "/" + project)

	entries := make([]TreeEntry, 0)
	page := 1

	for {
		apiURL := fmt.Sprintf(
			"%s/api/v4/projects/%s/repository/tree?ref=%s&recursive=true&per_page=100&page=%d",
			baseURL,
			projectPath,
			url.QueryEscape(branch),
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

func addGitLabAuthHeader(req *http.Request, repo *models.Repository) {
	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}
