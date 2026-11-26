package composeconfig

import (
	"errors"
	"fmt"
	"maps"
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

// parseServices reads x-orcacd of every compose service. Services that aren't
// mappings are left to compose, which reports them.
func parseServices(cfg *Config, raw any) error {
	services, ok := asMap(raw)
	if !ok {
		return nil
	}
	var errs []error
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
		for _, key := range sortedKeys(block) {
			if key != "release-age" {
				errs = append(errs, fmt.Errorf("%s: unknown key %s", path, quoteKey(key)))
				continue
			}
			seconds, err := durationBetween(0, models.MaxMinimumReleaseAgeSeconds)(block[key])
			if err != nil {
				errs = append(errs, fmt.Errorf("%s.%s: %w", path, key, err))
				continue
			}
			cfg.serviceReleaseAges[name] = seconds
		}
	}
	if len(cfg.serviceReleaseAges) > maxServiceReleaseAges {
		errs = append(errs, fmt.Errorf("at most %d services can set a release-age", maxServiceReleaseAges))
	}
	return errors.Join(errs...)
}

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

// applyServices sets the release ages of the services in x-orcacd. Overrides
// that x-orcacd set before but no longer does are removed, those set through
// the API stay.
func (c *Config) applyServices(app *models.Application) []string {
	overrides := maps.Clone(app.ReleaseAgeServiceOverrides)
	if overrides == nil {
		overrides = map[string]int64{}
	}
	for _, setting := range app.ManagedSettings {
		service, ok := strings.CutPrefix(setting, serviceReleaseAgePrefix)
		if _, set := c.serviceReleaseAges[service]; ok && !set {
			delete(overrides, service)
		}
	}
	maps.Copy(overrides, c.serviceReleaseAges)
	if maps.Equal(app.ReleaseAgeServiceOverrides, overrides) {
		return nil
	}
	app.ReleaseAgeServiceOverrides = overrides
	return []string{"ReleaseAgeServiceOverrides"}
}
