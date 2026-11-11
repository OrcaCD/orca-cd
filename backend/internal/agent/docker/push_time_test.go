package docker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var pushedAt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// serveRegistryAPI points every registry API at a test server.
func serveRegistryAPI(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	origHub, origGitHub, origGitLab, origClient := dockerHubAPI, gitHubAPI, gitLabAPI, registryHTTPClient
	t.Cleanup(func() {
		dockerHubAPI, gitHubAPI, gitLabAPI, registryHTTPClient = origHub, origGitHub, origGitLab, origClient
	})
	dockerHubAPI, gitHubAPI, gitLabAPI = srv.URL, srv.URL, srv.URL
	registryHTTPClient = srv.Client()
	return srv
}

func TestLookupPushTime_DockerHub(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"jwt"}`))
		case "/v2/namespaces/library/repositories/redis/tags/7.4":
			if r.Header.Get("Authorization") != "Bearer jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"digest":"` + newDigest + `","tag_last_pushed":"2026-11-01T12:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	img := registryImage{host: "docker.io", path: "library/redis", tag: "7.4", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{username: "user", password: "pat"})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want %v", got, pushedAt)
	}

	img.digest = oldDigest
	if _, err := lookupPushTime(t.Context(), img, registryCredentials{username: "user", password: "pat"}); err == nil || !strings.Contains(err.Error(), "another digest") {
		t.Errorf("expected a digest mismatch error, got %v", err)
	}
}

func TestLookupPushTime_GHCRFallsBackToUser(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/users/someone/packages/container/app/versions":
			_, _ = w.Write([]byte(`[{"name":"` + oldDigest + `","created_at":"2026-10-01T00:00:00Z"},{"name":"` + newDigest + `","created_at":"2026-11-01T12:00:00Z"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	img := registryImage{host: "ghcr.io", path: "someone/app", tag: "1.0", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{username: "someone", password: "ghp_token"})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want %v", got, pushedAt)
	}

	if _, err := lookupPushTime(t.Context(), img, registryCredentials{}); err == nil || !strings.Contains(err.Error(), "read:packages") {
		t.Errorf("expected an error about missing credentials, got %v", err)
	}
}

func TestLookupPushTime_GitLab(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/group%2Fproject%2Fimage/registry/repositories":
			w.WriteHeader(http.StatusNotFound)
		case "/api/v4/projects/group%2Fproject/registry/repositories":
			_, _ = w.Write([]byte(`[{"id":1,"path":"group/project"},{"id":7,"path":"group/project/image"}]`))
		case "/api/v4/projects/group%2Fproject/registry/repositories/7/tags/v1":
			_, _ = w.Write([]byte(`{"digest":"` + newDigest + `","created_at":"2026-11-01T12:00:00Z"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	img := registryImage{host: "registry.gitlab.com", path: "group/project/image", tag: "v1", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want %v", got, pushedAt)
	}
}

func TestLookupPushTime_Harbor(t *testing.T) {
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "robot" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.EscapedPath() == "/api/v2.0/projects/library/repositories/team%252Fapp/artifacts/"+newDigest {
			_, _ = w.Write([]byte(`{"push_time":"2026-11-01T12:00:00Z"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	host := strings.TrimPrefix(srv.URL, "https://")

	img := registryImage{host: host, path: "library/team/app", tag: "1.0", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{username: "robot", password: "secret"})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want %v", got, pushedAt)
	}

	img.path = "library/unknown"
	if _, err := lookupPushTime(t.Context(), img, registryCredentials{username: "robot", password: "secret"}); err == nil || !strings.Contains(err.Error(), "exposes no push time") {
		t.Errorf("expected an error for a registry without push time, got %v", err)
	}
}
