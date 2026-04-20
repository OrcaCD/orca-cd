package repositories

import (
	"context"
	"io"
	"net/http"
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
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
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
}

func TestGitHubListBranches(t *testing.T) {
	p := githubProvider{}
	originalClient := httpclient.Default
	t.Cleanup(func() {
		httpclient.Default = originalClient
	})

	httpclient.Default = mockClient(func(req *http.Request) (*http.Response, error) {
		expectedURL := githubAPIBase + "/repos/OrcaCD/orca-cd/branches?per_page=100"
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
				{"path":"README.md","type":"blob"}
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
