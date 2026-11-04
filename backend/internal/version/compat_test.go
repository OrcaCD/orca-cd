package version

import "testing"

func TestCheckAgentCompatibilitySkipMinVersion(t *testing.T) {
	SkipMinAgentVersion = true
	t.Cleanup(func() { SkipMinAgentVersion = false })

	tests := []struct {
		name  string
		hub   string
		agent string
		want  AgentCompatibility
	}{
		{"below minimum is not incompatible", "v0.8.0", "v0.5.0-30-gabc123", AgentOutdated},
		{"still outdated against hub", "v0.8.0", "v0.7.0", AgentOutdated},
		{"non-release hub", "dev", "v0.3.0", AgentUpToDate},
		{"not reported", "v0.8.0", "", AgentUnknown},
		{"non-release agent", "v0.8.0", "main", AgentUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckAgentCompatibility(tt.hub, tt.agent); got != tt.want {
				t.Errorf("CheckAgentCompatibility(%q, %q) = %q, want %q", tt.hub, tt.agent, got, tt.want)
			}
		})
	}
}

func TestCheckAgentCompatibility(t *testing.T) {
	tests := []struct {
		name  string
		hub   string
		agent string
		want  AgentCompatibility
	}{
		{"not reported", "v0.7.0", "", AgentUnknown},
		{"same release", "v0.7.0", "v0.7.0", AgentUpToDate},
		{"same dev build", "dev", "dev", AgentUpToDate},
		{"agent newer than hub", "v0.7.0", "v0.8.0", AgentUpToDate},
		{"agent older than hub", "v0.8.0", "v0.7.0", AgentOutdated},
		{"agent older patch", "v0.7.1", "v0.7.0", AgentOutdated},
		{"agent below minimum", "v0.8.0", "v0.3.2", AgentIncompatible},
		{"agent without env support", "v0.8.0", "v0.5.9", AgentIncompatible},
		{"agent at minimum", MinAgentVersion, MinAgentVersion, AgentUpToDate},
		{"release candidate of minimum", "v0.8.0", "v0.6.0-rc.1", AgentOutdated},
		{"version without v prefix", "v0.8.0", "0.7.0", AgentOutdated},
		{"git describe build counts as tag", "v0.7.0", "v0.7.0-3-gabc123-dirty", AgentUpToDate},
		{"dirty build counts as tag", "v0.7.0", "v0.7.0-dirty", AgentUpToDate},
		{"git describe hub build", "v0.7.0-3-gabc123", "v0.7.0", AgentUpToDate},
		{"pre-release older than release", "v0.7.0", "v0.7.0-rc.1", AgentOutdated},
		{"git describe on pre-release", "v0.7.0", "v0.7.0-rc.1-2-gabc123", AgentOutdated},
		{"pre-release hub", "v0.7.0-rc.1", "v0.7.0-rc.1-2-gabc123", AgentUpToDate},
		{"pre-release below minimum", "v0.7.0", "v0.5.0-rc.1", AgentIncompatible},
		{"non-release agent", "v0.7.0", "main", AgentUnknown},
		{"non-release hub", "dev", "v0.7.0", AgentUpToDate},
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
