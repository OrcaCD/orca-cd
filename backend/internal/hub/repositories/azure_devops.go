package repositories

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const azureDevOpsAPIVersion = "7.1"

var azureDevOpsOrganizationRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,48}[a-zA-Z0-9])?$`)

type azureDevOpsProvider struct{}

type parsedAzureDevOpsRepositoryURL struct {
	baseURL      string
	organization string
	project      string
	repository   string
}

type azureDevOpsRefsResponse struct {
	Value []struct {
		Name string `json:"name"`
	} `json:"value"`
}

type azureDevOpsItemsResponse struct {
	Value []azureDevOpsItem `json:"value"`
}

type azureDevOpsItem struct {
	Path          string `json:"path"`
	IsFolder      bool   `json:"isFolder"`
	GitObjectType string `json:"gitObjectType"`
}

type azureDevOpsFileResponse struct {
	Content string `json:"content"`
}

type azureDevOpsCommitsResponse struct {
	Value []azureDevOpsCommit `json:"value"`
}

type azureDevOpsCommit struct {
	CommitID string `json:"commitId"`
	Comment  string `json:"comment"`
}

func init() {
	Register(models.AzureDevOps, azureDevOpsProvider{})
}

func parseAzureDevOpsURL(rawURL string) (owner, repo string, err error) {
	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(rawURL)
	if err != nil {
		return "", "", err
	}

	return parsedRepoURL.organization + "/" + parsedRepoURL.project, parsedRepoURL.repository, nil
}

func parseAzureDevOpsRepositoryURL(rawURL string) (parsedAzureDevOpsRepositoryURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return parsedAzureDevOpsRepositoryURL{}, errors.New("invalid URL")
	}
	if u.Scheme != httpsScheme {
		return parsedAzureDevOpsRepositoryURL{}, fmt.Errorf("URL must use %s", httpsScheme)
	}
	if u.Host == "" {
		return parsedAzureDevOpsRepositoryURL{}, errors.New("URL must have a valid host")
	}

	parts, err := parseAzureDevOpsPathParts(u.EscapedPath())
	if err != nil {
		return parsedAzureDevOpsRepositoryURL{}, err
	}

	host := strings.ToLower(u.Host)
	switch {
	case host == "dev.azure.com":
		if len(parts) != 4 || !strings.EqualFold(parts[2], "_git") {
			return parsedAzureDevOpsRepositoryURL{}, errors.New("URL must be in the format https://dev.azure.com/{organization}/{project}/_git/{repository}")
		}

		organization, project, repository := parts[0], parts[1], strings.TrimSuffix(parts[3], ".git")
		if err := validateAzureDevOpsRepositoryParts(organization, project, repository); err != nil {
			return parsedAzureDevOpsRepositoryURL{}, err
		}

		return parsedAzureDevOpsRepositoryURL{
			baseURL:      fmt.Sprintf("%s://%s/%s", u.Scheme, u.Host, url.PathEscape(organization)),
			organization: organization,
			project:      project,
			repository:   repository,
		}, nil
	case strings.HasSuffix(host, ".visualstudio.com") && host != "visualstudio.com":
		if len(parts) != 3 || !strings.EqualFold(parts[1], "_git") {
			return parsedAzureDevOpsRepositoryURL{}, errors.New("URL must be in the format https://{organization}.visualstudio.com/{project}/_git/{repository}")
		}

		organization := strings.TrimSuffix(host, ".visualstudio.com")
		project, repository := parts[0], strings.TrimSuffix(parts[2], ".git")
		if err := validateAzureDevOpsRepositoryParts(organization, project, repository); err != nil {
			return parsedAzureDevOpsRepositoryURL{}, err
		}

		return parsedAzureDevOpsRepositoryURL{
			baseURL:      fmt.Sprintf("%s://%s", u.Scheme, u.Host),
			organization: organization,
			project:      project,
			repository:   repository,
		}, nil
	default:
		return parsedAzureDevOpsRepositoryURL{}, errors.New("URL host must be dev.azure.com or {organization}.visualstudio.com")
	}
}

func parseAzureDevOpsPathParts(escapedPath string) ([]string, error) {
	path := strings.Trim(escapedPath, "/")
	if path == "" {
		return nil, errors.New("URL path is required")
	}

	rawParts := strings.Split(path, "/")
	parts := make([]string, 0, len(rawParts))
	for _, rawPart := range rawParts {
		if rawPart == "" {
			return nil, errors.New("URL path contains an empty segment")
		}

		part, err := url.PathUnescape(rawPart)
		if err != nil {
			return nil, errors.New("URL path contains an invalid escape sequence")
		}
		parts = append(parts, part)
	}

	return parts, nil
}

func validateAzureDevOpsRepositoryParts(organization, project, repository string) error {
	if !azureDevOpsOrganizationRe.MatchString(organization) {
		return errors.New("invalid Azure DevOps organization name")
	}
	if err := validateAzureDevOpsPathSegment(project, "project"); err != nil {
		return err
	}
	if err := validateAzureDevOpsPathSegment(repository, "repository"); err != nil {
		return err
	}

	return nil
}

func validateAzureDevOpsPathSegment(segment, name string) error {
	if strings.TrimSpace(segment) == "" {
		return fmt.Errorf("azure DevOps %s name is required", name)
	}
	if segment == "." || segment == ".." {
		return fmt.Errorf("invalid Azure DevOps %s name", name)
	}
	if strings.ContainsAny(segment, `/\\:*?\"<>;#$*{},+=[]|`) || strings.ContainsAny(segment, "\x00\r\n\t") {
		return fmt.Errorf("invalid Azure DevOps %s name", name)
	}

	return nil
}

