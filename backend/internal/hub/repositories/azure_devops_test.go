package repositories

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const (
	testAzureDevOpsRepoURL = "https://dev.azure.com/OrcaCD/Platform/_git/orca-cd"
	testAzureDevOpsAPIPath = "/OrcaCD/Platform/_apis/git/repositories/orca-cd"
)

func TestAzureDevOpsParseURL(t *testing.T) {
	p := azureDevOpsProvider{}

	valid := []struct {
		url   string
		owner string
		repo  string
	}{
		{"https://dev.azure.com/OrcaCD/Platform/_git/orca-cd", "OrcaCD/Platform", "orca-cd"},
		{"https://dev.azure.com/OrcaCD/Platform/_git/orca-cd.git", "OrcaCD/Platform", "orca-cd"},
		{"https://dev.azure.com/my-org/My%20Project/_git/my%20repo", "my-org/My Project", "my repo"},
		{"https://my-org.visualstudio.com/Platform/_git/orca-cd", "my-org/Platform", "orca-cd"},
		{"https://my-org.visualstudio.com/My%20Project/_git/my%20repo.git", "my-org/My Project", "my repo"},
	}

	for _, tc := range valid {
		owner, repo, err := p.ParseURL(tc.url)
		if err != nil {
			t.Errorf("expected %q to be valid, got error: %v", tc.url, err)
			continue
		}
		if owner != tc.owner {
			t.Errorf("URL %q: expected owner %q, got %q", tc.url, tc.owner, owner)
		}
		if repo != tc.repo {
			t.Errorf("URL %q: expected repo %q, got %q", tc.url, tc.repo, repo)
		}
	}

	invalid := []string{
		"",
		"not-a-url",
		"http://dev.azure.com/OrcaCD/Platform/_git/orca-cd",
		"https://github.com/OrcaCD/Platform/_git/orca-cd",
		"https://dev.azure.com/OrcaCD/Platform/orca-cd",
		"https://dev.azure.com/OrcaCD/Platform/_git",
		"https://dev.azure.com/OrcaCD/Platform/_git/orca-cd/extra",
		"https://dev.azure.com/-OrcaCD/Platform/_git/orca-cd",
		"https://dev.azure.com/OrcaCD//_git/orca-cd",
		"https://dev.azure.com/OrcaCD/Platform/_git/repo%2Fname",
		"https://visualstudio.com/Platform/_git/orca-cd",
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
	}
}

func TestAzureDevOpsSupportedAuthMethods(t *testing.T) {
	p := azureDevOpsProvider{}
	got := p.SupportedAuthMethods()

	if len(got) != 2 {
		t.Fatalf("expected 2 auth methods, got %d", len(got))
	}
	if got[0] != models.AuthMethodNone {
		t.Fatalf("expected first auth method to be %q, got %q", models.AuthMethodNone, got[0])
	}
	if got[1] != models.AuthMethodToken {
		t.Fatalf("expected second auth method to be %q, got %q", models.AuthMethodToken, got[1])
	}
}

func TestAzureDevOpsTestConnection(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("success without auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath)
			if req.Header.Get("Authorization") != "" {
				t.Fatalf("expected no authorization header, got %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodNone}
		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with token auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":secret-token"))
			if req.Header.Get("Authorization") != expectedAuth {
				t.Fatalf("unexpected authorization header: %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK), nil
		})

		token := crypto.EncryptedString("secret-token")
		repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodToken, AuthToken: &token}
		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("nil repository", func(t *testing.T) {
		err := p.TestConnection(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://dev.azure.com/OrcaCD/Platform/orca-cd"}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})
}

func TestAzureDevOpsListBranches(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	token := crypto.EncryptedString("secret-token")
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath+"/refs")
		query := req.URL.Query()
		if query.Get("filter") != "heads/" {
			t.Fatalf("expected heads filter, got %q", query.Get("filter"))
		}
		if query.Get("$top") != "100" {
			t.Fatalf("expected top 100, got %q", query.Get("$top"))
		}
		if req.Header.Get("Authorization") == "" {
			t.Fatal("expected authorization header")
		}

		return jsonResponseWithBody(http.StatusOK, `{"value":[{"name":"refs/heads/release"},{"name":"refs/heads/main"},{"name":"refs/tags/v1"}]}`), nil
	})

	repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodToken, AuthToken: &token}
	branches, err := p.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}
	if branches[0] != "main" || branches[1] != "release" {
		t.Fatalf("expected sorted branches [main release], got %v", branches)
	}
}

func TestAzureDevOpsListBranchesPagination(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++
		assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath+"/refs")

		switch requestCount {
		case 1:
			if req.URL.Query().Get("continuationToken") != "" {
				t.Fatalf("unexpected continuation token on first request: %q", req.URL.Query().Get("continuationToken"))
			}
			resp := jsonResponseWithBody(http.StatusOK, `{"value":[{"name":"refs/heads/release"}]}`)
			resp.Header.Set("x-ms-continuationtoken", "next-page")
			return resp, nil
		case 2:
			if req.URL.Query().Get("continuationToken") != "next-page" {
				t.Fatalf("expected continuation token next-page, got %q", req.URL.Query().Get("continuationToken"))
			}
			return jsonResponseWithBody(http.StatusOK, `{"value":[{"name":"refs/heads/main"}]}`), nil
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
			return nil, nil
		}
	})

	repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodNone}
	branches, err := p.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected 2 requests, got %d", requestCount)
	}
	if len(branches) != 2 || branches[0] != "main" || branches[1] != "release" {
		t.Fatalf("expected sorted branches [main release], got %v", branches)
	}
}

