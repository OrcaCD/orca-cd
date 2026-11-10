package docker

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/docker/compose/v5/pkg/api"
	"go.yaml.in/yaml/v3"
)

type unsupportedComposeFeatureError struct {
	Feature string
	Service string
}

func (e *unsupportedComposeFeatureError) Error() string {
	if e.Service == "" {
		return fmt.Sprintf(`unsupported Compose feature "%s": local Compose references are not resolved; deployment was rejected`, e.Feature)
	}
	return fmt.Sprintf(`unsupported Compose feature "%s" in service "%s": local Compose references are not resolved; deployment was rejected`, e.Feature, e.Service)
}

func validateComposeFeatures(composeFile string) error {
	decoder := yaml.NewDecoder(strings.NewReader(composeFile))
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parse compose file: %w", err)
		}
		if err := validateComposeDocument(document); err != nil {
			return err
		}
	}
}

func validateComposeDocument(document map[string]any) error {
	if _, ok := document["include"]; ok {
		return &unsupportedComposeFeatureError{Feature: "include"}
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
				return &unsupportedComposeFeatureError{Feature: feature, Service: name}
			}
		}
	}
	return nil
}

// composeValidationWarnings turns a validateComposeFeatures error into the
// warning shown in the hub UI.
func composeValidationWarnings(err error) []composewarnings.Warning {
	if unsupported, ok := errors.AsType[*unsupportedComposeFeatureError](err); ok {
		return []composewarnings.Warning{{
			Code:    "unsupported_compose_feature",
			Service: unsupported.Service,
			Field:   unsupported.Feature,
		}}
	}
	return []composewarnings.Warning{{Code: "invalid_compose"}}
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
