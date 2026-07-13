package docker

import (
	"errors"
	"fmt"
	"strings"

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
}

// dangerous security_opt values
var unsafeSecurityOpts = map[string]struct{}{
	"seccomp:unconfined":  {},
	"apparmor:unconfined": {},
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

// inspects every service in the project and returns an
// aggregated error describing every policy violation found
func checkDeployPolicy(project *composetypes.Project) error {
	var violations []error
	for name, svc := range project.Services {
		violations = append(violations, checkServicePolicy(name, svc)...)
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("compose project violates deployment security policy: %w", errors.Join(violations...))
}

func checkServicePolicy(serviceName string, svc composetypes.ServiceConfig) []error {
	var violations []error
	violations = append(violations, checkPrivileged(serviceName, svc)...)
	violations = append(violations, checkCapabilities(serviceName, svc)...)
	violations = append(violations, checkHostNamespaces(serviceName, svc)...)
	violations = append(violations, checkBindMounts(serviceName, svc)...)
	violations = append(violations, checkDevices(serviceName, svc)...)
	violations = append(violations, checkSecurityOpt(serviceName, svc)...)
	return violations
}

func checkPrivileged(serviceName string, svc composetypes.ServiceConfig) []error {
	if svc.Privileged {
		return []error{fmt.Errorf("service %q: privileged mode is not allowed", serviceName)}
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
		if value == "host" {
			violations = append(violations, fmt.Errorf("service %q: %s: host is not allowed", serviceName, field))
		}
	}
	return violations
}

// checkBindMounts rejects bind mounts (not named volumes) whose source is, or
// is nested under, a sensitive host path.
// Also checks some paths for sensitive components (like .ssh).
//
// The paths are already resolved by the Docker compose library.
func checkBindMounts(serviceName string, svc composetypes.ServiceConfig) []error {
	var violations []error
	for _, vol := range svc.Volumes {
		if vol.Type != composetypes.VolumeTypeBind {
			continue
		}
		if underSensitiveComponentRoot(vol.Source) {
			if component, sensitive := checkForSensitivePathComponent(vol.Source); sensitive {
				violations = append(violations, fmt.Errorf("service %q: bind mount source %q contains sensitive path component %q", serviceName, vol.Source, component))
				continue
			}
		}
		for _, denied := range sensitiveHostPaths {
			if vol.Source == denied || strings.HasPrefix(vol.Source, denied+"/") {
				violations = append(violations, fmt.Errorf("service %q: bind mount of sensitive host path %q is not allowed", serviceName, vol.Source))
				break
			}
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
		normalized := strings.ToLower(strings.TrimSpace(opt))
		normalized = strings.Replace(normalized, "=", ":", 1)
		if _, unsafe := unsafeSecurityOpts[normalized]; unsafe {
			violations = append(violations, fmt.Errorf("service %q: security_opt %q is not allowed", serviceName, opt))
		}
	}
	return violations
}
