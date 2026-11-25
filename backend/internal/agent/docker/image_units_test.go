package docker

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

// stubPushTimes stubs the push time of every image by its reference.
func stubPushTimes(t *testing.T, pushed map[string]time.Time) {
	t.Helper()
	orig := getPushTime
	t.Cleanup(func() { getPushTime = orig })
	getPushTime = func(_ context.Context, _ command.Cli, ref, _ string) (time.Time, error) {
		at, ok := pushed[ref]
		if !ok {
			t.Errorf("unexpected push time lookup of %s", ref)
		}
		return at, nil
	}
}

// stubUnitProject loads services as the project and records what is pulled,
// deployed and tagged.
func stubUnitProject(t *testing.T, services composetypes.Services) (pulled, upped **composetypes.Project, tagged *[]string) {
	t.Helper()
	loadProject = func(context.Context, api.Compose, api.ProjectLoadOptions) (*composetypes.Project, error) {
		project := &composetypes.Project{Name: "myapp", Services: composetypes.Services{}}
		for name, service := range services {
			service.Name = name
			project.Services[name] = service
		}
		return project, nil
	}
	pulled, upped, tagged = new(*composetypes.Project), new(*composetypes.Project), new([]string)
	pullProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.PullOptions) error {
		*pulled = project
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		*upped = project
		return nil
	}
	tagImage = func(_ context.Context, _ client.APIClient, source, target string) error {
		*tagged = append(*tagged, source+" -> "+target)
		return nil
	}
	return pulled, upped, tagged
}

func serviceWith(image string, labels composetypes.Labels, hookImages ...string) composetypes.ServiceConfig {
	service := composetypes.ServiceConfig{Image: image, Labels: labels}
	for _, hookImage := range hookImages {
		service.PreStart = append(service.PreStart, composetypes.PreStartHook{Image: hookImage})
	}
	return service
}

// Pulling a tag changes the image of every service that uses it, so they all
// wait for the strictest release age among them.
func TestCheckAndPullImages_SharedTagWaitsForTheStrictestAge(t *testing.T) {
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-48*time.Hour), nil)
	pulled, _, _ := stubUnitProject(t, composetypes.Services{
		"web":    serviceWith("ghcr.io/org/app:1.0", composetypes.Labels{releaseAgeLabel: "0"}),
		"worker": serviceWith("ghcr.io/org/app:1.0", composetypes.Labels{releaseAgeLabel: "7d"}),
		"api":    serviceWith("ghcr.io/org/app:1.0", nil),
	})

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if err != nil || updated || *pulled != nil {
		t.Fatalf("expected the update to wait, got updated=%v err=%v pulled=%v", updated, err, *pulled != nil)
	}
	if len(images) != 3 {
		t.Fatalf("expected 3 image updates, got %v", images)
	}
	var workerEligible int64
	for _, update := range images {
		if update.Service == "worker" {
			workerEligible = update.EligibleAt
		}
	}
	for _, update := range images {
		if update.Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING || update.EligibleAt != workerEligible {
			t.Errorf("%s: status %v eligible at %d, want waiting until %d", update.Service, update.Status, update.EligibleAt, workerEligible)
		}
		if update.Service != "worker" && !strings.Contains(update.Reason, "service worker uses the same image tag") {
			t.Errorf("%s: unexpected reason %q", update.Service, update.Reason)
		}
	}
	if got := images[0]; got.Service != "api" || got.MinimumAgeSeconds != 24*60*60 {
		t.Errorf("api must keep its own minimum age, got %v", got)
	}
}

