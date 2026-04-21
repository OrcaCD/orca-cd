package repositories

import (
	"context"
	"encoding/base64"
	"errors"
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
	testRepoURL    = "https://github.com/OrcaCD/orca-cd"
	testRepoAPIURL = githubAPIBase + "/repos/OrcaCD/orca-cd"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func jsonResponse(code int) *http.Response {
	return jsonResponseWithBody(code, "{}")
}

func jsonResponseWithBody(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestGitHubParseURL(t *testing.T) {
	p := githubProvider{}

	valid := []string{
		"https://github.com/owner/repo",
		"https://github.com/owner/repo.git",
		"https://github.com/my-org/my-repo",
		"https://github.com/OrcaCD/orca-cd",
		"https://github.com/a/b",
		"https://github.com/owner/repo_name",
		"https://github.com/owner/repo.name",
	}

	for _, u := range valid {
		if _, _, err := p.ParseURL(u); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", u, err)
		}
	}

	invalid := []string{
		"",
		"not-a-url",
		"http://github.com/owner/repo",        // http, not https
		"https://gitlab.com/owner/repo",       // wrong host
		"https://github.com/owner",            // missing repo
		"https://github.com/",                 // missing owner and repo
		"https://github.com/owner/repo/extra", // extra path segment
		"ftp://github.com/owner/repo",         // wrong scheme
		"https://github.com/-owner/repo",      // owner starts with hyphen
		"https://github.com/owner-/repo",      // owner ends with hyphen
		"https://github.com/owner/",           // missing repo segment
		"https://github.com/owner/%20repo",    // invalid repo characters
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
	}
}

func TestGitHubSupportedAuthMethods(t *testing.T) {
	p := githubProvider{}
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

func TestGitHubTestConnection(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("success without auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testRepoAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get("Authorization") != "" {
				t.Fatalf("expected no authorization header, got %q", req.Header.Get("Authorization"))
			}
			return jsonResponse(http.StatusOK), nil
		})

		repo := &models.Repository{
			Url:        testRepoURL,
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
			return jsonResponse(http.StatusOK), nil
		})

		token := crypto.EncryptedString("secret-token")
		repo := &models.Repository{
			Url:        testRepoURL,
			AuthMethod: models.AuthMethodToken,
			AuthToken:  &token,
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
		repo := &models.Repository{Url: "https://github.com/invalid-only-owner"}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found/access denied error, got: %v", err)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("unexpected status code", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})

		repo := &models.Repository{Url: testRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to connect to GitHub") {
			t.Fatalf("expected connect error, got: %v", err)
		}
	})
}

func TestGitHubListBranches(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/branches?per_page=100&page=1"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		if req.Header.Get("Authorization") != "Bearer secret-token" {
			t.Fatalf("unexpected authorization header: %q", req.Header.Get("Authorization"))
		}

		return jsonResponseWithBody(http.StatusOK, `[{"name":"release"},{"name":"main"}]`), nil
	})

	token := crypto.EncryptedString("secret-token")
	repo := &models.Repository{
		Url:        testRepoURL,
		AuthMethod: models.AuthMethodToken,
		AuthToken:  &token,
	}

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

func TestGitHubListBranchesPagination(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++

		switch requestCount {
		case 1:
			expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/branches?per_page=100&page=1"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 1 URL: %s", req.URL.String())
			}

			var b strings.Builder
			b.WriteString("[")
			for i := range 100 {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(`{"name":"branch-` + strconv.Itoa(i) + `"}`)
			}
			b.WriteString("]")
			return jsonResponseWithBody(http.StatusOK, b.String()), nil
		case 2:
			expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/branches?per_page=100&page=2"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 2 URL: %s", req.URL.String())
			}
			return jsonResponseWithBody(http.StatusOK, `[{"name":"main"}]`), nil
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
			return nil, nil
		}
	})

	repo := &models.Repository{Url: testRepoURL, AuthMethod: models.AuthMethodNone}
	branches, err := p.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(branches) != 101 {
		t.Fatalf("expected 101 branches, got %d", len(branches))
	}
	if branches[0] != "main" {
		t.Fatalf("expected first sorted branch to be main, got %q", branches[0])
	}
	if branches[1] != "branch-0" {
		t.Fatalf("expected second sorted branch to be branch-0, got %q", branches[1])
	}
}

func TestGitHubListBranchesErrors(t *testing.T) {
	p := githubProvider{}
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
		repo := &models.Repository{Url: "https://github.com/invalid-only-owner"}
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

		repo := &models.Repository{Url: testRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitHub branches") {
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

		repo := &models.Repository{Url: testRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitHub branches response") {
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

		repo := &models.Repository{Url: testRepoURL}
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

		repo := &models.Repository{Url: testRepoURL}
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
			return jsonResponse(http.StatusBadGateway), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})
}

