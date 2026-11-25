package composeconfig

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/releaseage"
)

// Names of the settings, as the paths of their fields in the application
// response of the API.
const (
	SettingIcon                        = "icon"
	SettingImagePollEnabled            = "imagePollEnabled"
	SettingImagePollIntervalSeconds    = "imagePollIntervalSeconds"
	SettingDeleteOldImages             = "deleteOldImages"
	SettingRollbackEnabled             = "rollback.enabled"
	SettingRollbackOnDeployFailure     = "rollback.onDeployFailure"
	SettingRollbackOnUnhealthy         = "rollback.onUnhealthy"
	SettingRollbackHealthTimeout       = "rollback.healthTimeoutSeconds"
	SettingRollbackPauseAutoSync       = "rollback.pauseAutoSync"
	SettingReleaseAgeMinimum           = "releaseAge.minimumSeconds"
	SettingReleaseAgeAllowedRegistries = "releaseAge.allowedRegistries"
)

const maxIconLength = 128

// setting is a setting of an application that x-orcacd can manage.
type setting struct {
	name string
	// path is the key in x-orcacd, with dots between nested keys.
	path string
	// column is the field of models.Application, for selective updates.
	column string
	// def is the value the setting goes back to when it leaves x-orcacd,
	// the default of its column.
	def   any
	parse func(value any) (any, error)
	get   func(app *models.Application) any
	set   func(app *models.Application, value any)
}

// newSetting builds a setting of the application field that field returns.
func newSetting[T any](name, path, column string, def T, parse func(any) (T, error), field func(*models.Application) *T) setting {
	return setting{
		name:   name,
		path:   path,
		column: column,
		def:    def,
		parse:  func(value any) (any, error) { return parse(value) },
		get:    func(app *models.Application) any { return *field(app) },
		set:    func(app *models.Application, value any) { *field(app) = value.(T) },
	}
}

var settings = []setting{
	newSetting(SettingIcon, "icon", "Icon", "box", parseIcon,
		func(a *models.Application) *string { return &a.Icon }),
	newSetting(SettingImagePollEnabled, "image-poll.enabled", "ImagePollEnabled", false, parseBool,
		func(a *models.Application) *bool { return &a.ImagePollEnabled }),
	newSetting(SettingImagePollIntervalSeconds, "image-poll.interval", "ImagePollIntervalSeconds", int64(120),
		durationBetween(models.MinImagePollIntervalSeconds, models.MaxImagePollIntervalSeconds),
		func(a *models.Application) *int64 { return &a.ImagePollIntervalSeconds }),
	newSetting(SettingDeleteOldImages, "delete-old-images", "DeleteOldImages", false, parseBool,
		func(a *models.Application) *bool { return &a.DeleteOldImages }),
	newSetting(SettingRollbackEnabled, "rollback.enabled", "RollbackEnabled", false, parseBool,
		func(a *models.Application) *bool { return &a.RollbackEnabled }),
	newSetting(SettingRollbackOnDeployFailure, "rollback.on-deploy-failure", "RollbackOnDeployFailure", true, parseBool,
		func(a *models.Application) *bool { return &a.RollbackOnDeployFailure }),
	newSetting(SettingRollbackOnUnhealthy, "rollback.on-unhealthy", "RollbackOnUnhealthy", true, parseBool,
		func(a *models.Application) *bool { return &a.RollbackOnUnhealthy }),
	newSetting(SettingRollbackHealthTimeout, "rollback.health-timeout", "RollbackHealthTimeoutSeconds", int64(300),
		durationBetween(models.MinRollbackHealthTimeoutSeconds, models.MaxRollbackHealthTimeoutSeconds),
		func(a *models.Application) *int64 { return &a.RollbackHealthTimeoutSeconds }),
	newSetting(SettingRollbackPauseAutoSync, "rollback.pause-auto-sync", "RollbackPauseAutoSync", true, parseBool,
		func(a *models.Application) *bool { return &a.RollbackPauseAutoSync }),
	// The global minimum release age still applies when it is higher.
	newSetting(SettingReleaseAgeMinimum, "release-age.minimum", "MinimumReleaseAgeSeconds", (*int64)(nil), parseReleaseAge,
		func(a *models.Application) **int64 { return &a.MinimumReleaseAgeSeconds }),
	newSetting(SettingReleaseAgeAllowedRegistries, "release-age.allowed-registries", "ReleaseAgeAllowedRegistries", []string{}, parseRegistries,
		func(a *models.Application) *[]string { return &a.ReleaseAgeAllowedRegistries }),
}

