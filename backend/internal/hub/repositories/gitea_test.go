package repositories

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
)

const (
	testGiteaRepoURL         = "https://gitea.com/OrcaCD/orca-cd"
	testGiteaSelfHostedURL   = "https://gitea.example.com/OrcaCD/orca-cd"
	testGiteaRepoAPIURL      = "https://gitea.com/api/v1/repos/OrcaCD/orca-cd"
	testGiteaSelfHostAPIURL  = "https://gitea.example.com/api/v1/repos/OrcaCD/orca-cd"
	testGiteaTokenHeaderName = "Authorization"
)

func TestGiteaParseURL(t *testing.T) {
	p := giteaProvider{}

	valid := []struct {
		url   string
		owner string
		repo  string
	}{
		{"https://gitea.com/owner/repo", "owner", "repo"},
		{"https://gitea.com/owner/repo.git", "owner", "repo"},
		{"https://gitea.com/my-org/my-repo", "my-org", "my-repo"},
		{"https://gitea.com/OrcaCD/orca-cd", "OrcaCD", "orca-cd"},
		{"https://gitea.example.com/owner/repo", "owner", "repo"},
		{"https://code.mycompany.io/owner/repo", "owner", "repo"},
		{"https://gitea.com/owner/repo_name", "owner", "repo_name"},
		{"https://gitea.com/owner/repo.name", "owner", "repo.name"},
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
		"http://gitea.com/owner/repo",        // http not allowed
		"ftp://gitea.com/owner/repo",         // wrong scheme
		"https://gitea.com/owner",            // missing repo
		"https://gitea.com/",                 // missing owner and repo
		"https://gitea.com",                  // missing path
		"https:///owner/repo",                // empty host
		"https://gitea.com/owner/repo/extra", // extra path segment
		"https://gitea.com/-owner/repo",      // invalid owner
		"https://gitea.com/owner/re po",      // invalid repo
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
	}
}

func TestGiteaSupportedAuthMethods(t *testing.T) {
	p := giteaProvider{}
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

func TestGiteaTestConnection(t *testing.T) {
	p := giteaProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	t.Run("success without auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testGiteaRepoAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			if req.Header.Get(testGiteaTokenHeaderName) != "" {
				t.Fatalf("expected no authorization header, got %q", req.Header.Get(testGiteaTokenHeaderName))
			}
			return jsonResponse(http.StatusOK), nil
		})

		repo := &models.Repository{
			Url:        testGiteaRepoURL,
			AuthMethod: models.AuthMethodNone,
		}

		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with token auth", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get(testGiteaTokenHeaderName) != "token secret-token" {
				t.Fatalf("unexpected authorization header: %q", req.Header.Get(testGiteaTokenHeaderName))
			}
			return jsonResponse(http.StatusOK), nil
		})

		token := crypto.EncryptedString("secret-token")
		repo := &models.Repository{
			Url:        testGiteaRepoURL,
			AuthMethod: models.AuthMethodToken,
			AuthToken:  &token,
		}

		if err := p.TestConnection(context.Background(), repo); err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})

	t.Run("success with self-hosted instance", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != testGiteaSelfHostAPIURL {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			return jsonResponse(http.StatusOK), nil
		})

		repo := &models.Repository{
			Url:        testGiteaSelfHostedURL,
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
		repo := &models.Repository{Url: "https://gitea.com/invalid-only-owner"}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "invalid repository URL") {
			t.Fatalf("expected invalid repository URL error, got: %v", err)
		}
	})

	t.Run("not found response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound), nil
		})

		repo := &models.Repository{Url: testGiteaRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "repository not found or access denied") {
			t.Fatalf("expected not found/access denied error, got: %v", err)
		}
	})

	t.Run("unauthorized response", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusUnauthorized), nil
		})

		repo := &models.Repository{Url: testGiteaRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "authentication failed or access denied") {
			t.Fatalf("expected auth error, got: %v", err)
		}
	})

	t.Run("unexpected status code", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError), nil
		})

		repo := &models.Repository{Url: testGiteaRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d", http.StatusInternalServerError)) {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})

		repo := &models.Repository{Url: testGiteaRepoURL}
		err := p.TestConnection(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to connect to Gitea") {
			t.Fatalf("expected connect error, got: %v", err)
		}
	})
}

