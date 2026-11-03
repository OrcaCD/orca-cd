package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-spec/compose-go/v2/cli"
)

func TestComposeProjectLoadOptionsDoNotResolveLocalReferences(t *testing.T) {
	workingDir := t.TempDir()
	missingPath := filepath.Join(t.TempDir(), "missing.yaml")
	composePath := filepath.Join(workingDir, composeFileName)
	composeFile := `include:
  - "` + missingPath + `"
services:
  app:
    image: test:1
    extends:
      file: "` + missingPath + `"
      service: base
    label_file: "` + missingPath + `"
`
	if err := os.WriteFile(composePath, []byte(composeFile), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	options := composeProjectLoadOptions("test", composePath, workingDir)
	projectOptions, err := cli.NewProjectOptions(
		options.ConfigPaths,
		append([]cli.ProjectOptionsFn{
			cli.WithWorkingDirectory(options.WorkingDir),
			cli.WithName(options.ProjectName),
		}, options.ProjectOptionsFns...)...,
	)
	if err != nil {
		t.Fatalf("create project options: %v", err)
	}
	project, err := projectOptions.LoadProject(t.Context())
	if err != nil {
		t.Fatalf("load project without resolving local references: %v", err)
	}
	if _, ok := project.Services["app"]; !ok {
		t.Fatal("expected app service to remain in loaded project")
	}
}

func TestComposeProjectLoadOptionsRetainServiceLabels(t *testing.T) {
	workingDir := t.TempDir()
	composePath := filepath.Join(workingDir, composeFileName)
	composeFile := `services:
  app:
    image: test:1
    labels:
      existing: value
    label_file: missing-labels.yaml
`
	if err := os.WriteFile(composePath, []byte(composeFile), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}

	options := composeProjectLoadOptions("test", composePath, workingDir)
	projectOptions, err := cli.NewProjectOptions(
		options.ConfigPaths,
		append([]cli.ProjectOptionsFn{
			cli.WithWorkingDirectory(options.WorkingDir),
			cli.WithName(options.ProjectName),
		}, options.ProjectOptionsFns...)...,
	)
	if err != nil {
		t.Fatalf("create project options: %v", err)
	}
	project, err := projectOptions.LoadProject(t.Context())
	if err != nil {
		t.Fatalf("load project with label_file skipped: %v", err)
	}
	service := project.Services["app"]
	if service.Labels["existing"] != "value" {
		t.Fatalf("expected inline labels to be retained, got %#v", service.Labels)
	}
	if _, ok := service.Labels["label_file"]; ok {
		t.Fatalf("label_file was unexpectedly resolved into labels: %#v", service.Labels)
	}
}
