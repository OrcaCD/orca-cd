package repositories

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
		"http://gitlab.com/owner/repo",  // http not allowed
		"ftp://gitlab.com/owner/repo",   // wrong scheme
		"https://gitlab.com/owner",      // missing project
		"https://gitlab.com/",           // missing namespace and project
		"https://gitlab.com",            // missing path
		"https:///owner/repo",           // empty host
		"https://github.com/owner/repo", // github.com not allowed
	}

	for _, u := range invalid {
		if _, _, err := p.ParseURL(u); err == nil {
			t.Errorf("expected %q to be invalid, but got no error", u)
		}
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
}
