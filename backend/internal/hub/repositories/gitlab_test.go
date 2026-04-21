package repositories

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const (
	testGitLabRepoURL        = "https://gitlab.com/OrcaCD/orca-cd"
	testGitLabSelfHostedURL  = "https://gitlab.example.com/OrcaCD/orca-cd"
	testGitLabNestedURL      = "https://gitlab.com/group/subgroup/orca-cd"
	testGitLabRepoAPIURL     = "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd"
	testGitLabNestedAPIURL   = "https://gitlab.com/api/v4/projects/group%2Fsubgroup%2Forca-cd"
	testGitLabSelfHostAPIURL = "https://gitlab.example.com/api/v4/projects/OrcaCD%2Forca-cd"
)

func TestGitLabParseURL(t *testing.T) {
	p := gitlabProvider{}

	valid := []struct {
		url       string
		namespace string
		project   string
	}{
		{"https://gitlab.com/owner/repo", "owner", "repo"},
		{"https://gitlab.com/owner/repo.git", "owner", "repo"},
		{"https://gitlab.com/my-org/my-repo", "my-org", "my-repo"},
		{"https://gitlab.com/OrcaCD/orca-cd", "OrcaCD", "orca-cd"},
		{"https://gitlab.com/a/b", "a", "b"},
		{"https://gitlab.com/owner/repo_name", "owner", "repo_name"},
		{"https://gitlab.com/owner/repo.name", "owner", "repo.name"},
		// Self-hosted instances
		{"https://gitlab.example.com/owner/repo", "owner", "repo"},
		{"https://git.mycompany.io/owner/repo", "owner", "repo"},
		// Nested namespaces
		{"https://gitlab.com/group/subgroup/project", "group/subgroup", "project"},
		{"https://gitlab.com/a/b/c/d", "a/b/c", "d"},
	}

	for _, tc := range valid {
		ns, proj, err := p.ParseURL(tc.url)
		if err != nil {
			t.Errorf("expected %q to be valid, got error: %v", tc.url, err)
			continue
		}
		if ns != tc.namespace {
			t.Errorf("URL %q: expected namespace %q, got %q", tc.url, tc.namespace, ns)
		}
		if proj != tc.project {
			t.Errorf("URL %q: expected project %q, got %q", tc.url, tc.project, proj)
		}
	}

	invalid := []string{
		"",
		"not-a-url",
		"http://gitlab.com/owner/repo",   // http not allowed
		"ftp://gitlab.com/owner/repo",    // wrong scheme
		"https://gitlab.com/owner",       // missing project
		"https://gitlab.com/",            // missing namespace and project
		"https://gitlab.com",             // missing path
		"https:///owner/repo",            // empty host
		"https://github.com/owner/repo",  // github.com not allowed
		"https://gitlab.com/owner//repo", // empty path segment
		"https://gitlab.com/owner/re po", // invalid project name
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
	}
}

func TestGitLabSupportedAuthMethods(t *testing.T) {
	p := gitlabProvider{}
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

func TestGitLabTestConnection(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("success without auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testGitLabRepoAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "" {
				t.Fatalf("expected no authorization header, got %q", req.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{
			Url:        testGitLabRepoURL,
			AuthMethod: models.AuthMethodNone,
		}

		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with token auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer secret-token" {
				t.Fatalf("unexpected authorization header: %q", req.Header.Get("Authorization"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		token := crypto.EncryptedString("secret-token")
		repo := &models.Repository{
			Url:        testGitLabRepoURL,
			AuthMethod: models.AuthMethodToken,
			AuthToken:  &token,
		}

		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with self-hosted instance", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testGitLabSelfHostAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{
			Url:        testGitLabSelfHostedURL,
			AuthMethod: models.AuthMethodNone,
		}

		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with nested namespace", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testGitLabNestedAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{
			Url:        testGitLabNestedURL,
			AuthMethod: models.AuthMethodNone,
		}

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
		repo := &models.Repository{Url: "https://gitlab.com/invalid-only-namespace"}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found/access denied error, got: %v", err)
		}
	})

	t.Run("unauthorized response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("unexpected status code", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
			}, nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", http.StatusInternalServerError)) {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to connect to GitLab") {
			t.Fatalf("expected connect error, got: %v", err)
		}
	})
}

func TestGitLabListBranches(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/branches?per_page=100&page=1"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `[{"name":"release"},{"name":"main"}]`), nil
	})

	repo := &models.Repository{Url: testGitLabRepoURL, AuthMethod: models.AuthMethodNone}
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

func TestGitLabListBranchesPagination(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++

		switch requestCount {
		case 1:
			expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/branches?per_page=100&page=1"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 1 URL: %s", req.URL.String())
			}
			resp := jsonResponseWithBody(http.StatusOK, `[{"name":"branch-z"}]`)
			resp.Header.Set("X-Next-Page", "2")
			return resp, nil
		case 2:
			expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/branches?per_page=100&page=2"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 2 URL: %s", req.URL.String())
			}
			resp := jsonResponseWithBody(http.StatusOK, `[{"name":"branch-a"}]`)
			resp.Header.Set("X-Next-Page", "not-a-number")
			return resp, nil
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
			return nil, nil
		}
	})

	repo := &models.Repository{Url: testGitLabRepoURL}
	branches, err := p.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches))
	}
	if branches[0] != "branch-a" || branches[1] != "branch-z" {
		t.Fatalf("expected sorted branches [branch-a branch-z], got %v", branches)
	}
}

func TestGitLabListBranchesErrors(t *testing.T) {
	p := gitlabProvider{}
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
		repo := &models.Repository{Url: "https://gitlab.com/owner"}
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
			return nil, errors.New("dial timeout")
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitLab branches") {
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

		repo := &models.Repository{Url: testGitLabRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitLab branches response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})
}

