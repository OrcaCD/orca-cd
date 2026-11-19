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

func TestLookupPushTime_DockerHubErrors(t *testing.T) {
	var tagBody string
	var tokenStatus int
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/auth/token":
			w.WriteHeader(tokenStatus)
			_, _ = w.Write([]byte(`{"access_token":"jwt"}`))
		case "/v2/namespaces/library/repositories/redis/tags/7.4":
			_, _ = w.Write([]byte(tagBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	img := registryImage{host: "docker.io", path: "library/redis", tag: "7.4", digest: newDigest}
	creds := registryCredentials{username: "user", password: "pat"}

	tests := []struct {
		name        string
		tokenStatus int
		tagBody     string
		creds       registryCredentials
		img         registryImage
		want        string
	}{
		{name: "login fails", tokenStatus: http.StatusUnauthorized, creds: creds, img: img, want: "docker hub login"},
		{name: "unknown tag", tokenStatus: http.StatusOK, img: registryImage{host: "docker.io", path: "library/redis", tag: "missing", digest: newDigest}, want: "docker hub: unexpected status 404"},
		{name: "no push time", tokenStatus: http.StatusOK, tagBody: `{"digest":"` + newDigest + `"}`, img: img, want: "no push time"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenStatus, tagBody = tt.tokenStatus, tt.tagBody
			if _, err := lookupPushTime(t.Context(), tt.img, tt.creds); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLookupPushTime_DockerHubMatchesPlatformDigest(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"digest":"` + oldDigest + `","images":[{"digest":"` + newDigest + `"}],"tag_last_pushed":"2026-11-01T12:00:00Z"}`))
	})

	img := registryImage{host: "docker.io", path: "library/redis", tag: "7.4", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want %v", got, pushedAt)
	}
}

func TestLookupPushTime_GHCRErrors(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/org/packages/container/app/versions":
			_, _ = w.Write([]byte(`[{"name":"` + oldDigest + `","created_at":"2026-10-01T00:00:00Z"}]`))
		case "/orgs/broken/packages/container/app/versions":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	creds := registryCredentials{username: "someone", password: "ghp_token"}

	img := registryImage{host: "ghcr.io", path: "org/app", tag: "1.0", digest: newDigest}
	if _, err := lookupPushTime(t.Context(), img, creds); err == nil || !strings.Contains(err.Error(), "no package version found") {
		t.Errorf("expected an error about a missing version, got %v", err)
	}

	// Only a missing organization falls back to the user
	img.path = "broken/app"
	if _, err := lookupPushTime(t.Context(), img, creds); err == nil || !strings.Contains(err.Error(), "unexpected status 500") {
		t.Errorf("expected the server error, got %v", err)
	}
}

func TestLookupPushTime_GitLabErrors(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/group%2Fproject/registry/repositories":
			_, _ = w.Write([]byte(`[{"id":7,"path":"group/project"}]`))
		case "/api/v4/projects/group%2Fproject/registry/repositories/7/tags/moved":
			_, _ = w.Write([]byte(`{"digest":"` + oldDigest + `","created_at":"2026-11-01T12:00:00Z"}`))
		case "/api/v4/projects/group%2Fproject/registry/repositories/7/tags/undated":
			_, _ = w.Write([]byte(`{"digest":"` + newDigest + `"}`))
		case "/api/v4/projects/group%2Fbroken/registry/repositories":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	creds := registryCredentials{password: "glpat"}

	tests := []struct {
		name string
		img  registryImage
		want string
	}{
		{name: "pushed again", img: registryImage{path: "group/project", tag: "moved"}, want: "another digest"},
		{name: "no push time", img: registryImage{path: "group/project", tag: "undated"}, want: "no push time"},
		{name: "unknown tag", img: registryImage{path: "group/project", tag: "missing"}, want: "gitlab: unexpected status 404"},
		{name: "no matching repository", img: registryImage{path: "group/project/other", tag: "v1"}, want: "no registry repository found"},
		{name: "no project", img: registryImage{path: "group/unknown", tag: "v1"}, want: "no registry repository found"},
		{name: "server error", img: registryImage{path: "group/broken", tag: "v1"}, want: "gitlab: unexpected status 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.img.host, tt.img.digest = "registry.gitlab.com", newDigest
			if _, err := lookupPushTime(t.Context(), tt.img, creds); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLookupPushTime_HarborErrors(t *testing.T) {
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	host := strings.TrimPrefix(srv.URL, "https://")

	if _, err := lookupPushTime(t.Context(), registryImage{host: host, path: "app", digest: newDigest}, registryCredentials{}); err == nil || !strings.Contains(err.Error(), "not in a Harbor project") {
		t.Errorf("expected an error for an image outside a project, got %v", err)
	}
	if _, err := lookupPushTime(t.Context(), registryImage{host: host, path: "library/app", digest: newDigest}, registryCredentials{}); err == nil || !strings.Contains(err.Error(), "no push time in the response") {
		t.Errorf("expected an error for a missing push time, got %v", err)
	}
}

func TestGetPushTime_InvalidReference(t *testing.T) {
	if _, err := getPushTime(t.Context(), nil, "Invalid Ref", newDigest); err == nil {
		t.Error("expected an error for an invalid image reference")
	}
}
