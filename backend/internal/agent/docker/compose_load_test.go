package docker

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateComposeFeaturesRejectsUnresolvedLocalReferences(t *testing.T) {
	tests := []struct {
		name        string
		composeFile string
		wantError   string
	}{
		{
			name:        "include",
			composeFile: "include:\n  - ../shared/compose.yaml\nservices:\n  app:\n    image: test:1\n",
			wantError:   `unsupported Compose feature "include"`,
		},
		{
			name:        "extends",
			composeFile: "services:\n  app:\n    image: test:1\n    extends:\n      file: ../shared/compose.yaml\n      service: base\n",
			wantError:   `unsupported Compose feature "extends" in service "app"`,
		},
		{
			name:        "label_file",
			composeFile: "services:\n  app:\n    image: test:1\n    label_file: ../shared/labels.env\n",
			wantError:   `unsupported Compose feature "label_file" in service "app"`,
		},
		{
			name:        "extends file in second YAML document",
			composeFile: "services:\n  app:\n    image: test:1\n---\nservices:\n  app:\n    extends:\n      file: ../shared/compose.yaml\n      service: base\n",
			wantError:   `unsupported Compose feature "extends" in service "app"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateComposeFeatures(tt.composeFile)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validateComposeFeatures() error = %v, want it to contain %q", err, tt.wantError)
			}
		})
	}
}

func TestValidateComposeFeaturesAllowsComposeWithoutLocalReferences(t *testing.T) {
	composeFile := `services:
  app:
    image: test:1
    labels:
      existing: value
`
	if err := validateComposeFeatures(composeFile); err != nil {
		t.Fatalf("validateComposeFeatures() error = %v", err)
	}
}

func TestValidateComposeFeaturesRejectsInvalidYAML(t *testing.T) {
	err := validateComposeFeatures("services:\n  app: [")
	if err == nil || !strings.Contains(err.Error(), "parse compose file:") {
		t.Fatalf("validateComposeFeatures() error = %v, want YAML parse error", err)
	}
}

func TestValidateComposeFeaturesAllowsMultipleDocumentsWithoutLocalReferences(t *testing.T) {
	composeFile := "services:\n  app:\n    image: test:1\n---\nservices:\n  worker:\n    image: test:1\n"
	if err := validateComposeFeatures(composeFile); err != nil {
		t.Fatalf("validateComposeFeatures() error = %v", err)
	}
}

func TestValidateComposeFeaturesAllowsExtendsWithinTheSameFile(t *testing.T) {
	tests := []struct {
		name        string
		composeFile string
	}{
		{
			name:        "service mapping",
			composeFile: "services:\n  base:\n    image: test:1\n  app:\n    extends:\n      service: base\n",
		},
		{
			name:        "short form",
			composeFile: "services:\n  base:\n    image: test:1\n  app:\n    extends: base\n",
		},
		{
			name:        "second YAML document",
			composeFile: "services:\n  base:\n    image: test:1\n---\nservices:\n  app:\n    extends:\n      service: base\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateComposeFeatures(tt.composeFile); err != nil {
				t.Fatalf("validateComposeFeatures() error = %v", err)
			}
		})
	}
}

// Uses the real Compose loader, to cover that same-file extends is still
// resolved with the agent's load options.
func TestComposeProjectLoadOptionsResolveExtendsWithinTheSameFile(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	composePath := filepath.Join(dir, composeFileName)
	writeTestFile(t, composePath, `services:
  base:
    image: test:1
    environment:
      FROM_BASE: "1"
  app:
    extends:
      service: base
`)

	project, err := loadProject(t.Context(), c.compose, composeProjectLoadOptions("extends-test", composePath, dir))
	if err != nil {
		t.Fatalf("loadProject() error = %v", err)
	}
	app := project.Services["app"]
	if app.Image != "test:1" {
		t.Errorf("app image = %q, want inherited %q", app.Image, "test:1")
	}
	if value := app.Environment["FROM_BASE"]; value == nil || *value != "1" {
		t.Errorf("app environment FROM_BASE = %v, want inherited \"1\"", value)
	}
}