func TestAzureDevOpsListBranchesErrors(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("nil repository", func(t *testing.T) {
		branches, err := p.ListBranches(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://dev.azure.com/OrcaCD/Platform/orca-cd"}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("timeout")
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to fetch azure devops branches") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to decode azure devops branches response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found/access denied error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})
}

func TestAzureDevOpsListTree(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath+"/items")
		query := req.URL.Query()
		if query.Get("scopePath") != "/" {
			t.Fatalf("expected root scope path, got %q", query.Get("scopePath"))
		}
		if query.Get("recursionLevel") != "Full" {
			t.Fatalf("expected Full recursion, got %q", query.Get("recursionLevel"))
		}
		if query.Get("versionDescriptor.version") != "release/prod" {
			t.Fatalf("unexpected branch query: %q", query.Get("versionDescriptor.version"))
		}

		return jsonResponseWithBody(http.StatusOK, `{
			"value": [
				{"path":"/","isFolder":true,"gitObjectType":"tree"},
				{"path":"/docker-compose.yml","gitObjectType":"blob"},
				{"path":"/services","isFolder":true,"gitObjectType":"tree"},
				{"path":"/ignored","gitObjectType":"commit"}
			]
		}`), nil
	})

	repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodNone}
	tree, err := p.ListTree(context.Background(), repo, "release/prod")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(tree))
	}

	byPath := map[string]TreeEntryType{}
	for _, entry := range tree {
		byPath[entry.Path] = entry.Type
	}
	if byPath["services"] != TreeEntryTypeDir {
		t.Fatalf("expected services to be dir, got %q", byPath["services"])
	}
	if byPath["docker-compose.yml"] != TreeEntryTypeFile {
		t.Fatalf("expected docker-compose.yml to be file, got %q", byPath["docker-compose.yml"])
	}
}

func TestAzureDevOpsListTreeErrors(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "   ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"value": [invalid-json]}`), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode azure devops tree response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})
}

func TestAzureDevOpsGetFileContent(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath+"/items")
		query := req.URL.Query()
		if query.Get("path") != "/services/billing.yml" {
			t.Fatalf("unexpected file path query: %q", query.Get("path"))
		}
		if query.Get("includeContent") != "true" {
			t.Fatalf("expected includeContent=true, got %q", query.Get("includeContent"))
		}
		if query.Get("versionDescriptor.version") != "release/prod" {
			t.Fatalf("unexpected ref query: %q", query.Get("versionDescriptor.version"))
		}

		return jsonResponseWithBody(http.StatusOK, `{"content":"version: \"3.9\"\nservices:\n  api:\n    image: app:v1\n"}`), nil
	})

	repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodNone}
	content, err := p.GetFileContent(context.Background(), repo, "release/prod", "services/billing.yml")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if !strings.Contains(content, "services:") {
		t.Fatalf("expected compose content, got %q", content)
	}
}

func TestAzureDevOpsGetFileContentErrors(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("path is required", func(t *testing.T) {
		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "   ")
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "missing.yml")
		if err == nil || !strings.Contains(err.Error(), "file not found") {
			t.Fatalf("expected file not found error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})
}

func TestAzureDevOpsGetLatestCommit(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		assertAzureDevOpsRequest(t, req, testAzureDevOpsAPIPath+"/commits")
		query := req.URL.Query()
		if query.Get("$top") != "1" {
			t.Fatalf("expected top=1, got %q", query.Get("$top"))
		}
		if query.Get("searchCriteria.itemVersion.version") != "release/prod" {
			t.Fatalf("unexpected branch query: %q", query.Get("searchCriteria.itemVersion.version"))
		}

		return jsonResponseWithBody(http.StatusOK, `{"value":[{"commitId":"abc123","comment":"chore: update compose"}]}`), nil
	})

	repo := &models.Repository{Url: testAzureDevOpsRepoURL, AuthMethod: models.AuthMethodNone}
	commit, err := p.GetLatestCommit(context.Background(), repo, "release/prod")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if commit.Hash != "abc123" {
		t.Fatalf("expected commit hash %q, got %q", "abc123", commit.Hash)
	}
	if commit.Message != "chore: update compose" {
		t.Fatalf("expected commit message %q, got %q", "chore: update compose", commit.Message)
	}
}

func TestAzureDevOpsGetLatestCommitErrors(t *testing.T) {
	p := azureDevOpsProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, " ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
	})

	t.Run("empty commit list", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"value":[]}`), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "no commits found") {
			t.Fatalf("expected no commits found error, got: %v", err)
		}
	})

	t.Run("missing commit hash", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"value":[{"comment":"missing hash"}]}`), nil
		})

		repo := &models.Repository{Url: testAzureDevOpsRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "missing commit hash") {
			t.Fatalf("expected missing commit hash error, got: %v", err)
		}
	})
}

func assertAzureDevOpsRequest(t *testing.T, req *http.Request, expectedPath string) {
	t.Helper()

	if req.URL.Scheme != "https" {
		t.Fatalf("expected https scheme, got %q", req.URL.Scheme)
	}
	if req.URL.Host != "dev.azure.com" {
		t.Fatalf("expected dev.azure.com host, got %q", req.URL.Host)
	}
	if req.URL.Path != expectedPath {
		t.Fatalf("expected path %q, got %q", expectedPath, req.URL.Path)
	}
	if req.URL.Query().Get("api-version") != azureDevOpsAPIVersion {
		t.Fatalf("expected api version %q, got %q", azureDevOpsAPIVersion, req.URL.Query().Get("api-version"))
	}
	if req.Header.Get("Accept") != "application/json" {
		t.Fatalf("expected JSON accept header, got %q", req.Header.Get("Accept"))
	}
}
