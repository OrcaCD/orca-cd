package docker

import (
	"fmt"
	"sort"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/docker/compose/v5/pkg/api"
	"go.yaml.in/yaml/v3"
)

func validateComposeFeatures(composeFile string) error {
	var document map[string]any
	if err := yaml.Unmarshal([]byte(composeFile), &document); err != nil {
		return fmt.Errorf("parse compose file: %w", err)
	}
	if _, ok := document["include"]; ok {
		return fmt.Errorf(`unsupported Compose feature "include": local Compose references are not resolved; deployment was rejected`)
	}

	services, ok := document["services"].(map[string]any)
	if !ok {
		return nil
	}
	serviceNames := make([]string, 0, len(services))
	for name := range services {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, name := range serviceNames {
		service, ok := services[name].(map[string]any)
		if !ok {
			continue
		}
		for _, feature := range []string{"extends", "label_file"} {
			if _, ok := service[feature]; ok {
				return fmt.Errorf(`unsupported Compose feature "%s" in service "%s": local Compose references are not resolved; deployment was rejected`, feature, name)
			}
		}
	}
	return nil
}

func composeProjectLoadOptions(projectName, composePath, workingDir string) api.ProjectLoadOptions {
	return api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  workingDir,
		ProjectOptionsFns: []cli.ProjectOptionsFn{
			cli.WithLoadOptions(func(options *loader.Options) {
				// Local file references are rejected by validateComposeFeatures.
				options.SkipInclude = true
				options.SkipExtends = true
				options.SkipResolveLabels = true
			}),
		},
	}
}