func TestGiteaListBranches(t *testing.T) {
	p := giteaProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := "https://gitea.com/api/v1/repos/OrcaCD/orca-cd/branches?page=1&limit=100"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		if req.Header.Get(testGiteaTokenHeaderName) != "token secret-token" {
			t.Fatalf("unexpected authorization header: %q", req.Header.Get(testGiteaTokenHeaderName))
		}

		return jsonResponseWithBody(http.StatusOK, `[{"name":"release"},{"name":"main"}]`), nil
	})

	token := crypto.EncryptedString("secret-token")
	repo := &models.Repository{
		Url:        testGiteaRepoURL,
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

func TestGiteaListBranchesPagination(t *testing.T) {
	p := giteaProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	requestCount := 0
	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		requestCount++

		switch requestCount {
		case 1:
			expectedURL := "https://gitea.com/api/v1/repos/OrcaCD/orca-cd/branches?page=1&limit=100"
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
			expectedURL := "https://gitea.com/api/v1/repos/OrcaCD/orca-cd/branches?page=2&limit=100"
			if req.URL.String() != expectedURL {
				t.Fatalf("unexpected page 2 URL: %s", req.URL.String())
			}
			return jsonResponseWithBody(http.StatusOK, `[{"name":"main"}]`), nil
		default:
			t.Fatalf("unexpected request count: %d", requestCount)
			return nil, nil
		}
	})

	repo := &models.Repository{Url: testGiteaRepoURL, AuthMethod: models.AuthMethodNone}
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

func TestGiteaListBranchesErrors(t *testing.T) {
	p := giteaProvider{}
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
		repo := &models.Repository{Url: "https://gitea.com/owner"}
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

		repo := &models.Repository{Url: testGiteaRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to fetch Gitea branches") {
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

		repo := &models.Repository{Url: testGiteaRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "failed to decode Gitea branches response") {
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

		repo := &models.Repository{Url: testGiteaRepoURL}
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

		repo := &models.Repository{Url: testGiteaRepoURL}
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

		repo := &models.Repository{Url: testGiteaRepoURL}
		branches, err := p.ListBranches(context.Background(), repo)
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if branches != nil {
			t.Fatalf("expected nil branches, got %v", branches)
		}
	})
}

func TestGiteaListTree(t *testing.T) {
	p := giteaProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := "https://gitea.com/api/v1/repos/OrcaCD/orca-cd/git/trees/release%2Fprod?recursive=true"
		if req.URL.String() != expectedURL {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}

		return jsonResponseWithBody(http.StatusOK, `{
			"tree": [
				{"path":"docker-compose.yml","type":"blob"},
				{"path":"services","type":"tree"},
				{"path":"ignored","type":"commit"},
				{"path":"","type":"blob"}
			]
		}`), nil
	})

	repo := &models.Repository{Url: testGiteaRepoURL, AuthMethod: models.AuthMethodNone}
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

func TestGiteaListTreeErrors(t *testing.T) {
	p := giteaProvider{}
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
		repo := &models.Repository{Url: testGiteaRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "   ")
		if err == nil || !strings.Contains(err.Error(), "branch is required") {
			t.Fatalf("expected branch is required error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})

	t.Run("invalid repository URL", func(t *testing.T) {
		repo := &models.Repository{Url: "https://gitea.com/owner"}
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
			return nil, errors.New("dial tcp timeout")
		})

		repo := &models.Repository{Url: testGiteaRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to fetch Gitea repository tree") {
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

		repo := &models.Repository{Url: testGiteaRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "failed to decode Gitea tree response") {
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

		repo := &models.Repository{Url: testGiteaRepoURL}
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

		repo := &models.Repository{Url: testGiteaRepoURL}
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

		repo := &models.Repository{Url: testGiteaRepoURL}
		tree, err := p.ListTree(context.Background(), repo, "main")
		if err == nil || !strings.Contains(err.Error(), "unexpected status") {
			t.Fatalf("expected unexpected status error, got: %v", err)
		}
		if tree != nil {
			t.Fatalf("expected nil tree, got %v", tree)
		}
	})
}
