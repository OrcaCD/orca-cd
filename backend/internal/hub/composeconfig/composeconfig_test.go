package composeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

func defaultApplication() models.Application {
	return models.Application{
		Icon:                         "box",
		ImagePollIntervalSeconds:     120,
		RollbackOnDeployFailure:      true,
		RollbackOnUnhealthy:          true,
		RollbackHealthTimeoutSeconds: 300,
		RollbackPauseAutoSync:        true,
		ReleaseAgeAllowedRegistries:  []string{},
		ManagedSettings:              []string{},
	}
}

func mustParse(t *testing.T, compose string) *Config {
	t.Helper()
	cfg, err := Parse(compose)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return cfg
}

func TestParse_AppliesAllSettings(t *testing.T) {
	cfg := mustParse(t, `
x-orcacd:
  icon: chart-line
  image-poll:
    enabled: true
    interval: 5m
  delete-old-images: true
  rollback:
    enabled: true
    on-deploy-failure: false
    on-unhealthy: true
    health-timeout: 90
    pause-auto-sync: false
  release-age:
    minimum: 3d
    allowed-registries: [GHCR.io, ghcr.io, registry.example.com:5000]

services:
  web:
    image: nginx
`)
	app := defaultApplication()
	changed := cfg.Apply(&app)

	minimum := int64(3 * 24 * 60 * 60)
	want := models.Application{
		Icon:                         "chart-line",
		ImagePollEnabled:             true,
		ImagePollIntervalSeconds:     300,
		DeleteOldImages:              true,
		RollbackEnabled:              true,
		RollbackOnUnhealthy:          true,
		RollbackHealthTimeoutSeconds: 90,
		MinimumReleaseAgeSeconds:     &minimum,
		ReleaseAgeAllowedRegistries:  []string{"ghcr.io", "registry.example.com:5000"},
		ManagedSettings:              app.ManagedSettings,
	}
	if !reflect.DeepEqual(app, want) {
		t.Errorf("Apply() = %+v, want %+v", app, want)
	}
	if len(app.ManagedSettings) != len(settings) {
		t.Errorf("ManagedSettings = %v, want all %d settings", app.ManagedSettings, len(settings))
	}
	// on-unhealthy keeps its default value, so its column doesn't change.
	if len(changed) != len(settings)-1 || slices.Contains(changed, "RollbackOnUnhealthy") {
		t.Errorf("changed columns = %v, want all but RollbackOnUnhealthy", changed)
	}
}

func TestParse_BooleanShorthand(t *testing.T) {
	cfg := mustParse(t, "x-orcacd:\n  image-poll: true\n  rollback: true\n")
	app := defaultApplication()
	cfg.Apply(&app)
	if !app.ImagePollEnabled || !app.RollbackEnabled {
		t.Errorf("expected image polling and rollback to be enabled, got %+v", app)
	}
	if want := []string{SettingImagePollEnabled, SettingRollbackEnabled}; !slices.Equal(app.ManagedSettings, want) {
		t.Errorf("ManagedSettings = %v, want %v", app.ManagedSettings, want)
	}
}

