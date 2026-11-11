package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/client"
)

const (
	oldDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestParseReleaseAge(t *testing.T) {
	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "0", want: 0},
		{value: "", want: 0},
		{value: "3d", want: 72 * time.Hour},
		{value: "1d12h", want: 36 * time.Hour},
		{value: "90m", want: 90 * time.Minute},
		{value: " 2d ", want: 48 * time.Hour},
		{value: "xd", wantErr: true},
		{value: "-1d", wantErr: true},
		{value: "-5h", wantErr: true},
		{value: "soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseReleaseAge(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseReleaseAge(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseReleaseAge(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestPinnedRef(t *testing.T) {
	got, err := pinnedRef("redis:7.4-alpine", newDigest)
	if err != nil {
		t.Fatalf("pinnedRef: %v", err)
	}
	if want := "docker.io/library/redis@" + newDigest; got != want {
		t.Errorf("pinnedRef = %q, want %q", got, want)
	}
}

func stubPushTime(t *testing.T, pushed time.Time, err error) *int {
	t.Helper()
	orig := getPushTime
	t.Cleanup(func() { getPushTime = orig })
	calls := 0
	getPushTime = func(context.Context, command.Cli, string, string) (time.Time, error) {
		calls++
		return pushed, err
	}
	return &calls
}

func TestEvaluateUpdate(t *testing.T) {
	week := 7 * 24 * time.Hour
	policy := ReleaseAgePolicy{MinimumAge: 3 * 24 * time.Hour}
	service := composetypes.ServiceConfig{Name: "app", Image: "ghcr.io/org/app:1.0"}
	withLabel := func(value string) composetypes.ServiceConfig {
		return composetypes.ServiceConfig{Name: "app", Image: service.Image, Labels: composetypes.Labels{releaseAgeLabel: value}}
	}
	const (
		updated = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED
		waiting = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING
		blocked = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED
		skipped = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED
	)

	tests := []struct {
		name          string
		policy        ReleaseAgePolicy
		service       composetypes.ServiceConfig
		pushed        time.Time
		pushErr       error
		wantStatus    messages.ImageUpdateStatus
		wantReason    string
		wantChecked   bool
		wantLookup    bool
		wantFirstSeen bool
	}{
		{name: "disabled", policy: ReleaseAgePolicy{}, service: service, wantStatus: updated},
		{name: "old enough", policy: policy, service: service, pushed: time.Now().Add(-week), wantStatus: updated, wantChecked: true, wantLookup: true},
		{name: "too young", policy: policy, service: service, pushed: time.Now().Add(-time.Hour), wantStatus: waiting, wantReason: "minimum release age 3d", wantLookup: true},
		{
			name:          "push time unknown counts from first sight",
			policy:        policy,
			service:       service,
			pushErr:       errors.New("no push time"),
			wantStatus:    waiting,
			wantReason:    "first seen at",
			wantLookup:    true,
			wantFirstSeen: true,
		},
		{name: "allowed registry", policy: ReleaseAgePolicy{MinimumAge: policy.MinimumAge, AllowedRegistries: []string{"ghcr.io"}}, service: service, wantStatus: updated},
		{name: "service override disables it", policy: ReleaseAgePolicy{MinimumAge: policy.MinimumAge, ServiceMinimumAge: map[string]time.Duration{"app": 0}}, service: service, wantStatus: updated},
		{
			name:       "service override enables it",
			policy:     ReleaseAgePolicy{ServiceMinimumAge: map[string]time.Duration{"app": week}},
			service:    service,
			pushed:     time.Now().Add(-72 * time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 7d",
			wantLookup: true,
		},
		{name: "label disables it", policy: policy, service: withLabel("0"), wantStatus: updated},
		{
			name:       "label wins over service override",
			policy:     ReleaseAgePolicy{ServiceMinimumAge: map[string]time.Duration{"app": 0}},
			service:    withLabel("7d"),
			pushed:     time.Now().Add(-72 * time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 7d",
			wantLookup: true,
		},
		{name: "invalid label", policy: policy, service: withLabel("soon"), wantStatus: blocked, wantReason: "invalid " + releaseAgeLabel + " label"},
		{name: "announced by webhook", policy: ReleaseAgePolicy{MinimumAge: policy.MinimumAge, WebhookImages: []string{"ghcr.io/org/app:2.0"}}, service: service, wantStatus: updated},
		{
			name:       "not announced by webhook",
			policy:     ReleaseAgePolicy{MinimumAge: policy.MinimumAge, WebhookImages: []string{"ghcr.io/org/other:1.0"}},
			service:    service,
			wantStatus: skipped,
			wantReason: "not announced by the image pull webhook",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stubPushTime(t, tt.pushed, tt.pushErr)
			c := newTestClient(t)

			update := &messages.ImageUpdate{}
			checked := c.evaluateUpdate(t.Context(), "app-1", tt.policy, tt.service, newDigest, update)

			if update.Status != tt.wantStatus {
				t.Errorf("status = %v, want %v (reason %q)", update.Status, tt.wantStatus, update.Reason)
			}
			if !strings.Contains(update.Reason, tt.wantReason) {
				t.Errorf("reason = %q, want it to contain %q", update.Reason, tt.wantReason)
			}
			if checked != tt.wantChecked {
				t.Errorf("checked = %v, want %v", checked, tt.wantChecked)
			}
			if (*calls > 0) != tt.wantLookup {
				t.Errorf("push time looked up %d times, want lookup %v", *calls, tt.wantLookup)
			}
			if update.FirstSeen != tt.wantFirstSeen {
				t.Errorf("first seen = %v, want %v", update.FirstSeen, tt.wantFirstSeen)
			}
		})
	}
}

func TestFirstSeenAt_KeepsTimeUntilDigestChanges(t *testing.T) {
	c := newTestClient(t)

	first := c.firstSeenAt("app-1", "db", newDigest)
	time.Sleep(10 * time.Millisecond)
	if again := c.firstSeenAt("app-1", "db", newDigest); !again.Equal(first) {
		t.Errorf("first seen moved from %v to %v", first, again)
	}
	if other := c.firstSeenAt("app-1", "db", oldDigest); !other.After(first) {
		t.Errorf("a new digest must start over, got %v after %v", other, first)
	}
}

// setupStaleImage deploys an application whose image has a new digest in the registry.
func setupStaleImage(t *testing.T) *Client {
	t.Helper()
	saveRestoreVars(t)
	origTag := tagImage
	t.Cleanup(func() { tagImage = origTag })
	getImageVersion = func(context.Context, command.Cli, string, string) (string, error) { return "1.1.0", nil }
	getLocalVersion = func(context.Context, client.APIClient, string) string { return "1.0.0" }

	c := newTestClient(t)
	c.deploymentsDir = t.TempDir()
	appDir := filepath.Join(c.deploymentsDir, "myapp")
	if err := os.MkdirAll(appDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, composeFileName), []byte("services:\n  app:\n    image: ghcr.io/org/app:1.0\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadProject = func(context.Context, api.Compose, api.ProjectLoadOptions) (*composetypes.Project, error) {
		return &composetypes.Project{Name: "myapp", Services: composetypes.Services{
			"app": {Name: "app", ContainerSpec: composetypes.ContainerSpec{Image: "ghcr.io/org/app:1.0", PullPolicy: composetypes.PullPolicyAlways}},
		}}, nil
	}
	getRemoteDigest = func(context.Context, command.Cli, string) (string, error) { return newDigest, nil }
	getLocalDigests = func(context.Context, client.APIClient, string) ([]string, error) {
		return []string{"ghcr.io/org/app@" + oldDigest}, nil
	}
	return c
}

func TestCheckAndPullImages_HoldsBackYoungImage(t *testing.T) {
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-time.Hour), nil)

	pullProject = func(context.Context, api.Compose, *composetypes.Project, api.PullOptions) error {
		t.Error("a held back image must not be pulled")
		return nil
	}
	upProject = func(context.Context, api.Compose, *composetypes.Project, api.UpOptions) error {
		t.Error("nothing to redeploy when the only update is held back")
		return nil
	}

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("CheckAndPullImages: %v", err)
	}
	if updated {
		t.Error("expected no update")
	}
	if len(images) != 1 || images[0].Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING ||
		images[0].CurrentDigest != oldDigest || images[0].NewDigest != newDigest ||
		images[0].CurrentVersion != "1.0.0" || images[0].NewVersion != "1.1.0" ||
		!strings.Contains(images[0].Reason, "waiting until") {
		t.Fatalf("unexpected image updates: %v", images)
	}
}

