package version

import "testing"

func TestAgentImageTag(t *testing.T) {
	tests := []struct {
		name string
		hub  string
		want string
	}{
		{"release", "v0.9.2", "0.9.2"},
		{"release without v prefix", "0.9.2", "0.9.2"},
		{"pre-release", "v0.10.0-rc.1", "0.10.0-rc.1"},
		{"git describe build", "v0.9.2-3-gabc123-dirty", "0.9.2"},
		{"main build", "main", "main"},
		{"dev build", "dev", "latest"},
		{"empty", "", "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentImageTag(tt.hub); got != tt.want {
				t.Errorf("agentImageTag(%q) = %q, want %q", tt.hub, got, tt.want)
			}
		})
	}
}

func TestAgentImage(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = "v1.2.3"
	if got, want := AgentImage(), "ghcr.io/orcacd/agent:1.2.3"; got != want {
		t.Errorf("AgentImage() = %q, want %q", got, want)
	}
}
