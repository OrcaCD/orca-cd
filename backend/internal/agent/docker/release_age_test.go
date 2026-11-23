package docker

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
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
		{value: "365d", want: 365 * 24 * time.Hour},
		{value: "365d1h", wantErr: true},
		{value: "9000h", wantErr: true},
		// Would overflow a time.Duration and wrap around to about 36 days
		{value: "250000d", wantErr: true},
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

func TestSameTag(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"redis", "docker.io/library/redis:latest", true},
		{"ghcr.io/org/app:1.0", "ghcr.io/org/app:1.0", true},
		{"ghcr.io/org/app:1.0", "ghcr.io/org/app:2.0", false},
		{"ghcr.io/org/app:1.0", "ghcr.io/org/other:1.0", false},
		{"ghcr.io/org/app@" + newDigest, "ghcr.io/org/app:latest", false},
		{"Invalid Ref", "ghcr.io/org/app", false},
	}
	for _, tt := range tests {
		if got := sameTag(tt.a, tt.b); got != tt.want {
			t.Errorf("sameTag(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
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
		{name: "announced by webhook", policy: ReleaseAgePolicy{MinimumAge: policy.MinimumAge, WebhookImages: []string{"ghcr.io/org/app:1.0"}}, service: service, wantStatus: updated},
		{
			name:       "not announced by webhook",
			policy:     ReleaseAgePolicy{MinimumAge: policy.MinimumAge, WebhookImages: []string{"ghcr.io/org/other:1.0"}},
			service:    service,
			wantStatus: skipped,
			wantReason: "not announced by the image pull webhook",
		},
		{
			name:       "webhook announced another tag",
			policy:     ReleaseAgePolicy{MinimumAge: policy.MinimumAge, WebhookImages: []string{"ghcr.io/org/app:2.0"}},
			service:    service,
			wantStatus: skipped,
			wantReason: "not announced by the image pull webhook",
		},
		{
			name:       "enforced minimum raises a lower label",
			policy:     ReleaseAgePolicy{EnforcedMinimumAge: week},
			service:    withLabel("0"),
			pushed:     time.Now().Add(-72 * time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 7d",
			wantLookup: true,
		},
		{
			name:       "enforced minimum raises a lower service override",
			policy:     ReleaseAgePolicy{EnforcedMinimumAge: week, ServiceMinimumAge: map[string]time.Duration{"app": 0}},
			service:    service,
			pushed:     time.Now().Add(-72 * time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 7d",
			wantLookup: true,
		},
		{
			name:       "enforced minimum applies to allowed registries",
			policy:     ReleaseAgePolicy{MinimumAge: week, EnforcedMinimumAge: 3 * 24 * time.Hour, AllowedRegistries: []string{"ghcr.io"}},
			service:    service,
			pushed:     time.Now().Add(-time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 3d",
			wantLookup: true,
		},
		{
			name:       "enforced minimum applies to announced images",
			policy:     ReleaseAgePolicy{MinimumAge: week, EnforcedMinimumAge: 3 * 24 * time.Hour, WebhookImages: []string{"ghcr.io/org/app:1.0"}},
			service:    service,
			pushed:     time.Now().Add(-time.Hour),
			wantStatus: waiting,
			wantReason: "minimum release age 3d",
			wantLookup: true,
		},
		{
			name:       "exempt registry skips the enforced minimum",
			policy:     ReleaseAgePolicy{EnforcedMinimumAge: week, EnforcedExemptRegistries: []string{"ghcr.io"}, AllowedRegistries: []string{"ghcr.io"}},
			service:    withLabel("0"),
			wantStatus: updated,
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

func TestReleaseAgePolicyFromProto(t *testing.T) {
	got := ReleaseAgePolicyFromProto(&messages.ReleaseAgePolicy{
		MinimumSeconds:        3600,
		ServiceMinimumSeconds: map[string]int64{"db": 7 * 24 * 3600, "app": 0},
		AllowedRegistries:     []string{"registry.example.com"},
	}, []string{"ghcr.io/org/app:2.0"})

	want := ReleaseAgePolicy{
		MinimumAge:        time.Hour,
		ServiceMinimumAge: map[string]time.Duration{"db": 7 * 24 * time.Hour, "app": 0},
		AllowedRegistries: []string{"registry.example.com"},
		WebhookImages:     []string{"ghcr.io/org/app:2.0"},
	}
	if !got.equal(want) {
		t.Errorf("policy = %+v, want %+v", got, want)
	}

	if empty := ReleaseAgePolicyFromProto(nil, nil); !empty.equal(ReleaseAgePolicy{}) || empty.ServiceMinimumAge != nil {
		t.Errorf("policy of nil = %+v, want the zero policy", empty)
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
		return nil, errNoSuchImage
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

// Only the services that are updated may be pulled. The others keep the image
// they run.
func TestCheckAndPullImages_OnlyUpdatedServicesChangeTheirImage(t *testing.T) {
	const (
		postgresDigest = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
		redisDigest    = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	)
	c := setupStaleImage(t)
	stubPushTime(t, time.Now().Add(-time.Hour), nil)
	image := func(ref string, labels composetypes.Labels) composetypes.ServiceConfig {
		return composetypes.ServiceConfig{Image: ref, PullPolicy: composetypes.PullPolicyAlways, Labels: labels}
	}
	loadProject = func(context.Context, api.Compose, api.ProjectLoadOptions) (*composetypes.Project, error) {
		services := composetypes.Services{
			// Updated right away
			"web": image("ghcr.io/org/app:1.0", composetypes.Labels{releaseAgeLabel: "0"}),
			// Held back by the release age
			"worker": image("ghcr.io/org/worker:1.0", nil),
			"db":     image("postgres:17", nil),
			"cache":  image("redis:7", nil),
			"queue":  image("rabbitmq:3", nil),
		}
		for name, service := range services {
			service.Name = name
			services[name] = service
		}
		return &composetypes.Project{Name: "myapp", Services: services}, nil
	}
	getRemoteDigest = func(_ context.Context, _ command.Cli, ref string) (string, error) {
		switch ref {
		case "postgres:17":
			return postgresDigest, nil
		case "redis:7":
			return "", errors.New("toomanyrequests: rate limit exceeded")
		}
		return newDigest, nil
	}
	getLocalDigests = func(_ context.Context, _ client.APIClient, ref string) ([]string, error) {
		switch ref {
		case "postgres:17":
			return []string{"postgres@" + postgresDigest}, nil
		case "redis:7":
			return []string{"redis@" + redisDigest}, nil
		case "rabbitmq:3":
			return nil, errors.New("context deadline exceeded")
		}
		return []string{"ghcr.io/org/app@" + oldDigest}, nil
	}
	isLocalOnlyImage = func(context.Context, client.APIClient, string) bool { return false }

	var pulled, upped *composetypes.Project
	pullProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.PullOptions) error {
		pulled = project
		return nil
	}
	upProject = func(_ context.Context, _ api.Compose, project *composetypes.Project, _ api.UpOptions) error {
		upped = project
		return nil
	}

	updated, images, err := c.CheckAndPullImages(t.Context(), "app-1", "myapp", false, ReleaseAgePolicy{MinimumAge: 24 * time.Hour})
	if !updated || err == nil {
		t.Fatalf("expected web to be updated and the failed lookups to be reported, got updated=%v err=%v", updated, err)
	}
	statuses := map[string]messages.ImageUpdateStatus{}
	for _, update := range images {
		statuses[update.Service] = update.Status
	}
	want := map[string]messages.ImageUpdateStatus{
		"web":    messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED,
		"worker": messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING,
		"db":     messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UP_TO_DATE,
		"cache":  messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED,
		// Only an image missing locally may be pulled without a check
		"queue": messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_FAILED,
	}
	if !maps.Equal(statuses, want) {
		t.Errorf("statuses = %v, want %v", statuses, want)
	}

	if got := pulled.Services["web"].PullPolicy; got != composetypes.PullPolicyAlways {
		t.Errorf("web must be pulled, got pull policy %q", got)
	}
	// The others keep their tag, so the next deploy doesn't recreate them.
	wantUp := map[string]string{
		"worker": "ghcr.io/org/worker:1.0",
		"db":     "postgres:17",
		"cache":  "redis:7",
		"queue":  "rabbitmq:3",
	}
	for name, ref := range wantUp {
		if got := pulled.Services[name].PullPolicy; got != composetypes.PullPolicyNever {
			t.Errorf("%s must not be pulled, got pull policy %q", name, got)
		}
		if got := upped.Services[name]; got.Image != ref || got.PullPolicy != composetypes.PullPolicyNever {
			t.Errorf("compose up ran %s with %q and pull policy %q, want %q", name, got.Image, got.PullPolicy, ref)
		}
	}
}

func TestSetMinimumAge_ReportsEnforcedMinimum(t *testing.T) {
	policy := ReleaseAgePolicy{EnforcedMinimumAge: 24 * time.Hour}
	service := composetypes.ServiceConfig{Name: "app", Image: "redis:7", Labels: composetypes.Labels{releaseAgeLabel: "1h"}}

	update := &messages.ImageUpdate{}
	age, err := policy.setMinimumAge(service, update)
	if err != nil || age != 24*time.Hour {
		t.Fatalf("setMinimumAge = %v, %v", age, err)
	}
	if !update.MinimumAgeEnforced || update.MinimumAgeFromLabel || update.MinimumAgeSeconds != 86400 {
		t.Errorf("unexpected update %+v", update)
	}

	service.Labels[releaseAgeLabel] = "2d"
	update = &messages.ImageUpdate{}
	if _, err := policy.setMinimumAge(service, update); err != nil {
		t.Fatal(err)
	}
	if update.MinimumAgeEnforced || !update.MinimumAgeFromLabel || update.MinimumAgeSeconds != 2*86400 {
		t.Errorf("unexpected update %+v", update)
	}
}

func TestReleaseAgePolicyFromProto_Enforced(t *testing.T) {
	policy := ReleaseAgePolicyFromProto(&messages.ReleaseAgePolicy{
		EnforcedMinimumSeconds:   3600,
		EnforcedExemptRegistries: []string{"registry.local"},
	}, nil)
	if policy.EnforcedMinimumAge != time.Hour || !slices.Equal(policy.EnforcedExemptRegistries, []string{"registry.local"}) {
		t.Errorf("unexpected policy %+v", policy)
	}
	if policy.equal(ReleaseAgePolicy{EnforcedMinimumAge: time.Hour}) {
		t.Error("expected policies with other exempt registries to differ")
	}
}