func TestParse_ReleaseAgeNullUsesGlobal(t *testing.T) {
	cfg := mustParse(t, "x-orcacd:\n  release-age:\n    minimum: null\n")
	app := defaultApplication()
	own := int64(60)
	app.MinimumReleaseAgeSeconds = &own
	cfg.Apply(&app)
	if app.MinimumReleaseAgeSeconds != nil {
		t.Errorf("MinimumReleaseAgeSeconds = %v, want nil", *app.MinimumReleaseAgeSeconds)
	}
	if !slices.Contains(app.ManagedSettings, SettingReleaseAgeMinimum) {
		t.Errorf("expected %s to be managed, got %v", SettingReleaseAgeMinimum, app.ManagedSettings)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := map[string]struct {
		compose string
		want    []string
	}{
		"not a mapping":   {"x-orcacd: true\n", []string{"x-orcacd must be a mapping"}},
		"unknown key":     {"x-orcacd:\n  sync: auto\n", []string{`x-orcacd: unknown key "sync"`}},
		"unknown nested":  {"x-orcacd:\n  rollback:\n    enable: true\n", []string{`x-orcacd.rollback: unknown key "enable"`}},
		"wrong type":      {"x-orcacd:\n  delete-old-images: yes\n", []string{"x-orcacd.delete-old-images: must be true or false"}},
		"group not map":   {"x-orcacd:\n  release-age: 3d\n", []string{"x-orcacd.release-age: must be a mapping"}},
		"short interval":  {"x-orcacd:\n  image-poll:\n    interval: 30s\n", []string{"x-orcacd.image-poll.interval: must be at least 1m"}},
		"long timeout":    {"x-orcacd:\n  rollback:\n    health-timeout: 2h\n", []string{"x-orcacd.rollback.health-timeout: must be at most 1h"}},
		"bad duration":    {"x-orcacd:\n  rollback:\n    health-timeout: soon\n", []string{"x-orcacd.rollback.health-timeout: must be a duration"}},
		"long release":    {"x-orcacd:\n  release-age:\n    minimum: 400d\n", []string{"x-orcacd.release-age.minimum: must be at most 365d"}},
		"bad registry":    {"x-orcacd:\n  release-age:\n    allowed-registries: [ghcr.io/org]\n", []string{"x-orcacd.release-age.allowed-registries: must be a list"}},
		"empty icon":      {"x-orcacd:\n  icon: \"\"\n", []string{"x-orcacd.icon: must be an icon name"}},
		"both triggers":   {"x-orcacd:\n  rollback:\n    on-deploy-failure: false\n    on-unhealthy: false\n", []string{"x-orcacd.rollback: on-deploy-failure and on-unhealthy can't both be false"}},
		"long interval":   {"x-orcacd:\n  image-poll:\n    interval: 31d\n", []string{"x-orcacd.image-poll.interval: must be at most 30d"}},
		"several invalid": {"x-orcacd:\n  icon: 1\n  foo: bar\n", []string{`x-orcacd: unknown key "foo"`, "x-orcacd.icon: must be an icon name"}},
		"notifications":   {"x-orcacd:\n  notifications: ops\n", []string{"x-orcacd.notifications: must be a list"}},
		"empty name":      {"x-orcacd:\n  notifications: [\"\"]\n", []string{"x-orcacd.notifications: must be a list"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tt.compose)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestParse_Durations(t *testing.T) {
	tests := map[string]int64{
		"0":       0,
		"90":      90,
		"\"90s\"": 90,
		"1h30m":   90 * 60,
		"7d":      7 * 24 * 60 * 60,
		"1d12h":   36 * 60 * 60,
		"0d":      0,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			cfg := mustParse(t, "x-orcacd:\n  release-age:\n    minimum: "+value+"\n")
			app := defaultApplication()
			cfg.Apply(&app)
			if app.MinimumReleaseAgeSeconds == nil || *app.MinimumReleaseAgeSeconds != want {
				t.Errorf("minimum = %v, want %d", app.MinimumReleaseAgeSeconds, want)
			}
		})
	}
	for _, value := range []string{"-5", "1.5s", "d", "1x", "\"\""} {
		t.Run("invalid "+value, func(t *testing.T) {
			if _, err := Parse("x-orcacd:\n  release-age:\n    minimum: " + value + "\n"); err == nil {
				t.Errorf("expected %s to be rejected", value)
			}
		})
	}
}

func TestApply_RemovedSettingGoesBackToDefault(t *testing.T) {
	app := defaultApplication()
	mustParse(t, "x-orcacd:\n  icon: rocket\n  rollback:\n    enabled: true\n").Apply(&app)

	changed := mustParse(t, "x-orcacd:\n  icon: rocket\n").Apply(&app)
	if app.RollbackEnabled {
		t.Error("expected rollback to go back to disabled")
	}
	if app.Icon != "rocket" {
		t.Errorf("Icon = %q, want rocket", app.Icon)
	}
	if !slices.Equal(changed, []string{"RollbackEnabled"}) {
		t.Errorf("changed = %v, want [RollbackEnabled]", changed)
	}
	if !slices.Equal(app.ManagedSettings, []string{SettingIcon}) {
		t.Errorf("ManagedSettings = %v, want [icon]", app.ManagedSettings)
	}
}

func TestApply_UnmanagedSettingsAreKept(t *testing.T) {
	app := defaultApplication()
	app.DeleteOldImages = true
	app.Icon = "rocket"

	changed := mustParse(t, "services:\n  web:\n    image: nginx\n").Apply(&app)
	if !app.DeleteOldImages || app.Icon != "rocket" || len(changed) != 0 {
		t.Errorf("expected settings set through the API to be kept, got %+v, changed %v", app, changed)
	}
}

func TestApply_UnreadableFileKeepsSettings(t *testing.T) {
	app := defaultApplication()
	mustParse(t, "x-orcacd:\n  rollback: true\n").Apply(&app)

	changed := mustParse(t, "services: [\n").Apply(&app)
	if !app.RollbackEnabled || !slices.Equal(app.ManagedSettings, []string{SettingRollbackEnabled}) || changed != nil {
		t.Errorf("expected an unreadable file to keep the settings, got %+v", app)
	}
}

func TestManagedChanges(t *testing.T) {
	stored := defaultApplication()
	mustParse(t, "x-orcacd:\n  rollback:\n    enabled: true\n").Apply(&stored)

	requested := stored
	requested.DeleteOldImages = true
	if changes := ManagedChanges(&stored, &requested); len(changes) != 0 {
		t.Errorf("expected changes of unmanaged settings to pass, got %v", changes)
	}

	requested.RollbackEnabled = false
	if changes := ManagedChanges(&stored, &requested); !slices.Equal(changes, []string{SettingRollbackEnabled}) {
		t.Errorf("ManagedChanges() = %v, want [%s]", changes, SettingRollbackEnabled)
	}
}

func TestSameWithoutConfig(t *testing.T) {
	base := "services:\n  web:\n    image: nginx\n"
	withConfig := "x-orcacd:\n  rollback: true\n\n" + base
	otherConfig := "x-orcacd:\n  icon: rocket\n" + base

	if !SameWithoutConfig(base, withConfig) || !SameWithoutConfig(withConfig, otherConfig) {
		t.Error("expected files that only differ in x-orcacd to be the same")
	}
	if SameWithoutConfig(withConfig, strings.Replace(withConfig, "nginx", "caddy", 1)) {
		t.Error("expected a changed service to differ")
	}
	if SameWithoutConfig("services: [\n", "services: [\n") {
		t.Error("expected unreadable files to differ")
	}
}

// The schema has to accept exactly the keys the parser reads.
func TestSchemaMatchesSettings(t *testing.T) {
	data, err := os.ReadFile("compose.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	var paths []string
	collectSchemaPaths(t, schema.Defs["orcacd"], "", &paths)
	slices.Sort(paths)

	want := []string{keyNotifications}
	for _, s := range settings {
		want = append(want, s.path)
	}
	slices.Sort(want)
	if !slices.Equal(paths, want) {
		t.Errorf("schema keys = %v, want %v", paths, want)
	}
}

func collectSchemaPaths(t *testing.T, raw json.RawMessage, prefix string, paths *[]string) {
	t.Helper()
	var def struct {
		Properties map[string]json.RawMessage `json:"properties"`
		OneOf      []json.RawMessage          `json:"oneOf"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	// A group with a boolean shorthand lists its keys in its object variant.
	for _, variant := range def.OneOf {
		var object struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(variant, &object) == nil && object.Type == "object" {
			def.Properties = object.Properties
		}
	}
	if len(def.Properties) == 0 {
		*paths = append(*paths, strings.TrimPrefix(prefix, "."))
		return
	}
	for key, child := range def.Properties {
		collectSchemaPaths(t, child, prefix+"."+key, paths)
	}
}

func TestParse_LimitsErrors(t *testing.T) {
	var compose strings.Builder
	compose.WriteString("x-orcacd:\n")
	for i := range 20 {
		fmt.Fprintf(&compose, "  key%02d-%s: 1\n", i, strings.Repeat("x", 100))
	}
	_, err := Parse(compose.String())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "and 10 more") || len(err.Error()) > maxErrorLength+3 {
		t.Errorf("expected at most 10 errors of bounded length, got %q", err)
	}
	if strings.Contains(err.Error(), strings.Repeat("x", 100)) {
		t.Error("expected long keys to be shortened")
	}
}

func TestParse_LargeFileKeepsSettings(t *testing.T) {
	compose := "x-orcacd:\n  rollback: true\nx-padding: " + strings.Repeat("a", maxComposeSize) + "\n"
	app := defaultApplication()
	if changed := mustParse(t, compose).Apply(&app); changed != nil || app.RollbackEnabled {
		t.Errorf("expected a file over the size limit to be left alone, got %v", changed)
	}
}

func TestSameWithoutConfig_ComparesDecodedFiles(t *testing.T) {
	anchored := func(icon string) string {
		return "x-orcacd:\n  icon: &image " + icon + "\nservices:\n  web:\n    image: *image\n"
	}
	if SameWithoutConfig(anchored("nginx"), anchored("caddy")) {
		t.Error("expected a value shared through an anchor to count as a change")
	}
	base := "services:\n  web:\n    image: nginx\n"
	if SameWithoutConfig(base, "x-orcacd:\n  icon: rocket\n"+base+"---\nservices:\n  web:\n    image: caddy\n") {
		t.Error("expected a file with several documents never to count as the same")
	}
	if !SameWithoutConfig(base, "# comment\nservices:\n    web:\n        image: nginx\n") {
		t.Error("expected comments and indentation not to count as changes")
	}
}

func TestParse_Notifications(t *testing.T) {
	cfg := mustParse(t, "x-orcacd:\n  notifications: [ops, \" ops \", alerts]\n")
	if !slices.Equal(cfg.notifications, []string{"ops", "alerts"}) {
		t.Errorf("notifications = %v, want [ops alerts]", cfg.notifications)
	}
	app := defaultApplication()
	cfg.Apply(&app)
	if !slices.Equal(app.ManagedSettings, []string{SettingNotifications}) {
		t.Errorf("ManagedSettings = %v, want [notifications]", app.ManagedSettings)
	}

	// Removing the key clears the notifications on save.
	empty := mustParse(t, "services: {}\n")
	empty.Apply(&app)
	if !empty.clearNotifications || len(app.ManagedSettings) != 0 {
		t.Errorf("expected the notifications to be cleared, got clear=%v managed=%v", empty.clearNotifications, app.ManagedSettings)
	}
	// Without having been managed, they are left as they are.
	other := mustParse(t, "services: {}\n")
	other.Apply(&app)
	if other.clearNotifications {
		t.Error("expected notifications set through the API to be kept")
	}
}

func TestParse_Services(t *testing.T) {
	cfg := mustParse(t, `
services:
  web:
    image: nginx
    x-orcacd:
      image-poll: false
      release-age: 2d
  worker:
    image: worker
    x-orcacd:
      health: false
      image-poll: true
  db:
    image: postgres
`)
	app := defaultApplication()
	app.ReleaseAgeServiceOverrides = map[string]int64{"db": 60}
	changed := cfg.Apply(&app)

	if !slices.Equal(app.ImagePollExcludedServices, []string{"web"}) {
		t.Errorf("ImagePollExcludedServices = %v, want [web]", app.ImagePollExcludedServices)
	}
	if !slices.Equal(app.HealthIgnoredServices, []string{"worker"}) {
		t.Errorf("HealthIgnoredServices = %v, want [worker]", app.HealthIgnoredServices)
	}
	if want := map[string]int64{"db": 60, "web": 2 * 24 * 60 * 60}; !reflect.DeepEqual(app.ReleaseAgeServiceOverrides, want) {
		t.Errorf("ReleaseAgeServiceOverrides = %v, want %v", app.ReleaseAgeServiceOverrides, want)
	}
	if !slices.Equal(app.ManagedSettings, []string{ServiceReleaseAgeSetting("web")}) {
		t.Errorf("ManagedSettings = %v", app.ManagedSettings)
	}
	if len(changed) != 3 {
		t.Errorf("changed = %v, want the three service columns", changed)
	}

	// Removing x-orcacd of the services removes their settings, but keeps
	// release ages set through the API.
	changed = mustParse(t, "services:\n  web:\n    image: nginx\n").Apply(&app)
	if len(app.ImagePollExcludedServices) != 0 || len(app.HealthIgnoredServices) != 0 {
		t.Errorf("expected no excluded services, got %v and %v", app.ImagePollExcludedServices, app.HealthIgnoredServices)
	}
	if want := map[string]int64{"db": 60}; !reflect.DeepEqual(app.ReleaseAgeServiceOverrides, want) {
		t.Errorf("ReleaseAgeServiceOverrides = %v, want %v", app.ReleaseAgeServiceOverrides, want)
	}
	if len(changed) != 3 || len(app.ManagedSettings) != 0 {
		t.Errorf("changed = %v, managed = %v", changed, app.ManagedSettings)
	}
}

func TestParse_ServiceErrors(t *testing.T) {
	_, err := Parse("services:\n  web:\n    x-orcacd:\n      image-poll: no\n      restart: always\n  api:\n    x-orcacd: true\n")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{
		`services."web".x-orcacd.image-poll: must be true or false`,
		`services."web".x-orcacd: unknown key "restart"`,
		`services."api".x-orcacd: must be a mapping`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestSameWithoutConfig_Services(t *testing.T) {
	base := "services:\n  web:\n    image: nginx\n"
	withService := "services:\n  web:\n    image: nginx\n    x-orcacd:\n      health: false\n"
	if !SameWithoutConfig(base, withService) {
		t.Error("expected files that only differ in x-orcacd of a service to be the same")
	}
}

func TestSchemaMatchesServiceSettings(t *testing.T) {
	data, err := os.ReadFile("compose.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	var paths []string
	collectSchemaPaths(t, schema.Defs["service"], "", &paths)
	slices.Sort(paths)
	if want := []string{"health", "image-poll", "release-age"}; !slices.Equal(paths, want) {
		t.Errorf("schema keys = %v, want %v", paths, want)
	}
}

func TestParse_NumericServiceName(t *testing.T) {
	cfg := mustParse(t, "services:\n  2048:\n    image: game\n  web:\n    x-orcacd:\n      health: false\n")
	app := defaultApplication()
	cfg.Apply(&app)
	if !slices.Equal(app.HealthIgnoredServices, []string{"web"}) {
		t.Errorf("HealthIgnoredServices = %v, want [web]", app.HealthIgnoredServices)
	}
}
