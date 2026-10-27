package composewarnings

import (
	"context"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

type Warning struct {
	Code    string `json:"code"`
	Service string `json:"service,omitempty"`
}

var databaseImages = map[string]struct{}{
	"cassandra":     {},
	"clickhouse":    {},
	"cockroachdb":   {},
	"couchbase":     {},
	"couchdb":       {},
	"elasticsearch": {},
	"influxdb":      {},
	"mariadb":       {},
	"memcached":     {},
	"mongo":         {},
	"mysql":         {},
	"neo4j":         {},
	"postgres":      {},
	"postgresql":    {},
	"redis":         {},
	"timescaledb":   {},
	"valkey":        {},
	"yugabytedb":    {},
}

func Analyze(composeFile string) []Warning {
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir: ".",
		ConfigFiles: []composetypes.ConfigFile{{
			Filename: "compose.yaml",
			Content:  []byte(composeFile),
		}},
		Environment: composetypes.Mapping{},
	}, func(options *loader.Options) {
		options.SetProjectName("application", true)
		options.SkipResolveEnvironment = true
		loader.WithDiscardEnvFiles(options)
	})
	if err != nil {
		return []Warning{{Code: "invalid_compose"}}
	}

	serviceNames := make([]string, 0, len(project.Services))
	for name := range project.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	warnings := make([]Warning, 0)
	for _, name := range serviceNames {
		service := project.Services[name]
		if service.Restart == "" && (service.Deploy == nil || service.Deploy.RestartPolicy == nil) {
			warnings = append(warnings, Warning{Code: "missing_restart_policy", Service: name})
		}
		if isDatabaseImage(service.Image) && len(service.Volumes) == 0 {
			warnings = append(warnings, Warning{Code: "database_without_volume", Service: name})
		}
		if hasFloatingImageReference(service.Image) {
			warnings = append(warnings, Warning{Code: "floating_image_reference", Service: name})
		}
		if service.Privileged {
			warnings = append(warnings, Warning{Code: "privileged_container", Service: name})
		}
		if strings.EqualFold(service.NetworkMode, "host") {
			warnings = append(warnings, Warning{Code: "host_network", Service: name})
		}
	}
	return warnings
}

func hasFloatingImageReference(image string) bool {
	if image == "" || strings.Contains(image, "@") {
		return false
	}

	lastSlash := strings.LastIndexByte(image, '/')
	lastColon := strings.LastIndexByte(image, ':')
	if lastColon <= lastSlash {
		return true
	}
	return strings.EqualFold(image[lastColon+1:], "latest")
}

func isDatabaseImage(image string) bool {
	image = strings.ToLower(image)
	lastSlash := strings.LastIndexByte(image, '/')
	if lastSlash >= 0 {
		image = image[lastSlash+1:]
	}
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	if colon := strings.IndexByte(image, ':'); colon >= 0 {
		image = image[:colon]
	}
	_, ok := databaseImages[image]
	return ok
}