// node is a key of x-orcacd: a group of keys or a setting.
type node struct {
	children map[string]*node
	setting  *setting
}

var settingsTree = buildTree()

func buildTree() *node {
	root := &node{children: map[string]*node{}}
	for i := range settings {
		current := root
		keys := strings.Split(settings[i].path, ".")
		for _, key := range keys[:len(keys)-1] {
			child, ok := current.children[key]
			if !ok {
				child = &node{children: map[string]*node{}}
				current.children[key] = child
			}
			current = child
		}
		current.children[keys[len(keys)-1]] = &node{setting: &settings[i]}
	}
	return root
}

func parseBool(value any) (bool, error) {
	b, ok := value.(bool)
	if !ok {
		return false, errors.New("must be true or false")
	}
	return b, nil
}

func parseIcon(value any) (string, error) {
	icon, ok := value.(string)
	if !ok || icon == "" || len(icon) > maxIconLength {
		return "", fmt.Errorf("must be an icon name of at most %d characters", maxIconLength)
	}
	return icon, nil
}

func parseRegistries(value any) ([]string, error) {
	const message = "must be a list of at most 50 registry hosts, e.g. registry.example.com:5000"
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New(message)
	}
	registries := make([]string, 0, len(items))
	for _, item := range items {
		registry, ok := item.(string)
		if !ok {
			return nil, errors.New(message)
		}
		registries = append(registries, registry)
	}
	normalized, ok := releaseage.NormalizeRegistries(registries)
	if !ok {
		return nil, errors.New(message)
	}
	return normalized, nil
}

// parseReleaseAge reads the minimum release age. null uses the global one.
func parseReleaseAge(value any) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	seconds, err := durationBetween(0, models.MaxMinimumReleaseAgeSeconds)(value)
	if err != nil {
		return nil, err
	}
	return &seconds, nil
}

// durationBetween parses a duration in seconds between minSeconds and
// maxSeconds.
func durationBetween(minSeconds, maxSeconds int64) func(any) (int64, error) {
	return func(value any) (int64, error) {
		seconds, err := parseDuration(value)
		if err != nil {
			return 0, err
		}
		if seconds < minSeconds {
			return 0, fmt.Errorf("must be at least %s", formatSeconds(minSeconds))
		}
		if seconds > maxSeconds {
			return 0, fmt.Errorf("must be at most %s", formatSeconds(maxSeconds))
		}
		return seconds, nil
	}
}

var daysPattern = regexp.MustCompile(`^(\d+)d(.*)$`)

// parseDuration reads a duration in whole seconds: a number of seconds or a
// string like "90s", "5m", "1h30m" or "7d".
func parseDuration(value any) (int64, error) {
	const message = `must be a duration like "30s", "5m", "2h" or "7d", or a number of seconds`
	switch v := value.(type) {
	case int:
		if v < 0 {
			return 0, errors.New(message)
		}
		return int64(v), nil
	case string:
		var days int64
		rest := strings.TrimSpace(v)
		match := daysPattern.FindStringSubmatch(rest)
		if match != nil {
			parsed, err := strconv.ParseInt(match[1], 10, 32)
			if err != nil {
				return 0, errors.New(message)
			}
			days, rest = parsed, match[2]
		}
		var duration time.Duration
		if rest != "" || match == nil {
			parsed, err := time.ParseDuration(rest)
			if err != nil || parsed < 0 || parsed%time.Second != 0 {
				return 0, errors.New(message)
			}
			duration = parsed
		}
		return days*24*60*60 + int64(duration/time.Second), nil
	default:
		return 0, errors.New(message)
	}
}

func formatSeconds(seconds int64) string {
	for _, unit := range []struct {
		suffix  string
		seconds int64
	}{{"d", 24 * 60 * 60}, {"h", 60 * 60}, {"m", 60}} {
		if seconds%unit.seconds == 0 {
			return strconv.FormatInt(seconds/unit.seconds, 10) + unit.suffix
		}
	}
	return strconv.FormatInt(seconds, 10) + "s"
}