func (azureDevOpsProvider) ParseURL(rawURL string) (string, string, error) {
	return parseAzureDevOpsURL(rawURL)
}

func (azureDevOpsProvider) SupportedAuthMethods() []models.RepositoryAuthMethod {
	return []models.RepositoryAuthMethod{
		models.AuthMethodNone,
		models.AuthMethodToken,
	}
}

func (azureDevOpsProvider) TestConnection(ctx context.Context, repo *models.Repository) error {
	if repo == nil {
		return errors.New("repository is required")
	}

	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(repo.Url)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	apiURL := buildAzureDevOpsAPIURL(parsedRepoURL, fmt.Sprintf(
		"_apis/git/repositories/%s",
		url.PathEscape(parsedRepoURL.repository),
	), nil)
	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build Azure DevOps request: %w", err)
	}
	addAzureDevOpsHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Azure DevOps: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", err)
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
		return fmt.Errorf("azure DevOps API returned unexpected status: %d", resp.StatusCode)
	}
}

func (azureDevOpsProvider) ListBranches(ctx context.Context, repo *models.Repository) ([]string, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	branches := make([]string, 0)
	continuationToken := ""

	for {
		query := url.Values{}
		query.Set("$top", strconv.Itoa(providerPageSize))
		query.Set("filter", "heads/")
		if continuationToken != "" {
			query.Set("continuationToken", continuationToken)
		}

		apiURL := buildAzureDevOpsAPIURL(parsedRepoURL, fmt.Sprintf(
			"_apis/git/repositories/%s/refs",
			url.PathEscape(parsedRepoURL.repository),
		), query)
		req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build Azure DevOps request: %w", err)
		}
		addAzureDevOpsHeaders(req, repo)

		resp, err := httpclient.Default.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Azure DevOps branches: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			var parsed azureDevOpsRefsResponse
			if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", closeErr)
				}
				return nil, fmt.Errorf("failed to decode Azure DevOps branches response: %w", err)
			}

			for _, branch := range parsed.Value {
				name := strings.TrimPrefix(branch.Name, "refs/heads/")
				if name != "" && name != branch.Name {
					branches = append(branches, name)
				}
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", closeErr)
			}
			return nil, errors.New("authentication failed or access denied")
		case http.StatusNotFound:
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", closeErr)
			}
			return nil, errors.New("repository not found or access denied")
		default:
			statusCode := resp.StatusCode
			closeErr := resp.Body.Close()
			if closeErr != nil {
				fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", closeErr)
			}
			return nil, fmt.Errorf("azure DevOps API returned unexpected status: %d", statusCode)
		}

		nextToken := strings.TrimSpace(resp.Header.Get("x-ms-continuationtoken"))
		closeErr := resp.Body.Close()
		if closeErr != nil {
			fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", closeErr)
		}

		if nextToken == "" {
			sortBranches(branches)
			return branches, nil
		}
		continuationToken = nextToken
	}
}

func (azureDevOpsProvider) ListTree(ctx context.Context, repo *models.Repository, branch string) ([]TreeEntry, error) {
	if repo == nil {
		return nil, errors.New("repository is required")
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, errors.New("branch is required")
	}

	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(repo.Url)
	if err != nil {
		return nil, fmt.Errorf("invalid repository URL: %w", err)
	}

	query := url.Values{}
	query.Set("includeContentMetadata", "true")
	query.Set("recursionLevel", "Full")
	query.Set("scopePath", "/")
	query.Set("versionDescriptor.version", branch)
	query.Set("versionDescriptor.versionType", "branch")
	apiURL := buildAzureDevOpsAPIURL(parsedRepoURL, fmt.Sprintf(
		"_apis/git/repositories/%s/items",
		url.PathEscape(parsedRepoURL.repository),
	), query)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build Azure DevOps request: %w", err)
	}
	addAzureDevOpsHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Azure DevOps repository tree: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed azureDevOpsItemsResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("failed to decode Azure DevOps tree response: %w", err)
		}

		entries := make([]TreeEntry, 0, len(parsed.Value))
		for _, item := range parsed.Value {
			path := strings.TrimPrefix(item.Path, "/")
			if path == "" {
				continue
			}

			switch {
			case item.IsFolder || item.GitObjectType == "tree":
				entries = append(entries, TreeEntry{Path: path, Type: TreeEntryTypeDir})
			case item.GitObjectType == "blob":
				entries = append(entries, TreeEntry{Path: path, Type: TreeEntryTypeFile})
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
		return nil, fmt.Errorf("azure DevOps API returned unexpected status: %d", resp.StatusCode)
	}
}

