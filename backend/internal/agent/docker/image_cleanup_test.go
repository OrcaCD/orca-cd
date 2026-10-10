package docker

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// fakeImageAPIClient implements only the APIClient methods used by image cleanup.
type fakeImageAPIClient struct {
	client.APIClient
	containers  []containerapi.Summary
	listErr     error
	listOptions client.ContainerListOptions
	removeErr   error
	removed     []string
	removeOpts  client.ImageRemoveOptions
}

func (f *fakeImageAPIClient) ContainerList(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	f.listOptions = options
	if f.listErr != nil {
		return client.ContainerListResult{}, f.listErr
	}
	return client.ContainerListResult{Items: f.containers}, nil
}

func (f *fakeImageAPIClient) ImageRemove(_ context.Context, imageID string, options client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	f.removed = append(f.removed, imageID)
	f.removeOpts = options
	return client.ImageRemoveResult{}, f.removeErr
}

func TestListApplicationImageIDs(t *testing.T) {
	fake := &fakeImageAPIClient{containers: []containerapi.Summary{
		{ImageID: "sha256:app"},
		{ImageID: "sha256:sidecar"},
		{ImageID: "sha256:app"},
		{ImageID: ""},
	}}

	ids, err := listApplicationImageIDs(t.Context(), fake, "app-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("expected 2 distinct image ids, got %v", ids)
	}
	for _, id := range []string{"sha256:app", "sha256:sidecar"} {
		if _, ok := ids[id]; !ok {
			t.Errorf("expected %q in image ids, got %v", id, ids)
		}
	}
	if !fake.listOptions.All {
		t.Error("expected stopped containers to be included")
	}
	if !fake.listOptions.Filters["label"][labelApplicationID+"=app-123"] {
		t.Errorf("expected application label filter, got %v", fake.listOptions.Filters)
	}
}

func TestListApplicationImageIDs_Error(t *testing.T) {
	fake := &fakeImageAPIClient{listErr: errors.New("daemon unavailable")}

	if _, err := listApplicationImageIDs(t.Context(), fake, "app-123"); err == nil {
		t.Fatal("expected error when listing containers fails")
	}
}

func TestRemoveImage(t *testing.T) {
	fake := &fakeImageAPIClient{}

	if err := removeImage(t.Context(), fake, "sha256:old"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(fake.removed, []string{"sha256:old"}) {
		t.Errorf("expected sha256:old to be removed, got %v", fake.removed)
	}
	if !fake.removeOpts.PruneChildren {
		t.Error("expected untagged parent images to be pruned")
	}
	if fake.removeOpts.Force {
		t.Error("expected removal not to be forced so images in use are kept")
	}
}

func TestRemoveImage_Error(t *testing.T) {
	fake := &fakeImageAPIClient{removeErr: cerrdefs.ErrConflict}

	if err := removeImage(t.Context(), fake, "sha256:old"); !cerrdefs.IsConflict(err) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestApplicationImages_ListError(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)

	listApplicationImageIDs = func(_ context.Context, _ client.APIClient, _ string) (map[string]struct{}, error) {
		return nil, errors.New("daemon unavailable")
	}

	if got := c.applicationImages(t.Context(), "app-123"); got != nil {
		t.Errorf("expected nil snapshot on list error, got %v", got)
	}
}

func TestRemoveReplacedImages_NothingPrevious(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)

	listApplicationImageIDs = func(_ context.Context, _ client.APIClient, _ string) (map[string]struct{}, error) {
		t.Fatal("must not list images without a previous snapshot")
		return nil, nil
	}

	c.removeReplacedImages(t.Context(), "app-123", nil)
}

func TestRemoveReplacedImages_ListError(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)

	listApplicationImageIDs = func(_ context.Context, _ client.APIClient, _ string) (map[string]struct{}, error) {
		return nil, errors.New("daemon unavailable")
	}
	removeImage = func(_ context.Context, _ client.APIClient, imageID string) error {
		t.Fatalf("must not remove %q when the current images are unknown", imageID)
		return nil
	}

	c.removeReplacedImages(t.Context(), "app-123", map[string]struct{}{"sha256:old": {}})
}

func TestRemoveReplacedImages_ContinuesOnRemoveErrors(t *testing.T) {
	saveRestoreVars(t)
	c := newTestClient(t)

	listApplicationImageIDs = func(_ context.Context, _ client.APIClient, _ string) (map[string]struct{}, error) {
		return map[string]struct{}{"sha256:current": {}}, nil
	}
	removeErrs := map[string]error{
		"sha256:shared":  fmt.Errorf("image is in use: %w", cerrdefs.ErrConflict),
		"sha256:missing": fmt.Errorf("no such image: %w", cerrdefs.ErrNotFound),
		"sha256:broken":  errors.New("daemon unavailable"),
		"sha256:old":     nil,
	}
	var removed []string
	removeImage = func(_ context.Context, _ client.APIClient, imageID string) error {
		removed = append(removed, imageID)
		return removeErrs[imageID]
	}

	previous := map[string]struct{}{"sha256:current": {}}
	for id := range removeErrs {
		previous[id] = struct{}{}
	}
	c.removeReplacedImages(t.Context(), "app-123", previous)

	slices.Sort(removed)
	want := []string{"sha256:broken", "sha256:missing", "sha256:old", "sha256:shared"}
	if !slices.Equal(removed, want) {
		t.Errorf("expected every replaced image to be attempted despite errors, got %v", removed)
	}
}
