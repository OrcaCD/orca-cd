package docker

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// Linux capabilities that grant meaningful host access when added via cap_add.
var dangerousCapabilities = map[string]struct{}{
	"ALL":             {},
	"SYS_ADMIN":       {},
	"SYS_MODULE":      {},
	"SYS_PTRACE":      {},
	"SYS_RAWIO":       {},
	"SYS_BOOT":        {},
	"NET_ADMIN":       {},
	"BPF":             {},
	"PERFMON":         {},
	"DAC_READ_SEARCH": {},
	"DAC_OVERRIDE":    {},
	"MAC_ADMIN":       {},
	"MAC_OVERRIDE":    {},
}

// Paths that can lead to host compromise if bind-mounted into a container
var sensitiveHostPaths = []string{
	"/",
	"/etc",
	"/boot",
	"/root",
	"/sys",
	"/proc",
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/dev",
	"/var/run",
	"/run",
	"/var/run/docker.sock",
	"/run/docker.sock",
	"/var/lib/docker",
	"/var/lib/containerd",
}

// security_opt keys that are harmless to set. Every other key (seccomp, apparmor,
// label, systempaths, ...) can weaken container isolation and is rejected.
var allowedSecurityOptKeys = map[string]struct{}{
	"no-new-privileges": {},
}

// Top-level directories under which sensitivePathComponents is checked.
var sensitiveComponentRoots = []string{
	"/home",
}

// Path components that are sensitive when found under sensitiveComponentRoots
var sensitivePathComponents = map[string]struct{}{
	".ssh":            {},
	".aws":            {},
	".kube":           {},
	".docker":         {},
	".gnupg":          {},
	"id_rsa":          {},
	"id_dsa":          {},
	"id_ecdsa":        {},
	"id_ed25519":      {},
	"authorized_keys": {},
	"shadow":          {},
	"gshadow":         {},
	"passwd":          {},
	"sudoers":         {},
	"sudoers.d":       {},
	"cron.d":          {},
	"cron.hourly":     {},
	"cron.daily":      {},
	"cron.weekly":     {},
	"cron.monthly":    {},
	"pam.d":           {},
	"systemd":         {},
	"networkmanager":  {},
}

// pathRules configures the checks for paths that are read from the host.
type pathRules struct {
	// restrictMountsDir switches to allow-list mode if set: every host path
	// must be inside it, and the sensitive-path deny-list is skipped since it
	// becomes redundant.
	restrictMountsDir string
	// deploymentsDir and applicationDir confine paths inside the deployments
	// directory to the application's own directory, see deploymentPathViolation.
	deploymentsDir     string
	applicationDir     string
	hostDeploymentsDir string
}

// inspects every service in the project and returns an
// aggregated error describing every policy violation found
func checkDeployPolicy(project *composetypes.Project, rules pathRules) error {
	var violations []error
	for name, svc := range project.Services {
		violations = append(violations, checkServicePolicy(name, svc, rules)...)
	}
	violations = append(violations, checkVolumeDefinitions(project)...)
	violations = append(violations, checkFileObjectDefinitions(project, rules)...)
	violations = append(violations, checkJobs(project)...)
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("compose project violates deployment security policy: %w", errors.Join(violations...))
}

func checkServicePolicy(serviceName string, svc composetypes.ServiceConfig, rules pathRules) []error {
	violations := checkContainerPolicy(serviceName, svc, rules)
	violations = append(violations, checkProvider(serviceName, svc)...)
	violations = append(violations, checkHooks(serviceName, svc, rules)...)
	return violations
}

// checkContainerPolicy checks the fields of a container specification, which
// services and pre_start hooks share.
func checkContainerPolicy(serviceName string, svc composetypes.ServiceConfig, rules pathRules) []error {
	var violations []error
	violations = append(violations, checkPrivileged(serviceName, svc)...)
	violations = append(violations, checkAPISocket(serviceName, svc)...)
	violations = append(violations, checkCapabilities(serviceName, svc)...)
	violations = append(violations, checkHostNamespaces(serviceName, svc)...)
	violations = append(violations, checkBindMounts(serviceName, svc, rules)...)
	violations = append(violations, checkDevices(serviceName, svc)...)
	violations = append(violations, checkSecurityOpt(serviceName, svc)...)
	violations = append(violations, checkVolumesFrom(serviceName, svc)...)
	violations = append(violations, checkDeviceCgroupRules(serviceName, svc)...)
	violations = append(violations, checkEnvFile(serviceName, svc, rules)...)
	return violations
}

