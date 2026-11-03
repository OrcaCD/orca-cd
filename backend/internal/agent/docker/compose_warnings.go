package docker

import (
	"path"
	"sort"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

var databaseImages = map[string]struct{}{
	"cassandra":     {},
	"clickhouse":    {},
	"cockroachdb":   {},
	"couchbase":     {},
	"couchdb":       {},
	"elasticsearch": {},
	"influxdb":      {},
	"mariadb":       {},
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

var databaseDataDirectories = map[string][]string{
	"cassandra":     {"/var/lib/cassandra"},
	"clickhouse":    {"/var/lib/clickhouse"},
	"cockroachdb":   {"/cockroach/cockroach-data"},
	"couchbase":     {"/opt/couchbase/var"},
	"couchdb":       {"/opt/couchdb/data"},
	"elasticsearch": {"/usr/share/elasticsearch/data"},
	"influxdb":      {"/var/lib/influxdb", "/var/lib/influxdb2"},
	"mariadb":       {"/var/lib/mysql"},
	"mongo":         {"/data/db"},
	"mysql":         {"/var/lib/mysql"},
	"neo4j":         {"/data"},
	"postgres":      {"/var/lib/postgresql/data"},
	"postgresql":    {"/var/lib/postgresql/data"},
	"redis":         {"/data"},
	"timescaledb":   {"/var/lib/postgresql/data"},
	"valkey":        {"/data"},
	"yugabytedb":    {"/home/yugabyte/yb-data"},
}

func analyzeComposeWarnings(project *composetypes.Project) []composewarnings.Warning {
	serviceNames := make([]string, 0, len(project.Services))
	for name := range project.Services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	warnings := make([]composewarnings.Warning, 0)
	for _, name := range serviceNames {
		service := project.Services[name]
		if service.Restart == "" && (service.Deploy == nil || service.Deploy.RestartPolicy == nil) {
			warnings = append(warnings, composewarnings.Warning{Code: "missing_restart_policy", Service: name})
		}
		if isDatabaseImage(service.Image) && !hasPersistentDataMount(service.Image, service.Volumes) {
			warnings = append(warnings, composewarnings.Warning{Code: "database_without_volume", Service: name})
		}
		if hasFloatingImageReference(service.Image) {
			warnings = append(warnings, composewarnings.Warning{Code: "floating_image_reference", Service: name})
		}
		if service.Privileged {
			warnings = append(warnings, composewarnings.Warning{Code: "privileged_container", Service: name})
		}
		for _, namespace := range []struct {
			field string
			value string
		}{
			{field: "network_mode", value: service.NetworkMode},
			{field: "pid", value: service.Pid},
			{field: "ipc", value: service.Ipc},
			{field: "uts", value: service.Uts},
			{field: "userns_mode", value: service.UserNSMode},
			{field: "cgroup", value: service.Cgroup},
		} {
			normalized := strings.ToLower(strings.TrimSpace(namespace.value))
			switch {
			case normalized == "host":
				if namespace.field == "network_mode" {
					warnings = append(warnings, composewarnings.Warning{Code: "host_network", Service: name})
				} else {
					warnings = append(warnings, composewarnings.Warning{Code: "host_namespace", Service: name, Field: namespace.field})
				}
			case strings.HasPrefix(normalized, "container:"):
				warnings = append(warnings, composewarnings.Warning{Code: "host_namespace", Service: name, Field: namespace.field})
			}
		}
	}
	return warnings
}

func hasFloatingImageReference(image string) bool {
	if image == "" || strings.Contains(image, "@") || strings.Contains(image, "$") {
		return false
	}

	lastSlash := strings.LastIndexByte(image, '/')
	lastColon := strings.LastIndexByte(image, ':')
	if lastColon <= lastSlash {
		return true
	}
	return strings.EqualFold(image[lastColon+1:], "latest")
}

func hasPersistentDataMount(image string, volumes []composetypes.ServiceVolumeConfig) bool {
	dataDirectories := databaseDataDirectories[databaseImageName(image)]
	for _, volume := range volumes {
		if volume.Type != composetypes.VolumeTypeVolume && volume.Type != composetypes.VolumeTypeBind {
			continue
		}
		target := path.Clean(volume.Target)
		for _, dataDirectory := range dataDirectories {
			dataDirectory = path.Clean(dataDirectory)
			if target == "/" || target == dataDirectory || strings.HasPrefix(dataDirectory, target+"/") {
				return true
			}
		}
	}
	return false
}

func isDatabaseImage(image string) bool {
	_, ok := databaseImages[databaseImageName(image)]
	return ok
}

func databaseImageName(image string) string {
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
	return image
}
