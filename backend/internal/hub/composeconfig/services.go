package composeconfig

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
)

// serviceReleaseAgePrefix names the release age of a service in
// ManagedSettings, followed by the service name.
const serviceReleaseAgePrefix = "releaseAge.serviceOverrides."

// maxServiceReleaseAges matches the API's limit of service overrides.
const maxServiceReleaseAges = 100

// ServiceReleaseAgeSetting is the setting of the minimum release age of a
// compose service.
func ServiceReleaseAgeSetting(service string) string {
	return serviceReleaseAgePrefix + service
}

// serviceConfig is x-orcacd of a compose service. Nil fields are not set.
type serviceConfig struct {
	imagePoll  *bool
	health     *bool
	releaseAge *int64
}

// parseServices reads x-orcacd of every compose service. Services that aren't
// mappings are left to compose, which reports them.
func parseServices(cfg *Config, raw any) error {
	services, ok := asMap(raw)
	if !ok {
		return nil
	}
	var errs []error
	releaseAges := 0
	for _, name := range sortedKeys(services) {
		service, ok := asMap(services[name])
		if !ok || service[Key] == nil {
			continue
		}
		path := "services." + quoteKey(name) + "." + Key
		block, ok := asMap(service[Key])
		if !ok {
			errs = append(errs, fmt.Errorf("%s: must be a mapping", path))
			continue
		}
		var sc serviceConfig
		for _, key := range sortedKeys(block) {
			var err error
			switch key {
			case "image-poll":
				sc.imagePoll, err = pointer(parseBool(block[key]))
			case "health":
				sc.health, err = pointer(parseBool(block[key]))
			case "release-age":
				sc.releaseAge, err = pointer(durationBetween(0, models.MaxMinimumReleaseAgeSeconds)(block[key]))
				releaseAges++
			default:
				err = errUnknownKey
			}
			if errors.Is(err, errUnknownKey) {
				errs = append(errs, fmt.Errorf("%s: unknown key %s", path, quoteKey(key)))
			} else if err != nil {
				errs = append(errs, fmt.Errorf("%s.%s: %w", path, key, err))
			}
		}
		cfg.services[name] = sc
	}
	if releaseAges > maxServiceReleaseAges {
		errs = append(errs, fmt.Errorf("at most %d services can set a release-age", maxServiceReleaseAges))
	}
	return errors.Join(errs...)
}

var errUnknownKey = errors.New("unknown key")

// asMap returns a YAML mapping with string keys. Mappings with keys that
// aren't strings, like a service named 2048, decode with keys of any type.
func asMap(value any) (map[string]any, bool) {
	switch m := value.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		converted := make(map[string]any, len(m))
		for key, item := range m {
			converted[fmt.Sprint(key)] = item
		}
		return converted, true
	}
	return nil, false
}

func pointer[T any](value T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// servicesWhere returns the services that turn a setting off.
func (c *Config) servicesWhere(off func(serviceConfig) *bool) []string {
	names := []string{}
	for _, name := range sortedKeys(c.services) {
		if value := off(c.services[name]); value != nil && !*value {
			names = append(names, name)
		}
	}
	return names
}

// applyServices sets the service settings of the application. The services
// left out of image polling and health only come from x-orcacd, the release
// ages of services can also be set through the API.
func (c *Config) applyServices(app *models.Application) []string {
	var changed []string
	excluded := c.servicesWhere(func(s serviceConfig) *bool { return s.imagePoll })
	if !slices.Equal(app.ImagePollExcludedServices, excluded) {
		app.ImagePollExcludedServices = excluded
		changed = append(changed, "ImagePollExcludedServices")
	}
	ignored := c.servicesWhere(func(s serviceConfig) *bool { return s.health })
	if !slices.Equal(app.HealthIgnoredServices, ignored) {
		app.HealthIgnoredServices = ignored
		changed = append(changed, "HealthIgnoredServices")
	}

	overrides := maps.Clone(app.ReleaseAgeServiceOverrides)
	if overrides == nil {
		overrides = map[string]int64{}
	}
	for _, setting := range app.ManagedSettings {
		service, ok := strings.CutPrefix(setting, serviceReleaseAgePrefix)
		if ok && c.services[service].releaseAge == nil {
			delete(overrides, service)
		}
	}
	for service, sc := range c.services {
		if sc.releaseAge != nil {
			overrides[service] = *sc.releaseAge
		}
	}
	if !maps.Equal(app.ReleaseAgeServiceOverrides, overrides) {
		app.ReleaseAgeServiceOverrides = overrides
		changed = append(changed, "ReleaseAgeServiceOverrides")
	}
	return changed
}