func TestGitHubListTree(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/git/trees/feature%2Fprod?recursive=1"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `{
			"tree": [
				{"path":"docker-compose.yml","type":"blob"},
				{"path":"services","type":"tree"},
				{"path":"README.md","type":"blob"},
				{"path":"submodule","type":"commit"},
				{"path":"","type":"blob"}
			]
		}`), nil
	})

	repo := &models.Repository{Url: testRepoURL, AuthMethod: models.AuthMethodNone}

	tree, err := p.ListTree(context.Background(), repo, "feature/prod")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if len(tree) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(tree))
	}

	byPath := map[string]TreeEntryType{}
	for _, entry := range tree {
		byPath[entry.Path] = entry.Type
	}

	if byPath["docker-compose.yml"] != TreeEntryTypeFile {
		t.Fatalf("expected docker-compose.yml to be file, got %q", byPath["docker-compose.yml"])
	}
	if byPath["services"] != TreeEntryTypeDir {
		t.Fatalf("expected services to be dir, got %q", byPath["services"])
	}
}

func TestGitHubListTreeErrors(t *testing.T) {
	p := githubProvider{}
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
		repo := &models.Repository{Url: testRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "   ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://github.com/invalid-only-owner"}
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
			return nil, errors.New("temporary network error")
		})

		repo := &models.Repository{Url: testRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitHub repository tree") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"tree": [invalid-json]}`), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitHub tree response") {
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

		repo := &models.Repository{Url: testRepoURL}
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

		repo := &models.Repository{Url: testRepoURL}
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
			return jsonResponse(http.StatusBadGateway), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})
}

func TestGitHubGetFileContent(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	encodedContent := base64.StdEncoding.EncodeToString([]byte("version: \"3.9\"\nservices:\n  api:\n    image: app:v1\n"))

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/contents/services%2Fbilling.yml?ref=feature%2Fprod"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `{"content":"`+encodedContent+`","encoding":"base64"}`), nil
	})

	repo := &models.Repository{Url: testRepoURL, AuthMethod: models.AuthMethodNone}
	content, err := p.GetFileContent(context.Background(), repo, "feature/prod", "services/billing.yml")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if !strings.Contains(content, "services:") {
		t.Fatalf("expected decoded compose content, got %q", content)
	}
}

func TestGitHubGetFileContentErrors(t *testing.T) {
	p := githubProvider{}
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

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitHub file content") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "missing.yml")
		if err == nil || !strings.Contains(err.Error(), "file not found") {
			t.Fatalf("expected file not found error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("ref is required", func(t *testing.T) {
		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "   ", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "ref is required") {
			t.Fatalf("expected ref is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("path is required", func(t *testing.T) {
		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "   ")
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("expected path is required error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://github.com/invalid-only-owner"}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitHub file response") {
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

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "unsupported GitHub file encoding") {
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

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitHub file content") {
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

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusBadGateway), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		content, err := p.GetFileContent(context.Background(), repo, "main", "docker-compose.yml")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if content != "" {
			t.Fatalf("expected empty content, got %q", content)
		}
	})
}

func TestGitHubGetLatestCommit(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/commits/feature%2Fprod"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `{"sha":"abc123","commit":{"message":"feat: update compose"}}`), nil
	})

	repo := &models.Repository{Url: testRepoURL, AuthMethod: models.AuthMethodNone}
	commit, err := p.GetLatestCommit(context.Background(), repo, "feature/prod")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if commit.Hash != "abc123" {
		t.Fatalf("expected commit hash %q, got %q", "abc123", commit.Hash)
	}
	if commit.Message != "feat: update compose" {
		t.Fatalf("expected commit message %q, got %q", "feat: update compose", commit.Message)
	}
}

func TestGitHubGetLatestCommitErrors(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("branch is required", func(t *testing.T) {
		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, " ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{invalid-json`), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode GitHub commit response") {
			t.Fatalf("expected decode error, got: %v", err)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "repository or branch not found") {
			t.Fatalf("expected not found error, got: %v", err)
		}
	})

	t.Run("nil repository", func(t *testing.T) {
		_, err := p.GetLatestCommit(context.Background(), nil, "main")
		if err == nil || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("expected repository is required error, got: %v", err)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://github.com/invalid-only-owner"}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch GitHub commit") {
			t.Fatalf("expected fetch error, got: %v", err)
		}
	})

	t.Run("missing commit hash", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponseWithBody(http.StatusOK, `{"sha":"","commit":{"message":"msg"}}`), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "missing commit hash") {
			t.Fatalf("expected missing hash error, got: %v", err)
		}
	})

	t.Run("forbidden response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusForbidden), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("unexpected status", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusBadGateway), nil
		})

		repo := &models.Repository{Url: testRepoURL}
		_, err := p.GetLatestCommit(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
	})
}
