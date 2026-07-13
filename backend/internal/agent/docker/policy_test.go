package docker

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func projectWithService(svc composetypes.ServiceConfig) *composetypes.Project {
	return &composetypes.Project{
		Services: composetypes.Services{"app": svc},
	}
}

func TestCheckDeployPolicy_AllowsSafeService(t *testing.T) {
	project := projectWithService(composetypes.ServiceConfig{
		Image: "ghcr.io/orcacd/app:1.0.0",
		Volumes: []composetypes.ServiceVolumeConfig{
			{Type: composetypes.VolumeTypeBind, Source: "/data", Target: "/data"},
			{Type: composetypes.VolumeTypeVolume, Source: "app-data", Target: "/var/lib/app"},
		},
	})
	if err := checkDeployPolicy(project); err != nil {
		t.Fatalf("expected safe service to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsPrivileged(t *testing.T) {
	project := projectWithService(composetypes.ServiceConfig{Privileged: true})
	if err := checkDeployPolicy(project); err == nil {
		t.Fatal("expected privileged service to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsDangerousCapability(t *testing.T) {
	tests := []string{"SYS_ADMIN", "cap_sys_admin", "NET_ADMIN", "ALL", "sys_ptrace", "CAP_SYS_MODULE"}
	for _, capability := range tests {
		t.Run(capability, func(t *testing.T) {
			project := projectWithService(composetypes.ServiceConfig{CapAdd: []string{capability}})
			if err := checkDeployPolicy(project); err == nil {
				t.Fatalf("expected cap_add %q to be rejected", capability)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsCapDropAllPlusSafeCapAdd(t *testing.T) {
	project := projectWithService(composetypes.ServiceConfig{
		CapDrop: []string{"ALL"},
		CapAdd:  []string{"NET_BIND_SERVICE"},
	})
	if err := checkDeployPolicy(project); err != nil {
		t.Fatalf("expected cap_drop:[ALL]+cap_add:[NET_BIND_SERVICE] hardening pattern to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsHostNamespaceModes(t *testing.T) {
	tests := []struct {
		name string
		svc  composetypes.ServiceConfig
	}{
		{"network_mode", composetypes.ServiceConfig{NetworkMode: "host"}},
		{"pid", composetypes.ServiceConfig{Pid: "host"}},
		{"ipc", composetypes.ServiceConfig{Ipc: "host"}},
		{"uts", composetypes.ServiceConfig{Uts: "host"}},
		{"userns_mode", composetypes.ServiceConfig{UserNSMode: "host"}},
		{"cgroup", composetypes.ServiceConfig{Cgroup: "host"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkDeployPolicy(projectWithService(tt.svc)); err == nil {
				t.Fatalf("expected %s: host to be rejected", tt.name)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsSensitiveBindMounts(t *testing.T) {
	tests := []string{"/", "/etc", "/etc/foo", "/root", "/var/run/docker.sock", "/run/docker.sock", "/sys", "/proc"}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc)); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected", source)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsNonSensitiveBindMounts(t *testing.T) {
	tests := []string{
		"/data", "/home/user/app-data", "/etcetera", "/etc-data",
		"/data/.ssh-backup", "/data/passwords", "/data/mykubeconfig",
		"/opt/app/.ssh", "/opt/app/systemd", "/srv/app/.aws",
		"/data/.kube", "/data/.docker", "/data/.gnupg", "/mnt/NetworkManager",
		"/mnt/backup/etc/shadow", "/mnt/backup/etc/passwd", "/mnt/backup/etc/sudoers",
	}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc)); err != nil {
				t.Fatalf("expected bind mount of %q to be allowed, got: %v", source, err)
			}
		})
	}
}

func loadComposeProject(t *testing.T, workingDir, yaml string) *composetypes.Project {
	t.Helper()
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir: workingDir,
		ConfigFiles: []composetypes.ConfigFile{
			{Filename: "compose.yaml", Content: []byte(yaml)},
		},
	})
	if err != nil {
		t.Fatalf("failed to load compose project: %v", err)
	}
	return project
}

func TestCheckDeployPolicy_ResolvesRelativeBindMountsBeforeChecking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path resolution is OS-native; only meaningful on a Unix-like runner matching production")
	}

	tests := []struct {
		name       string
		source     string
		wantReject bool
	}{
		{"traversal escapes working dir to a sensitive host path", "../../etc/shadow", true},
		{"traversal escapes working dir to a sensitive path component", "../../home/alice/.ssh", true},
		{"relative path stays within a safe app data dir", "data", false},
		{"traversal stays within a safe sibling dir", "../sibling-app/data", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
name: demo
services:
  app:
    image: ghcr.io/orcacd/app:1.0.0
    volumes:
      - type: bind
        source: %s
        target: /mnt
`, tt.source)
			project := loadComposeProject(t, "/srv/app", yaml)
			err := checkDeployPolicy(project)
			if tt.wantReject && err == nil {
				t.Fatalf("expected bind mount source %q to be rejected after resolution", tt.source)
			}
			if !tt.wantReject && err != nil {
				t.Fatalf("expected bind mount source %q to be allowed after resolution, got: %v", tt.source, err)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsSensitivePathComponents(t *testing.T) {
	tests := []string{
		"/home/alice/.ssh",
		"/home/alice/.ssh/id_rsa",
		"/home/alice/.ssh/authorized_keys",
		"/home/alice/.SSH", // component match is case-insensitive
		"/home/bob/.aws",
		"/home/bob/.kube",
		"/home/bob/.docker",
		"/home/carol/.gnupg",
		"/root/.ssh",
		"/root/.aws",
		"/etc/shadow",
		"/etc/passwd",
		"/etc/sudoers",
		"/etc/sudoers.d",
		"/etc/cron.d",
		"/etc/pam.d",
		"/etc/NetworkManager",
	}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc)); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected", source)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsNamedVolumeMounts(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Volumes: []composetypes.ServiceVolumeConfig{
			{Type: composetypes.VolumeTypeVolume, Source: "etc", Target: "/mnt"},
		},
	}
	if err := checkDeployPolicy(projectWithService(svc)); err != nil {
		t.Fatalf("expected named volume mount to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsDeviceMappings(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Devices: []composetypes.DeviceMapping{{Source: "/dev/ttyUSB0", Target: "/dev/ttyUSB0"}},
	}
	if err := checkDeployPolicy(projectWithService(svc)); err == nil {
		t.Fatal("expected device mapping to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsUnsafeSecurityOpt(t *testing.T) {
	tests := []string{"seccomp:unconfined", "apparmor:unconfined", "apparmor=unconfined", "Seccomp:Unconfined"}
	for _, opt := range tests {
		t.Run(opt, func(t *testing.T) {
			svc := composetypes.ServiceConfig{SecurityOpt: []string{opt}}
			if err := checkDeployPolicy(projectWithService(svc)); err == nil {
				t.Fatalf("expected security_opt %q to be rejected", opt)
			}
		})
	}
}

func TestCheckDeployPolicy_CollectsMultipleViolationsAcrossServices(t *testing.T) {
	project := &composetypes.Project{
		Services: composetypes.Services{
			"privileged-app": composetypes.ServiceConfig{Privileged: true},
			"host-net-app":   composetypes.ServiceConfig{NetworkMode: "host"},
		},
	}
	err := checkDeployPolicy(project)
	if err == nil {
		t.Fatal("expected violations to be reported")
	}
	msg := err.Error()
	for _, want := range []string{"privileged-app", "host-net-app"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected error message to mention %q, got: %s", want, msg)
		}
	}
}
