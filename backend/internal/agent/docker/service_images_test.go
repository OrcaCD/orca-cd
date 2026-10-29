package docker

import (
	"context"
	"errors"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	digestAaa   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestBbb   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestOther = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func stubServiceImageLookups(t *testing.T, containers []containerapi.Summary, digests map[string][]string) {
	t.Helper()
	origList := listApplicationContainers
	origDigests := getLocalDigests
	t.Cleanup(func() {
		listApplicationContainers = origList
		getLocalDigests = origDigests
	})
	listApplicationContainers = func(context.Context, client.APIClient, string) ([]containerapi.Summary, error) {
		return containers, nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, imageID string) ([]string, error) {
		repoDigests, ok := digests[imageID]
		if !ok {
			return nil, errors.New("no such image")
		}
		return repoDigests, nil
	}
}

func serviceContainer(service, image, imageID string) containerapi.Summary {
	return containerapi.Summary{Image: image, ImageID: imageID, Labels: map[string]string{api.ServiceLabel: service}}
}

func TestServiceImages(t *testing.T) {
	c := newTestClient(t)
	stubServiceImageLookups(t, []containerapi.Summary{
		serviceContainer("web", "nginx:1.27", "sha256:web"),
		serviceContainer("web", "nginx:1.27", "sha256:web"),
		serviceContainer("app", "ghcr.io/org/app:latest", "sha256:app"),
		serviceContainer("local", "local/app:dev", "sha256:local"),
		serviceContainer("", "busybox", "sha256:unlabeled"),
	}, map[string][]string{
		"sha256:web":   {"nginx@sha256:" + digestAaa},
		"sha256:app":   {"ghcr.io/other/app@sha256:" + digestOther, "ghcr.io/org/app@sha256:" + digestBbb},
		"sha256:local": nil,
	})

	images, err := c.ServiceImages(t.Context(), "app-123")
	if err != nil {
		t.Fatalf("ServiceImages: %v", err)
	}

	want := []struct{ service, imageID, pinned string }{
		{"app", "sha256:app", "ghcr.io/org/app@sha256:" + digestBbb},
		{"local", "sha256:local", ""},
		{"web", "sha256:web", "nginx@sha256:" + digestAaa},
	}
	if len(images) != len(want) {
		t.Fatalf("expected %d images, got %v", len(want), images)
	}
	for i, w := range want {
		got := images[i]
		if got.Service != w.service || got.ImageId != w.imageID || got.PinnedRef != w.pinned {
			t.Errorf("image %d = {%s %s %s}, want %v", i, got.Service, got.ImageId, got.PinnedRef, w)
		}
	}
}

func TestServiceImages_ListError(t *testing.T) {
	c := newTestClient(t)
	origList := listApplicationContainers
	t.Cleanup(func() { listApplicationContainers = origList })
	listApplicationContainers = func(context.Context, client.APIClient, string) ([]containerapi.Summary, error) {
		return nil, errors.New("daemon unavailable")
	}

	if _, err := c.ServiceImages(t.Context(), "app-123"); err == nil {
		t.Fatal("expected error when listing containers fails")
	}
}

func TestPinnedReference(t *testing.T) {
	tests := []struct {
		name     string
		imageRef string
		digests  []string
		want     string
	}{
		{"familiar name", "nginx:latest", []string{"nginx@sha256:" + digestAaa}, "nginx@sha256:" + digestAaa},
		{"fully qualified name", "docker.io/library/nginx", []string{"nginx@sha256:" + digestAaa}, "nginx@sha256:" + digestAaa},
		{"already pinned", "nginx@sha256:" + digestAaa, []string{"nginx@sha256:" + digestAaa}, "nginx@sha256:" + digestAaa},
		{"other repository only", "ghcr.io/org/app:1", []string{"ghcr.io/org/fork@sha256:" + digestAaa}, ""},
		{"no digests", "local/app:dev", nil, ""},
		{"image id with one digest", "sha256:0123", []string{"nginx@sha256:" + digestAaa}, "nginx@sha256:" + digestAaa},
		{"image id with ambiguous digests", "sha256:0123", []string{"nginx@sha256:" + digestAaa, "mirror/nginx@sha256:" + digestAaa}, ""},
		{"invalid reference", "Invalid Ref", []string{"nginx@sha256:" + digestAaa}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pinnedReference(tt.imageRef, tt.digests); got != tt.want {
				t.Errorf("pinnedReference(%q) = %q, want %q", tt.imageRef, got, tt.want)
			}
		})
	}
}

func TestPinServiceImages(t *testing.T) {
	project := &composetypes.Project{Services: composetypes.Services{
		"web":   {Name: "web", Image: "nginx:latest"},
		"built": {Name: "built", Image: "local/app", Build: &composetypes.BuildConfig{Context: "."}},
		"db":    {Name: "db", Image: "postgres:17"},
	}}

	pinServiceImages(project, map[string]string{
		"web":     "nginx@sha256:" + digestAaa,
		"built":   "sha256:built",
		"removed": "redis@sha256:ccc",
	})

	if got := project.Services["web"].Image; got != "nginx@sha256:"+digestAaa {
		t.Errorf("web image = %q, want pinned digest", got)
	}
	if got := project.Services["built"].Image; got != "local/app" {
		t.Errorf("built image = %q, want it unchanged", got)
	}
	if got := project.Services["db"].Image; got != "postgres:17" {
		t.Errorf("db image = %q, want it unchanged", got)
	}
	if _, ok := project.Services["removed"]; ok {
		t.Error("pins must not add services")
	}
}