func TestCheckAndPullImages_SharedTagUpdatesTogether(t *testing.T) {
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-48*time.Hour), nil)
	pulled, upped, tagged := stubUnitProject(t, composetypes.Services{
		"web":    serviceWith("ghcr.io/org/app:1.0", composetypes.Labels{releaseAgeLabel: "0"}),
		"worker": serviceWith("ghcr.io/org/app:1.0", nil),
	})

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if err != nil || !updated {
		t.Fatalf("expected an update, got updated=%v err=%v", updated, err)
	}
	for _, update := range images {
		if update.Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED {
			t.Errorf("%s: status %v", update.Service, update.Status)
		}
	}
	pinned := "ghcr.io/org/app@" + newDigest
	for _, name := range []string{"web", "worker"} {
		if got := (*pulled).Services[name]; got.Image != pinned || got.PullPolicy != "" {
			t.Errorf("pulled %s as %q with pull policy %q, want %q", name, got.Image, got.PullPolicy, pinned)
		}
		if got := (*upped).Services[name]; got.Image != "ghcr.io/org/app:1.0" || got.PullPolicy != composetypes.PullPolicyNever {
			t.Errorf("compose up ran %s with %q and pull policy %q", name, got.Image, got.PullPolicy)
		}
	}
	if want := []string{pinned + " -> ghcr.io/org/app:1.0"}; !slices.Equal(*tagged, want) {
		t.Errorf("tagged %v, want %v", *tagged, want)
	}
}

// The images of pre_start hooks wait for the release age of their service, and
// compose must not pull them by their tag.
func TestCheckAndPullImages_ChecksPreStartHookImages(t *testing.T) {
	const migrate = "ghcr.io/org/migrate:latest"
	tests := []struct {
		name          string
		migratePushed time.Duration
		wantHookPull  bool
	}{
		{name: "young hook image waits", migratePushed: time.Hour},
		{name: "old hook image is pulled by digest", migratePushed: 48 * time.Hour, wantHookPull: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := setupStaleImage(t)
			stubPushTimes(t, map[string]time.Time{
				"ghcr.io/org/app:1.0": time.Now().Add(-48 * time.Hour),
				migrate:               time.Now().Add(-tt.migratePushed),
			})
			pulled, upped, tagged := stubUnitProject(t, composetypes.Services{
				"web": serviceWith("ghcr.io/org/app:1.0", nil, migrate),
			})

			updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
			if err != nil || !updated {
				t.Fatalf("expected web to be updated, got updated=%v err=%v", updated, err)
			}
			if len(images) != 1 || images[0].Service != "web" || images[0].Image != "ghcr.io/org/app:1.0" {
				t.Errorf("expected only the service image to be reported, got %v", images)
			}

			var hookPulls []string
			for name, service := range (*pulled).Services {
				if len(service.PreStart) > 0 {
					t.Errorf("compose would pull the hook images of %s by their tag", name)
				}
				if name != "web" {
					hookPulls = append(hookPulls, service.Image)
				}
			}
			wantTags := []string{"ghcr.io/org/app@" + newDigest + " -> ghcr.io/org/app:1.0"}
			var wantHookPulls []string
			if tt.wantHookPull {
				pinned := "ghcr.io/org/migrate@" + newDigest
				wantHookPulls = []string{pinned}
				wantTags = append(wantTags, pinned+" -> "+migrate)
			}
			if !slices.Equal(hookPulls, wantHookPulls) {
				t.Errorf("pulled hook images %v, want %v", hookPulls, wantHookPulls)
			}
			slices.Sort(*tagged)
			if !slices.Equal(*tagged, wantTags) {
				t.Errorf("tagged %v, want %v", *tagged, wantTags)
			}
			if got := (*upped).Services["web"]; got.PullPolicy != composetypes.PullPolicyNever || len(got.PreStart) != 1 {
				t.Errorf("compose up must run the hook without pulling it, got pull policy %q and hooks %v", got.PullPolicy, got.PreStart)
			}
		})
	}
}

