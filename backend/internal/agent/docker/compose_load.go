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
		if extendsOtherFile(service["extends"]) {
			return &unsupportedComposeFeatureError{Feature: "extends", Service: name}
		}
		if _, ok := service["label_file"]; ok {
			return &unsupportedComposeFeatureError{Feature: "label_file", Service: name}
		}
	}
	return nil
}

func extendsOtherFile(extends any) bool {
	options, ok := extends.(map[string]any)
	if !ok {
		return false
	}
	_, ok = options["file"]
	return ok
}

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

func composeProjectLoadOptions(projectName, composePath, workingDir string, extraOptions ...cli.ProjectOptionsFn) api.ProjectLoadOptions {
	return api.ProjectLoadOptions{
		ProjectName: projectName,
		ConfigPaths: []string{composePath},
		WorkingDir:  workingDir,
		ProjectOptionsFns: append([]cli.ProjectOptionsFn{
			cli.WithLoadOptions(func(options *loader.Options) {
				options.SkipInclude = true
				options.SkipResolveLabels = true
			}),
		}, extraOptions...),
	}
}