// checkHooks applies the policy to lifecycle hooks. A pre_start hook is a full
// container specification that compose creates like a service container, so it
// gets the same checks. post_start and pre_stop hooks run as an exec in the
// service container, where privileged would grant every capability.
func checkHooks(serviceName string, svc composetypes.ServiceConfig, rules pathRules) []error {
	var violations []error
	for i, hook := range svc.PreStart {
		hookName := fmt.Sprintf("%s.pre_start[%d]", serviceName, i)
		violations = append(violations, checkContainerPolicy(hookName, composetypes.ServiceConfig{ContainerSpec: hook.ContainerSpec}, rules)...)
	}
	for i, hook := range svc.PostStart {
		if hook.Privileged {
			violations = append(violations, fmt.Errorf("service %q: post_start[%d]: privileged hooks are not allowed", serviceName, i))
		}
	}
	for i, hook := range svc.PreStop {
		if hook.Privileged {
			violations = append(violations, fmt.Errorf("service %q: pre_stop[%d]: privileged hooks are not allowed", serviceName, i))
		}
	}
	return violations
}

// Jobs are full container specifications that the agent never runs, so they are
// rejected instead of being checked. A later compose version could run them.
func checkJobs(project *composetypes.Project) []error {
	var violations []error
	for name := range project.AllJobs() {
		violations = append(violations, fmt.Errorf("job %q: jobs are not allowed", name))
	}
	return violations
}

// Top-level volumes with driver_opts (e.g. type=none, o=bind, device=/) are a
// bind mount in disguise and bypass the bind mount checks.
func checkVolumeDefinitions(project *composetypes.Project) []error {
	var violations []error
	for name, vol := range project.Volumes {
		if len(vol.DriverOpts) > 0 {
			violations = append(violations, fmt.Errorf("volume %q: driver_opts are not allowed", name))
		}
	}
	return violations
}

// Top-level secrets and configs with a file source are bind-mounted read-only
// from that host path, so they get the same rules as bind mounts.
func checkFileObjectDefinitions(project *composetypes.Project, rules pathRules) []error {
	var violations []error
	for name, secret := range project.Secrets {
		if secret.File == "" {
			continue
		}
		if reason := hostPathViolation(secret.File, rules); reason != "" {
			violations = append(violations, fmt.Errorf("secret %q: file %q %s", name, secret.File, reason))
		}
	}
	for name, config := range project.Configs {
		if config.File == "" {
			continue
		}
		if reason := hostPathViolation(config.File, rules); reason != "" {
			violations = append(violations, fmt.Errorf("config %q: file %q %s", name, config.File, reason))
		}
	}
	return violations
}

// volumes_from can inherit the mounts of any container on the host, including
// the agent's Docker socket.
func checkVolumesFrom(serviceName string, svc composetypes.ServiceConfig) []error {
	if len(svc.VolumesFrom) > 0 {
		return []error{fmt.Errorf("service %q: volumes_from is not allowed", serviceName)}
	}
	return nil
}

func checkDeviceCgroupRules(serviceName string, svc composetypes.ServiceConfig) []error {
	if len(svc.DeviceCgroupRules) > 0 {
		return []error{fmt.Errorf("service %q: device_cgroup_rules are not allowed", serviceName)}
	}
	return nil
}

func checkPrivileged(serviceName string, svc composetypes.ServiceConfig) []error {
	if svc.Privileged {
		return []error{fmt.Errorf("service %q: privileged mode is not allowed", serviceName)}
	}
	return nil
}

// A provider service makes compose run provider.type as an executable inside the
// agent, which has access to the Docker API.
func checkProvider(serviceName string, svc composetypes.ServiceConfig) []error {
	if svc.Provider != nil {
		return []error{fmt.Errorf("service %q: provider services are not allowed", serviceName)}
	}
	return nil
}

// use_api_socket bind-mounts the Docker API socket (and its auth) into the
// container, granting the same host control as mounting docker.sock directly.
func checkAPISocket(serviceName string, svc composetypes.ServiceConfig) []error {
	if svc.UseAPISocket {
		return []error{fmt.Errorf("service %q: use_api_socket is not allowed", serviceName)}
	}
	return nil
}

func checkCapabilities(serviceName string, svc composetypes.ServiceConfig) []error {
	var violations []error
	for _, capability := range svc.CapAdd {
		normalized := strings.ToUpper(strings.TrimSpace(capability))
		normalized = strings.TrimPrefix(normalized, "CAP_")
		if _, dangerous := dangerousCapabilities[normalized]; dangerous {
			violations = append(violations, fmt.Errorf("service %q: cap_add %q is not allowed", serviceName, capability))
		}
	}
	return violations
}

// checkHostNamespaces rejects sharing a namespace with the host or with another
// container: joining the agent container's PID namespace exposes its
// environment and filesystem, including the Docker socket.
func checkHostNamespaces(serviceName string, svc composetypes.ServiceConfig) []error {
	var violations []error
	namespaces := map[string]string{
		"network_mode": svc.NetworkMode,
		"pid":          svc.Pid,
		"ipc":          svc.Ipc,
		"uts":          svc.Uts,
		"userns_mode":  svc.UserNSMode,
		"cgroup":       svc.Cgroup,
	}
	for field, value := range namespaces {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch {
		case normalized == "host":
			violations = append(violations, fmt.Errorf("service %q: %s: host is not allowed", serviceName, field))
		case strings.HasPrefix(normalized, "container:"):
			violations = append(violations, fmt.Errorf("service %q: %s: joining another container's namespace is not allowed", serviceName, field))
		}
	}
	return violations
}

