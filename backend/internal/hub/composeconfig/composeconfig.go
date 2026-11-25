// Package composeconfig reads the settings of an application from the
// x-orcacd extension of its compose file. Compose ignores fields starting with
// x-, so the file still works with docker compose up.
//
// A setting in x-orcacd is managed by Git: it is applied on every sync and
// can't be changed through the API. When it is removed from the file, the
// setting goes back to its default.
package composeconfig

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"go.yaml.in/yaml/v3"
)

// Key is the name of the compose extension.
const Key = "x-orcacd"

// Config holds the settings found in x-orcacd, by setting name.
type Config struct {
	values map[string]any
	// unreadable is set for a file that isn't valid YAML.
	unreadable bool
}

// maxComposeSize is the largest compose file that is parsed. Larger files
// keep their settings, so a huge file can't exhaust the memory of the hub.
const maxComposeSize = 1 << 20

// Parse reads x-orcacd from a compose file. A file without x-orcacd gives an
// empty config, which resets the settings Git managed before. A file that
// isn't valid YAML leaves the settings as they are, the agent reports it when
// it deploys the file.
func Parse(compose string) (*Config, error) {
	cfg := &Config{values: map[string]any{}}
	var document map[string]any
	if len(compose) > maxComposeSize || yaml.Unmarshal([]byte(compose), &document) != nil {
		cfg.unreadable = true
		return cfg, nil
	}
	raw, ok := document[Key]
	if !ok || raw == nil {
		return cfg, nil
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", Key)
	}
	errs := []error{parseGroup(cfg, block, settingsTree, Key)}
	if cfg.values[SettingRollbackOnDeployFailure] == false && cfg.values[SettingRollbackOnUnhealthy] == false {
		errs = append(errs, fmt.Errorf("%s.rollback: on-deploy-failure and on-unhealthy can't both be false", Key))
	}
	if err := joinErrors(errs); err != nil {
		return nil, err
	}
	return cfg, nil
}

// maxErrors and maxErrorLength bound the error of a compose file, which is
// shown on the application and sent in notifications.
const (
	maxErrors      = 10
	maxErrorLength = 2000
)

func joinErrors(errs []error) error {
	var messages []string
	for _, err := range errs {
		if err != nil {
			messages = append(messages, strings.Split(err.Error(), "\n")...)
		}
	}
	if len(messages) == 0 {
		return nil
	}
	if len(messages) > maxErrors {
		messages = append(messages[:maxErrors], fmt.Sprintf("and %d more", len(messages)-maxErrors))
	}
	message := strings.Join(messages, "\n")
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength] + "..."
	}
	return errors.New(message)
}

// quoteKey quotes a key from the compose file for an error message.
func quoteKey(key string) string {
	const maxKeyLength = 64
	if len(key) > maxKeyLength {
		key = key[:maxKeyLength] + "..."
	}
	return fmt.Sprintf("%q", key)
}

// Managed returns the names of the settings in the config.
func (c *Config) Managed() []string {
	names := make([]string, 0, len(c.values))
	for _, s := range settings {
		if _, ok := c.values[s.name]; ok {
			names = append(names, s.name)
		}
	}
	return names
}

// Apply sets the settings of the application from the config. Settings that
// were managed before but are no longer in the config go back to their
// defaults. It returns the columns it changed, and always updates
// ManagedSettings.
func (c *Config) Apply(app *models.Application) []string {
	if c.unreadable {
		return nil
	}
	var changed []string
	for _, s := range settings {
		before := s.get(app)
		if value, ok := c.values[s.name]; ok {
			s.set(app, value)
		} else if slices.Contains(app.ManagedSettings, s.name) {
			s.set(app, s.def)
		} else {
			continue
		}
		if !equal(before, s.get(app)) {
			changed = append(changed, s.column)
		}
	}
	app.ManagedSettings = c.Managed()
	return changed
}

// CopySettings copies the settings x-orcacd can change, and which of them
// it manages, from src to dst.
func CopySettings(dst, src *models.Application) {
	for _, s := range settings {
		s.set(dst, s.get(src))
	}
	dst.ManagedSettings = src.ManagedSettings
}

// ManagedChanges returns the settings managed by Git whose value differs
// between the application as stored and as a request would change it.
func ManagedChanges(stored, requested *models.Application) []string {
	var changed []string
	for _, s := range settings {
		if slices.Contains(stored.ManagedSettings, s.name) && !equal(s.get(stored), s.get(requested)) {
			changed = append(changed, s.name)
		}
	}
	return changed
}

// equal compares setting values, with a nil list equal to an empty one.
func equal(a, b any) bool {
	if listA, ok := a.([]string); ok {
		listB, ok := b.([]string)
		return ok && slices.Equal(listA, listB)
	}
	return reflect.DeepEqual(a, b)
}

// ManagedError is the error message for a request that changes settings
// managed by Git.
func ManagedError(names []string) string {
	return "managed by " + Key + " in the compose file: " + strings.Join(names, ", ")
}

// SameWithoutConfig reports whether two compose files only differ in
// x-orcacd, so applying their settings doesn't need a deployment. The files
// are compared as decoded, with their aliases expanded, so a value that
// x-orcacd shares through an anchor counts as a change. Files with several
// YAML documents, which compose merges, never count as the same.
func SameWithoutConfig(a, b string) bool {
	documentA, okA := decodeWithoutConfig(a)
	documentB, okB := decodeWithoutConfig(b)
	return okA && okB && reflect.DeepEqual(documentA, documentB)
}

func decodeWithoutConfig(compose string) (map[string]any, bool) {
	if len(compose) > maxComposeSize {
		return nil, false
	}
	decoder := yaml.NewDecoder(strings.NewReader(compose))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, false
	}
	var next any
	if err := decoder.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, false
	}
	delete(document, Key)
	return document, true
}

// parseGroup reads a mapping of x-orcacd against the keys allowed there.
func parseGroup(cfg *Config, block map[string]any, group *node, path string) error {
	var errs []error
	for _, key := range sortedKeys(block) {
		value := block[key]
		keyPath := path + "." + key
		child, ok := group.children[key]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: unknown key %s", path, quoteKey(key)))
			continue
		}
		if child.setting != nil {
			parsed, err := child.setting.parse(value)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", keyPath, err))
				continue
			}
			cfg.values[child.setting.name] = parsed
			continue
		}
		switch v := value.(type) {
		case map[string]any:
			if err := parseGroup(cfg, v, child, keyPath); err != nil {
				errs = append(errs, err)
			}
		case bool:
			// "rollback: true" is short for "rollback: {enabled: true}".
			enabled, ok := child.children["enabled"]
			if !ok {
				errs = append(errs, fmt.Errorf("%s: must be a mapping", keyPath))
				continue
			}
			cfg.values[enabled.setting.name] = v
		default:
			errs = append(errs, fmt.Errorf("%s: must be a mapping", keyPath))
		}
	}
	return errors.Join(errs...)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