func TestGitLabListTree(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++

		switch requestCount {
		case 1:
			expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/tree?ref=release%2Fprod&recursive=true&per_page=100&page=1"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 1 URL: %s", req.URL.String())
			}

			resp := jsonResponseWithBody(http.StatusOK, `[{"path":"services","type":"tree"}]`)
			resp.Header.Set("X-Next-Page", "2")
			return resp, nil
		case 2:
			expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/tree?ref=release%2Fprod&recursive=true&per_page=100&page=2"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 2 URL: %s", req.URL.String())
			}

			resp := jsonResponseWithBody(http.StatusOK, `[
				{"path":"docker-compose.yml","type":"blob"},
				{"path":"","type":"blob"},
				{"path":"ignored","type":"commit"}
			]`)
			resp.Header.Set("X-Next-Page", "abc")
			return resp, nil
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
			return nil, nil
		}
	})

	repo := &models.Repository{Url: testGitLabRepoURL, AuthMethod: models.AuthMethodNone}
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

func TestGitLabListTreeErrors(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("nil repository", func(t *testing.T) {
		tree, err := p.ListTree(context.Background(), nil, "main")
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, " ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://gitlab.com/owner"}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection failed")
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitLab repository tree") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitLab tree response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})
}

func TestGitLabListBranchesAuthHeader(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	token := crypto.EncryptedString("secret-token")
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("unexpected authorization header: %q", req.Header.Get("Authorization"))
		}
		return jsonResponseWithBody(http.StatusOK, `[{"name":"main"}]`), nil
	})

	repo := &models.Repository{
		Url:        testGitLabRepoURL,
		AuthMethod: models.AuthMethodToken,
		AuthToken:  &token,
	}

	branches, err := p.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(branches) != 1 || branches[0] != "main" {
		t.Fatalf("expected [main], got %v", branches)
	}
}

func TestGitLabListTreeNextPageInvalidStops(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if requestCount != 1 {
			t.Fatalf("expected single request, got %d", requestCount)
		}

		resp := jsonResponseWithBody(http.StatusOK, `[{"path":"app.yaml","type":"blob"}]`)
		resp.Header.Set("X-Next-Page", strconv.Itoa(0))
		return resp, nil
	})

	repo := &models.Repository{Url: testGitLabRepoURL}
	tree, err := p.ListTree(context.Background(), repo, "main")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(tree) != 1 || tree[0].Path != "app.yaml" {
		t.Fatalf("unexpected tree: %v", tree)
	}
}

func TestGitLabGetFileContent(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	encodedContent := base64.StdEncoding.EncodeToString([]byte("version: \"3.9\"\nservices:\n  api:\n    image: app:v1\n"))

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/files/services%2Fbilling.yml?ref=release%2Fprod"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `{"content":"`+encodedContent+`","encoding":"base64"}`), nil
	})

	repo := &models.Repository{Url: testGitLabRepoURL, AuthMethod: models.AuthMethodNone}
	content, err := p.GetFileContent(context.Background(), repo, "release/prod", "services/billing.yml")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if !strings.Contains(content, "services:") {
		t.Fatalf("expected decoded compose content, got %q", content)
	}
}

func TestGitLabGetLatestCommit(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := "https://gitlab.com/api/v4/projects/OrcaCD%2Forca-cd/repository/commits?ref_name=release%2Fprod&per_page=1"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `[{"id":"abc123","message":"chore: update compose"}]`), nil
	})

	repo := &models.Repository{Url: testGitLabRepoURL, AuthMethod: models.AuthMethodNone}
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

func TestGitLabGetLatestCommitErrors(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, " ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
	})

	t.Run("no commits in response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `[]`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "no commits found") {
			t.Fatalf("expected no commits error, got: %v", err)
		}
	})
}

func TestGitLabGetFileContentErrors(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("nil repository", func(t *testing.T) {
		content, err := p.GetFileContent(context.Background(), nil, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "   ", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("path is required", func(t *testing.T) {
		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "   ")
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://gitlab.com/owner"}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitLab file content") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitLab file response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("unsupported encoding", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"content":"hello","encoding":"utf-8"}`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "unsupported GitLab file encoding") {
			t.Fatalf("expected unsupported encoding error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"content":"***","encoding":"base64"}`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitLab file content") {
			t.Fatalf("expected invalid base64 error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "missing.yml")
		if err == nil || !strings.Contains(err.Error(), "file not found") {
			t.Fatalf("expected file not found error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})
}

func TestGitLabGetLatestCommitAdditionalErrors(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("nil repository", func(t *testing.T) {
		_, err := p.GetLatestCommit(context.Background(), nil, "main")
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://gitlab.com/owner"}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitLab commit") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitLab commit response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
	})

	t.Run("missing commit hash", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `[{"id":"","message":"msg"}]`), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "missing commit hash") {
			t.Fatalf("expected missing hash error, got: %v", err)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "repository or branch not found") {
			t.Fatalf("expected not found error, got: %v", err)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testGitLabRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
	})
}

func TestGitLabGetLatestCommitUsesTitleWhenMessageEmpty(t *testing.T) {
	p := gitlabProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		return jsonResponseWithBody(http.StatusOK, `[{"id":"abc123","message":"","title":"fallback title"}]`), nil
	})

	repo := &models.Repository{Url: testGitLabRepoURL}
	commit, err := p.GetLatestCommit(context.Background(), repo, "main")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if commit.Hash != "abc123" {
		t.Fatalf("expected hash abc123, got %q", commit.Hash)
	}
	if commit.Message != "fallback title" {
		t.Fatalf("expected title fallback message, got %q", commit.Message)
	}
}
