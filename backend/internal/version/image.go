package version

import "strings"

const agentImageRepository = "ghcr.io/orcacd/agent"

// AgentImage returns the agent image that matches this hub build.
func AgentImage() string {
	return agentImageRepository + ":" + agentImageTag(Version)
}

func agentImageTag(hubVersion string) string {
	// Builds from main are published under the "main" tag.
	if hubVersion == "main" {
		return "main"
	}
	// Release images are tagged without the "v" prefix.
	if v, ok := normalize(hubVersion); ok {
		return strings.TrimPrefix(v, "v")
	}
	return "latest"
}