func (azureDevOpsProvider) GetFileContent(ctx context.Context, repo *models.Repository, ref string, path string) (string, error) {
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

	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(repo.Url)
	if err != nil {
		return "", fmt.Errorf("invalid repository URL: %w", err)
	}

	query := url.Values{}
	query.Set("includeContent", "true")
	query.Set("path", "/"+normalizedPath)
	query.Set("versionDescriptor.version", ref)
	query.Set("versionDescriptor.versionType", "branch")
	apiURL := buildAzureDevOpsAPIURL(parsedRepoURL, fmt.Sprintf(
		"_apis/git/repositories/%s/items",
		url.PathEscape(parsedRepoURL.repository),
	), query)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build Azure DevOps request: %w", err)
	}
	addAzureDevOpsHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Azure DevOps file content: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed azureDevOpsFileResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return "", fmt.Errorf("failed to decode Azure DevOps file response: %w", err)
		}

		return parsed.Content, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return "", errors.New("file not found in repository branch or access denied")
	default:
		return "", fmt.Errorf("azure DevOps API returned unexpected status: %d", resp.StatusCode)
	}
}

func (azureDevOpsProvider) GetLatestCommit(ctx context.Context, repo *models.Repository, branch string) (CommitInfo, error) {
	if repo == nil {
		return CommitInfo{}, errors.New("repository is required")
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return CommitInfo{}, errors.New("branch is required")
	}

	parsedRepoURL, err := parseAzureDevOpsRepositoryURL(repo.Url)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("invalid repository URL: %w", err)
	}

	query := url.Values{}
	query.Set("$top", "1")
	query.Set("searchCriteria.itemVersion.version", branch)
	query.Set("searchCriteria.itemVersion.versionType", "branch")
	apiURL := buildAzureDevOpsAPIURL(parsedRepoURL, fmt.Sprintf(
		"_apis/git/repositories/%s/commits",
		url.PathEscape(parsedRepoURL.repository),
	), query)

	req, err := httpclient.NewRequest(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to build Azure DevOps request: %w", err)
	}
	addAzureDevOpsHeaders(req, repo)

	resp, err := httpclient.Default.Do(req)
	if err != nil {
		return CommitInfo{}, fmt.Errorf("failed to fetch Azure DevOps commit: %w", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			fmt.Printf("warning: failed to close Azure DevOps response body: %v\n", err)
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed azureDevOpsCommitsResponse
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return CommitInfo{}, fmt.Errorf("failed to decode Azure DevOps commit response: %w", err)
		}

		if len(parsed.Value) == 0 {
			return CommitInfo{}, errors.New("no commits found for branch")
		}

		commit := parsed.Value[0]
		if commit.CommitID == "" {
			return CommitInfo{}, errors.New("missing commit hash in Azure DevOps response")
		}

		return CommitInfo{Hash: commit.CommitID, Message: commit.Comment}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return CommitInfo{}, errors.New("authentication failed or access denied")
	case http.StatusNotFound:
		return CommitInfo{}, errors.New("repository or branch not found or access denied")
	default:
		return CommitInfo{}, fmt.Errorf("azure DevOps API returned unexpected status: %d", resp.StatusCode)
	}
}

func buildAzureDevOpsAPIURL(parsedRepoURL parsedAzureDevOpsRepositoryURL, apiPath string, query url.Values) string {
	if query == nil {
		query = url.Values{}
	}
	query.Set("api-version", azureDevOpsAPIVersion)

	return fmt.Sprintf(
		"%s/%s/%s?%s",
		parsedRepoURL.baseURL,
		url.PathEscape(parsedRepoURL.project),
		strings.TrimPrefix(apiPath, "/"),
		query.Encode(),
	)
}

func addAzureDevOpsHeaders(req *http.Request, repo *models.Repository) {
	req.Header.Set("Accept", "application/json")

	if repo.AuthMethod == models.AuthMethodToken && repo.AuthToken != nil {
		token := strings.TrimSpace(repo.AuthToken.String())
		if token != "" {
			encodedToken := base64.StdEncoding.EncodeToString([]byte(":" + token))
			req.Header.Set("Authorization", "Basic "+encodedToken)
		}
	}
}
