package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/rs/zerolog"
)

func TestRegistryClientVersion_MultiPlatformWithBearerToken(t *testing.T) {
	const platformDigest = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	const configDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"

	var srvURL string
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.URL.Query().Get("scope") != "repository:library/redis:pull" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"token":"pull-token"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer pull-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srvURL+`/token",service="registry.test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/library/redis/manifests/" + newDigest:
			_, _ = w.Write([]byte(`{"manifests":[
				{"digest":"sha256:attestation","platform":{"os":"unknown","architecture":"unknown"}},
				{"digest":"` + platformDigest + `","platform":{"os":"linux","architecture":"` + runtime.GOARCH + `"}}
			]}`))
		case "/v2/library/redis/manifests/" + platformDigest:
			_, _ = w.Write([]byte(`{"config":{"digest":"` + configDigest + `"}}`))
		case "/v2/library/redis/blobs/" + configDigest:
			_, _ = w.Write([]byte(`{"config":{"Labels":{"org.opencontainers.image.version":"7.4.2"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srvURL = srv.URL

	registry := &registryClient{base: srv.URL + "/v2/library/redis", repo: "library/redis"}
	version, err := registry.version(t.Context(), newDigest)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != "7.4.2" {
		t.Errorf("version = %q, want 7.4.2", version)
	}
}

func TestRegistryClientVersion_BasicAuthNeedsCredentials(t *testing.T) {
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	})

	registry := &registryClient{base: srv.URL + "/v2/app", repo: "app"}
	if _, err := registry.version(t.Context(), newDigest); err == nil || !strings.Contains(err.Error(), "needs credentials") {
		t.Errorf("expected an error about missing credentials, got %v", err)
	}
}

func TestRegistryClientVersion_BasicAuthWithCredentials(t *testing.T) {
	const configDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "robot" || pass != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/app/manifests/" + newDigest:
			_, _ = w.Write([]byte(`{"config":{"digest":"` + configDigest + `"}}`))
		case "/v2/app/blobs/" + configDigest:
			_, _ = w.Write([]byte(`{"config":{"Labels":{"org.opencontainers.image.version":"1.2.3"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	registry := &registryClient{base: srv.URL + "/v2/app", repo: "app", creds: registryCredentials{username: "robot", password: "secret"}}
	version, err := registry.version(t.Context(), newDigest)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("version = %q, want 1.2.3", version)
	}
}

func TestRegistryClientVersion_AccessTokenWithCredentials(t *testing.T) {
	const configDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	var srvURL string
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != "pat" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer access" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srvURL+`/token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v2/app/manifests/" + newDigest:
			_, _ = w.Write([]byte(`{"config":{"digest":"` + configDigest + `"}}`))
		case "/v2/app/blobs/" + configDigest:
			_, _ = w.Write([]byte(`{"config":{"Labels":{}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srvURL = srv.URL

	registry := &registryClient{base: srv.URL + "/v2/app", repo: "app", creds: registryCredentials{username: "user", password: "pat"}}
	version, err := registry.version(t.Context(), newDigest)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version != "" {
		t.Errorf("version = %q, want none", version)
	}
}

func TestRegistryClientVersion_Errors(t *testing.T) {
	var srvURL string
	srv := serveRegistryAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/unsupported/manifests/" + newDigest:
			w.Header().Set("WWW-Authenticate", `Digest realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/notoken/manifests/" + newDigest:
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srvURL+`/token"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			_, _ = w.Write([]byte(`{}`))
		case "/noconfig/manifests/" + newDigest:
			_, _ = w.Write([]byte(`{}`))
		case "/index/manifests/" + newDigest:
			_, _ = w.Write([]byte(`{"manifests":[{"digest":"sha256:gone","platform":{"os":"linux","architecture":"` + runtime.GOARCH + `"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srvURL = srv.URL

	tests := []struct {
		repo string
		want string
	}{
		{repo: "unsupported", want: "unsupported challenge"},
		{repo: "notoken", want: "returned no token"},
		{repo: "noconfig", want: "manifest has no config"},
		{repo: "index", want: "unexpected status 404"},
		{repo: "missing", want: "unexpected status 404"},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			registry := &registryClient{base: srv.URL + "/" + tt.repo, repo: tt.repo}
			if _, err := registry.version(t.Context(), newDigest); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected an error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestPlatformManifest(t *testing.T) {
	var index ociManifest
	if err := json.Unmarshal([]byte(`{"manifests":[
		{"digest":"attestation","platform":{"os":"unknown","architecture":"unknown"}},
		{"digest":"windows","platform":{"os":"windows","architecture":"amd64"}},
		{"digest":"linux","platform":{"os":"linux","architecture":"`+runtime.GOARCH+`"}}
	]}`), &index); err != nil {
		t.Fatal(err)
	}
	if got := platformManifest(index); got != "linux" {
		t.Errorf("platformManifest = %q, want the agent's platform", got)
	}

	index.Manifests = index.Manifests[:2]
	if got := platformManifest(index); got != "windows" {
		t.Errorf("platformManifest = %q, want the first other platform", got)
	}

	index.Manifests = index.Manifests[:1]
	if got := platformManifest(index); got != "attestation" {
		t.Errorf("platformManifest = %q, want the first manifest", got)
	}
}

func TestImageVersion_CachesByDigest(t *testing.T) {
	calls := 0
	orig := getImageVersion
	t.Cleanup(func() { getImageVersion = orig })
	getImageVersion = func(_ context.Context, _ command.Cli, _, digest string) (string, error) {
		calls++
		if digest == oldDigest {
			return "", errors.New("registry unreachable")
		}
		return "2.0", nil
	}

	c := &Client{log: zerolog.Nop()}
	for range 2 {
		if got := c.imageVersion(t.Context(), "ghcr.io/org/app:2.0", newDigest); got != "2.0" {
			t.Errorf("imageVersion = %q, want 2.0", got)
		}
	}
	if calls != 1 {
		t.Errorf("registry read %d times, want once", calls)
	}

	// A failed read is not cached
	for range 2 {
		if got := c.imageVersion(t.Context(), "ghcr.io/org/app:1.0", oldDigest); got != "" {
			t.Errorf("imageVersion = %q, want none", got)
		}
	}
	if calls != 3 {
		t.Errorf("registry read %d times, want 3", calls)
	}
}

func TestGetImageVersion_InvalidReference(t *testing.T) {
	if _, err := getImageVersion(t.Context(), nil, "Invalid Ref", newDigest); err == nil {
		t.Error("expected an error for an invalid image reference")
	}
}
