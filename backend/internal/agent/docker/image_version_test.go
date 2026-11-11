package docker

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
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