func TestCheckAndPullImages_PullsCheckedImageByDigest(t *testing.T) {
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-48*time.Hour), nil)

	var pulled, upped *composetypes.Project
	pullProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.PullOptions) error {
		pulled = project
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		upped = project
		return nil
	}
	var tagSource, tagTarget string
	tagImage = func(_ context.Context, _ client.APIClient, source, target string) error {
		tagSource, tagTarget = source, target
		return nil
	}

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("CheckAndPullImages: %v", err)
	}
	if !updated || len(images) != 1 || images[0].Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED {
		t.Fatalf("expected an update, got updated=%v images=%v", updated, images)
	}

	pinned := "ghcr.io/org/app@" + newDigest
	if got := pulled.Services["app"].Image; got != pinned {
		t.Errorf("pulled image = %q, want %q", got, pinned)
	}
	if tagSource != pinned || tagTarget != "ghcr.io/org/app:1.0" {
		t.Errorf("tagged %q as %q, want %q as ghcr.io/org/app:1.0", tagSource, tagTarget, pinned)
	}
	// compose up must not pull the tag again, which may have moved by now.
	if got := upped.Services["app"]; got.Image != "ghcr.io/org/app:1.0" || got.PullPolicy != composetypes.PullPolicyNever {
		t.Errorf("compose up ran %q with pull policy %q", got.Image, got.PullPolicy)
	}
}

func TestCheckAndPullImages_MissingImageSkipsReleaseAge(t *testing.T) {
	c := setupStaleImage(t)
	calls := stubPushTime(t, time.Now(), nil)
	getLocalDigests = func(context.Context, client.APIClient, string) ([]string, error) {
		return nil, errors.New("no such image")
	}
	pullProject = func(context.Context, api.Compose, *composetypes.Project, api.PullOptions) error { return nil }
	upProject = func(context.Context, api.Compose, *composetypes.Project, api.UpOptions) error { return nil }

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("CheckAndPullImages: %v", err)
	}
	if !updated || len(images) != 1 || images[0].Status != messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED || *calls != 0 {
		t.Fatalf("expected the missing image to be pulled without an age check, got updated=%v images=%v lookups=%d", updated, images, *calls)
	}
}
