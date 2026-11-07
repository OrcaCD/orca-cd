package docker

import (
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