// A hook image that is also the image of another service waits for both.
func TestCheckAndPullImages_PreStartHookSharesTagWithService(t *testing.T) {
	const migrate = "ghcr.io/org/migrate:latest"
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-48*time.Hour), nil)
	pulled, _, _ := stubUnitProject(t, composetypes.Services{
		"web":     serviceWith("ghcr.io/org/app:1.0", composetypes.Labels{releaseAgeLabel: "0"}, migrate),
		"migrate": serviceWith(migrate, composetypes.Labels{releaseAgeLabel: "7d"}),
	})
	getLocalDigests = func(_ context.Context, _ client.APIClient, ref string) ([]string, error) {
		if ref == "ghcr.io/org/app:1.0" {
			return []string{"ghcr.io/org/app@" + newDigest}, nil
		}
		return []string{"ghcr.io/org/migrate@" + oldDigest}, nil
	}

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{})
	if err != nil || updated || *pulled != nil {
		t.Fatalf("expected the hook image to wait for migrate, got updated=%v err=%v pulled=%v", updated, err, *pulled != nil)
	}
	statuses := map[string]messages.ImageUpdateStatus{}
	for _, update := range images {
		statuses[update.Service] = update.Status
	}
	want := map[string]messages.ImageUpdateStatus{
		"web":     messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE,
		"migrate": messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING,
	}
	if !maps.Equal(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}
}

// A hook image of a service that isn't updated is pulled as a service of its own.
func TestPullTargetProject_PullsHookOnlyUnits(t *testing.T) {
	project := &composetypes.Project{Name: "myapp", Services: composetypes.Services{
		"web":                       {Name: "web", ContainerSpec: composetypes.ContainerSpec{Image: "app:1", PullPolicy: composetypes.PullPolicyBuild}},
		"orca-cd-pre-start-image-0": {Name: "orca-cd-pre-start-image-0", ContainerSpec: composetypes.ContainerSpec{Image: "other:1"}},
	}}
	hookService := project.Services["web"]
	hookService.Image = "migrate:1"
	units := []*imageUnit{
		{ref: "app:1", uses: []*imageUse{{service: project.Services["web"]}}},
		{ref: "migrate:1", updated: true, uses: []*imageUse{{service: hookService, hook: true}}},
	}

	pull := pullTargetProject(project, units)
	if got := pull.Services["web"].PullPolicy; got != composetypes.PullPolicyNever {
		t.Errorf("web must not be pulled, got %q", got)
	}
	hook, ok := pull.Services["orca-cd-pre-start-image-1"]
	if !ok || hook.Image != "migrate:1" || hook.PullPolicy != composetypes.PullPolicyMissing {
		t.Errorf("expected the hook image to be pulled if missing, got %v", pull.Services)
	}
	if project.Services["web"].PullPolicy != composetypes.PullPolicyBuild {
		t.Error("the project itself must not change")
	}
}

// Image polling neither checks nor pulls an image that only excluded services
// use, also when it is missing locally.
func TestCheckAndPullImages_ExcludedServicesAreNotChecked(t *testing.T) {
	c := setupStaleImage(t)
	pulled, _, _ := stubUnitProject(t, composetypes.Services{
		"web": serviceWith("ghcr.io/org/app:1.0", nil),
		"db":  serviceWith("postgres:17", nil),
	})
	checked := []string{}
	getRemoteDigest = func(_ context.Context, _ command.Cli, ref string) (string, error) {
		checked = append(checked, ref)
		return newDigest, nil
	}

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{ExcludedServices: []string{"db"}})
	if err != nil || !updated {
		t.Fatalf("expected web to update, got updated=%v err=%v", updated, err)
	}
	if !slices.Equal(checked, []string{"ghcr.io/org/app:1.0"}) {
		t.Errorf("checked %v, want only the image of web", checked)
	}
	if got := (*pulled).Services["db"].PullPolicy; got != composetypes.PullPolicyNever {
		t.Errorf("expected db not to be pulled, got pull policy %q", got)
	}
	for _, update := range images {
		if update.Service == "db" && update.Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED {
			t.Errorf("db: status %v, want skipped", update.Status)
		}
	}
}