// checkBindMounts rejects bind mounts (not named volumes) whose source is, or
// is nested under, a sensitive host path.
// Also checks some paths for sensitive components (like .ssh).
// The paths are already resolved by the Docker compose library.
func checkBindMounts(serviceName string, svc composetypes.ServiceConfig, rules pathRules) []error {
	var violations []error
	for _, vol := range svc.Volumes {
		if vol.Type != composetypes.VolumeTypeBind {
			continue
		}
		if reason := hostPathViolation(vol.Source, rules); reason != "" {
			violations = append(violations, fmt.Errorf("service %q: bind mount source %q %s", serviceName, vol.Source, reason))
		}
	}
	return violations
}

// hostPathViolation applies the bind mount rules to a path that is read from
// the host, and returns why it is not allowed, or "" if it is.
func hostPathViolation(path string, rules pathRules) string {
	if rules.restrictMountsDir != "" {
		if err := utils.IsPathWithinBase(rules.restrictMountsDir, path); err != nil {
			return fmt.Sprintf("is not inside the agent's deployment directory %q", rules.restrictMountsDir)
		}
	} else if reason := sensitivePathReason(path); reason != "" {
		return reason
	}
	return deploymentPathViolation(path, rules)
}

// sensitivePathReason returns why a path is on the deny-list, or "" if it is not.
// The path is cleaned first, because compose keeps absolute paths as written
// while the kernel resolves "." and ".." segments.
func sensitivePathReason(source string) string {
	if !filepath.IsAbs(source) {
		return "is not an absolute path"
	}
	source = filepath.Clean(source)
	if underSensitiveComponentRoot(source) {
		if component, sensitive := checkForSensitivePathComponent(source); sensitive {
			return fmt.Sprintf("contains sensitive path component %q", component)
		}
		if isComponentRootOrDirectChild(source) {
			return "is a home directory, which contains sensitive paths"
		}
	}
	for _, denied := range sensitiveHostPaths {
		if source == denied || strings.HasPrefix(source, denied+"/") {
			return fmt.Sprintf("is under sensitive host path %q, which is not allowed", denied)
		}
		// A parent directory exposes the sensitive path below it. "/" itself is
		// matched exactly above.
		if denied != "/" && strings.HasPrefix(denied, source+"/") {
			return fmt.Sprintf("contains sensitive host path %q, which is not allowed", denied)
		}
	}
	return ""
}

// checkEnvFile applies the same rules as checkBindMounts to env_file, since it
// is read from the agent's filesystem (e.g. /proc/self/environ holds the
// agent's token). The path is read as is and not by the Docker daemon, so it is
// not mapped from the host's deployments directory.
func checkEnvFile(serviceName string, svc composetypes.ServiceConfig, rules pathRules) []error {
	rules.hostDeploymentsDir = ""
	var violations []error
	for _, envFile := range svc.EnvFiles {
		if reason := hostPathViolation(envFile.Path, rules); reason != "" {
			violations = append(violations, fmt.Errorf("service %q: env_file %q %s", serviceName, envFile.Path, reason))
		}
	}
	return violations
}

func underSensitiveComponentRoot(source string) bool {
	for _, root := range sensitiveComponentRoots {
		if source == root || strings.HasPrefix(source, root+"/") {
			return true
		}
	}
	return false
}

// isComponentRootOrDirectChild reports whether source is a root like /home or
// a directory directly below it like /home/alice, which holds .ssh and the
// other sensitive components.
func isComponentRootOrDirectChild(source string) bool {
	for _, root := range sensitiveComponentRoots {
		if source == root || filepath.Dir(source) == root {
			return true
		}
	}
	return false
}

func checkForSensitivePathComponent(source string) (string, bool) {
	for part := range strings.SplitSeq(source, "/") {
		if part == "" {
			continue
		}
		if _, sensitive := sensitivePathComponents[strings.ToLower(part)]; sensitive {
			return part, true
		}
	}
	return "", false
}

func checkDevices(serviceName string, svc composetypes.ServiceConfig) []error {
	if len(svc.Devices) > 0 {
		return []error{fmt.Errorf("service %q: device mappings are not allowed", serviceName)}
	}
	return nil
}

func checkSecurityOpt(serviceName string, svc composetypes.ServiceConfig) []error {
	var violations []error
	for _, opt := range svc.SecurityOpt {
		key, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(opt)), "=")
		key, _, _ = strings.Cut(key, ":")
		if _, allowed := allowedSecurityOptKeys[key]; !allowed {
			violations = append(violations, fmt.Errorf("service %q: security_opt %q is not allowed", serviceName, opt))
		}
	}
	return violations
}
