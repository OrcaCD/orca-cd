package version

import "testing"

func TestCheckAgentCompatibility(t *testing.T) {
	tests := []struct {
		name  string
		hub   string
		agent string
		want  AgentCompatibility
	}{
		{"not reported", "v0.5.0", "", AgentUnknown},
		{"same release", "v0.5.0", "v0.5.0", AgentUpToDate},
		{"same dev build", "dev", "dev", AgentUpToDate},
		{"agent newer than hub", "v0.5.0", "v0.6.0", AgentUpToDate},
		{"agent older than hub", "v0.6.0", "v0.5.0", AgentOutdated},
		{"agent older patch", "v0.5.1", "v0.5.0", AgentOutdated},
		{"agent below minimum", "v0.6.0", "v0.3.2", AgentIncompatible},
		{"agent at minimum", MinAgentVersion, MinAgentVersion, AgentUpToDate},
		{"version without v prefix", "v0.6.0", "0.5.0", AgentOutdated},
		{"git describe build counts as tag", "v0.5.0", "v0.5.0-3-gabc123-dirty", AgentUpToDate},
		{"dirty build counts as tag", "v0.5.0", "v0.5.0-dirty", AgentUpToDate},
		{"git describe hub build", "v0.5.0-3-gabc123", "v0.5.0", AgentUpToDate},
		{"pre-release older than release", "v0.5.0", "v0.5.0-rc.1", AgentOutdated},
		{"git describe on pre-release", "v0.5.0", "v0.5.0-rc.1-2-gabc123", AgentOutdated},
		{"pre-release hub", "v0.5.0-rc.1", "v0.5.0-rc.1-2-gabc123", AgentUpToDate},
		{"pre-release below minimum", "v0.5.0", "v0.4.0-rc.1", AgentIncompatible},
		{"non-release agent", "v0.5.0", "main", AgentUnknown},
		{"non-release hub", "dev", "v0.5.0", AgentUpToDate},
		{"non-release hub with old agent", "dev", "v0.3.0", AgentIncompatible},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckAgentCompatibility(tt.hub, tt.agent); got != tt.want {
				t.Errorf("CheckAgentCompatibility(%q, %q) = %q, want %q", tt.hub, tt.agent, got, tt.want)
			}
		})
	}
}

func TestSupportsEnvManagement(t *testing.T) {
	tests := []struct {
		agent string
		want  bool
	}{
		{"", false},
		{"v0.5.0", false},
		{"v0.5.9", false},
		{"v0.5.0-dirty", false},
		{"v0.5.0-11-g2d9317c", true},
		{"v0.5.0-11-g2d9317c-dirty", true},
		{"v0.6.0-rc.1", true},
		{"v0.6.0", true},
		{"0.7.1", true},
		{"dev", true},
		{"main", true},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			if got := SupportsEnvManagement(tt.agent); got != tt.want {
				t.Errorf("SupportsEnvManagement(%q) = %v, want %v", tt.agent, got, tt.want)
			}
		})
	}
}
