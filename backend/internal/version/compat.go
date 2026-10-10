package version

import (
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// MinAgentVersion is the oldest agent release the hub still works with. Bump it
// whenever a hub change requires agents to be upgraded (e.g. a breaking change
// to the Hub↔Agent protocol), so those agents are flagged as incompatible.
const MinAgentVersion = "v0.4.0"

type AgentCompatibility string

const (
	AgentUpToDate     AgentCompatibility = "upToDate"
	AgentOutdated     AgentCompatibility = "outdated"
	AgentIncompatible AgentCompatibility = "incompatible"
	// AgentUnknown is used when the agent did not report a version (agents
	// before version reporting was introduced) or reported a non-release build.
	AgentUnknown AgentCompatibility = "unknown"
)

// CheckAgentCompatibility classifies an agent version relative to the hub
// version and MinAgentVersion.
func CheckAgentCompatibility(hubVersion, agentVersion string) AgentCompatibility {
	if agentVersion == "" {
		return AgentUnknown
	}
	// Fast path that also covers identical non-release builds such as "dev".
	if agentVersion == hubVersion {
		return AgentUpToDate
	}

	agent, ok := normalize(agentVersion)
	if !ok {
		return AgentUnknown
	}
	if semver.Compare(agent, MinAgentVersion) < 0 {
		return AgentIncompatible
	}
	// Non-release hub builds (e.g. "dev", "main") can only be checked against
	// MinAgentVersion.
	if hub, ok := normalize(hubVersion); ok && semver.Compare(agent, hub) < 0 {
		return AgentOutdated
	}
	return AgentUpToDate
}

// AtLeast reports whether version is a release greater than or equal to min.
// It can be used to gate features that require a newer agent. Versions that
// are not valid semver never satisfy the check.
func AtLeast(version, minVersion string) bool {
	v, ok := normalize(version)
	if !ok {
		return false
	}
	m, ok := normalize(minVersion)
	if !ok {
		return false
	}
	return semver.Compare(v, m) >= 0
}

// gitDescribeSuffix matches what `git describe --tags --dirty` appends to the
// tag for local builds, e.g. "-3-gabc1234" or "-3-gabc1234-dirty".
var gitDescribeSuffix = regexp.MustCompile(`(?:-\d+-g[0-9a-f]+)?(?:-dirty)?$`)

// normalize converts a version to canonical semver. The `git describe` suffix
// is dropped because such builds are newer than their tag, while semver would
// order them before it as a pre-release. Real pre-releases (v0.5.0-rc.1) keep
// their suffix and therefore sort before the release.
func normalize(version string) (string, bool) {
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	canonical := semver.Canonical(gitDescribeSuffix.ReplaceAllString(version, ""))
	return canonical, canonical != ""
}
