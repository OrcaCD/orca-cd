package docker

import (
	"encoding/json"
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

// gitLabRepository is a container repository the GitLab GraphQL mock knows.
type gitLabRepository struct {
	id, path string
	// tags maps a tag name to the JSON of its details. Without tags, reading
	// one fails with a GraphQL error.
	tags map[string]string
}

// serveGitLabGraphQL answers the GraphQL queries for the given projects.
func serveGitLabGraphQL(t *testing.T, token string, projects map[string][]gitLabRepository) {
	t.Helper()
	serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/graphql" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Variables map[string]string `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if project, ok := body.Variables["project"]; ok {
			repositories, ok := projects[project]
			if !ok {
				_, _ = w.Write([]byte(`{"data":{"project":null}}`))
				return
			}
			nodes := []string{}
			for _, repository := range repositories {
				if name := body.Variables["name"]; name != "" && !strings.HasSuffix(repository.path, "/"+name) {
					continue
				}
				nodes = append(nodes, `{"id":"`+repository.id+`","path":"`+repository.path+`"}`)
			}
			_, _ = w.Write([]byte(`{"data":{"project":{"containerRepositories":{"nodes":[` + strings.Join(nodes, ",") + `]}}}}`))
			return
		}
		for _, repositories := range projects {
			for _, repository := range repositories {
				if repository.id != body.Variables["id"] || repository.tags == nil {
					continue
				}
				details, ok := repository.tags[body.Variables["tag"]]
				if !ok {
					details = "null"
				}
				_, _ = w.Write([]byte(`{"data":{"containerRepository":{"tagDetails":` + details + `}}}`))
				return
			}
		}
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"repository not found"}]}`))
	})
}

func TestLookupPushTime_GitLab(t *testing.T) {
	serveGitLabGraphQL(t, "glpat", map[string][]gitLabRepository{
		"group/project": {
			{id: "gid://gitlab/ContainerRepository/1", path: "group/project"},
			{id: "gid://gitlab/ContainerRepository/7", path: "group/project/image", tags: map[string]string{
				// The tag was created long ago, the new image was pushed to it later.
				"v1": `{"digest":"` + newDigest + `","createdAt":"2025-01-01T00:00:00Z","publishedAt":"2026-11-01T12:00:00Z"}`,
			}},
		},
	})

	img := registryImage{host: "registry.gitlab.com", path: "group/project/image", tag: "v1", digest: newDigest}
	got, err := lookupPushTime(t.Context(), img, registryCredentials{password: "glpat"})
	if err != nil {
		t.Fatalf("lookupPushTime: %v", err)
	}
	if !got.Equal(pushedAt) {
		t.Errorf("push time = %v, want the time the tag was last published, %v", got, pushedAt)
	}
}

func TestLookupPushTime_Harbor(t *testing.T) {
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "robot" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.EscapedPath() == "/api/v2.0/projects/library/repositories/team%252Fapp/artifacts/"+newDigest && r.URL.Query().Get("with_tag") == "true" {
			// The digest was pushed under another tag long before 1.0 was moved to it.
			_, _ = w.Write([]byte(`{"push_time":"2025-01-01T00:00:00Z","tags":[
				{"name":"nightly","push_time":"2025-01-01T00:00:00Z"},
				{"name":"1.0","push_time":"2026-11-01T12:00:00Z"}
			]}`))
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
		t.Errorf("push time = %v, want the push time of the tag, %v", got, pushedAt)
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
	serveGitLabGraphQL(t, "", map[string][]gitLabRepository{
		"group/project": {{id: "gid://gitlab/ContainerRepository/7", path: "group/project", tags: map[string]string{
			"moved":   `{"digest":"` + oldDigest + `","publishedAt":"2026-11-01T12:00:00Z"}`,
			"undated": `{"digest":"` + newDigest + `","publishedAt":null}`,
		}}},
		"group/broken": {{id: "gid://gitlab/ContainerRepository/unknown", path: "group/broken"}},
	})

	tests := []struct {
		name string
		img  registryImage
		want string
	}{
		{name: "pushed again", img: registryImage{path: "group/project", tag: "moved"}, want: "another digest"},
		{name: "no push time", img: registryImage{path: "group/project", tag: "undated"}, want: "no push time"},
		{name: "unknown tag", img: registryImage{path: "group/project", tag: "missing"}, want: "no tag missing"},
		{name: "no matching repository", img: registryImage{path: "group/project/other", tag: "v1"}, want: "no registry repository found"},
		{name: "no project", img: registryImage{path: "group/unknown", tag: "v1"}, want: "no registry repository found"},
		{name: "graphql error", img: registryImage{path: "group/broken", tag: "v1"}, want: "gitlab: repository not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.img.host, tt.img.digest = "registry.gitlab.com", newDigest
			if _, err := lookupPushTime(t.Context(), tt.img, registryCredentials{}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLookupPushTime_GitLabServerError(t *testing.T) {
	serveRegistryAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	img := registryImage{host: "registry.gitlab.com", path: "group/project", tag: "v1", digest: newDigest}
	if _, err := lookupPushTime(t.Context(), img, registryCredentials{}); err == nil || !strings.Contains(err.Error(), "gitlab: unexpected status 500") {
		t.Errorf("expected the server error, got %v", err)
	}
}

func TestLookupPushTime_HarborErrors(t *testing.T) {
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"tags":[{"name":"undated"}]}`))
	})
	host := strings.TrimPrefix(srv.URL, "https://")

	tests := []struct {
		name string
		img  registryImage
		want string
	}{
		{name: "not in a project", img: registryImage{path: "app", tag: "1.0"}, want: "not in a Harbor project"},
		{name: "no push time", img: registryImage{path: "library/app", tag: "undated"}, want: "no push time in the response"},
		{name: "tag moved", img: registryImage{path: "library/app", tag: "1.0"}, want: "no longer points to the digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.img.host, tt.img.digest = host, newDigest
			if _, err := lookupPushTime(t.Context(), tt.img, registryCredentials{}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestGetPushTime_InvalidReference(t *testing.T) {
	if _, err := getPushTime(t.Context(), nil, "Invalid Ref", newDigest); err == nil {
		t.Error("expected an error for an invalid image reference")
	}
}
