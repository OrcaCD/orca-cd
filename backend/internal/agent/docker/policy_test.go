package docker

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
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
	if err := checkDeployPolicy(project, pathRules{}); err != nil {
		t.Fatalf("expected safe service to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsPrivileged(t *testing.T) {
	project := projectWithService(composetypes.ServiceConfig{Privileged: true})
	if err := checkDeployPolicy(project, pathRules{}); err == nil {
		t.Fatal("expected privileged service to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsUseAPISocket(t *testing.T) {
	project := projectWithService(composetypes.ServiceConfig{UseAPISocket: true})
	if err := checkDeployPolicy(project, pathRules{}); err == nil {
		t.Fatal("expected use_api_socket to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsDangerousCapability(t *testing.T) {
	tests := []string{"SYS_ADMIN", "cap_sys_admin", "NET_ADMIN", "ALL", "sys_ptrace", "CAP_SYS_MODULE"}
	for _, capability := range tests {
		t.Run(capability, func(t *testing.T) {
			project := projectWithService(composetypes.ServiceConfig{CapAdd: []string{capability}})
			if err := checkDeployPolicy(project, pathRules{}); err == nil {
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
	if err := checkDeployPolicy(project, pathRules{}); err != nil {
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
			if err := checkDeployPolicy(projectWithService(tt.svc), pathRules{}); err == nil {
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
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
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
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err != nil {
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
	}, func(o *loader.Options) { o.SkipResolveEnvironment = true })
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
			err := checkDeployPolicy(project, pathRules{})
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
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
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
	if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err != nil {
		t.Fatalf("expected named volume mount to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsDeviceMappings(t *testing.T) {
	svc := composetypes.ServiceConfig{
		Devices: []composetypes.DeviceMapping{{Source: "/dev/ttyUSB0", Target: "/dev/ttyUSB0"}},
	}
	if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
		t.Fatal("expected device mapping to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsUnsafeSecurityOpt(t *testing.T) {
	tests := []string{
		"seccomp:unconfined", "apparmor:unconfined", "apparmor=unconfined", "Seccomp:Unconfined",
		"label=disable", "label:disable", "systempaths=unconfined", "seccomp=/tmp/profile.json",
		"apparmor=custom", "label=type:spc_t",
	}
	for _, opt := range tests {
		t.Run(opt, func(t *testing.T) {
			svc := composetypes.ServiceConfig{SecurityOpt: []string{opt}}
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
				t.Fatalf("expected security_opt %q to be rejected", opt)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsNoNewPrivileges(t *testing.T) {
	for _, opt := range []string{"no-new-privileges", "no-new-privileges:true", "no-new-privileges=true"} {
		t.Run(opt, func(t *testing.T) {
			svc := composetypes.ServiceConfig{SecurityOpt: []string{opt}}
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err != nil {
				t.Fatalf("expected security_opt %q to be allowed, got: %v", opt, err)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsVolumesFrom(t *testing.T) {
	svc := composetypes.ServiceConfig{VolumesFrom: []string{"container:orca-agent"}}
	if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
		t.Fatal("expected volumes_from to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsDeviceCgroupRules(t *testing.T) {
	svc := composetypes.ServiceConfig{DeviceCgroupRules: []string{"c 1:3 mr"}}
	if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
		t.Fatal("expected device_cgroup_rules to be rejected")
	}
}

func envFileProject(path string) *composetypes.Project {
	return projectWithService(composetypes.ServiceConfig{
		EnvFiles: []composetypes.EnvFile{{Path: path, Required: true}},
	})
}

func TestCheckEnvFiles_DenyListRejectsSensitivePaths(t *testing.T) {
	tests := []string{"/proc/self/environ", "/etc/shadow", "/root/.env", "/home/alice/.ssh/id_rsa", "/var/lib/docker/x.env"}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			if err := checkDeployPolicy(envFileProject(path), pathRules{}); err == nil {
				t.Fatalf("expected env_file %q to be rejected", path)
			}
		})
	}
}

func TestCheckEnvFiles_DenyListAllowsOtherPaths(t *testing.T) {
	for _, path := range []string{"/deployments/app/.env", "/srv/app/.env.prod", "/data/app.env"} {
		t.Run(path, func(t *testing.T) {
			if err := checkDeployPolicy(envFileProject(path), pathRules{}); err != nil {
				t.Fatalf("expected env_file %q to be allowed, got: %v", path, err)
			}
		})
	}
}

func TestCheckEnvFiles_RestrictDirOnlyAllowsInside(t *testing.T) {
	for _, path := range []string{"/deployments/app/.env", "/deployments/.env"} {
		if err := checkDeployPolicy(envFileProject(path), pathRules{restrictMountsDir: "/deployments"}); err != nil {
			t.Fatalf("expected env_file %q to be allowed, got: %v", path, err)
		}
	}
	for _, path := range []string{"/data/app.env", "/proc/self/environ", "/deploymentsbutnotquite/.env"} {
		if err := checkDeployPolicy(envFileProject(path), pathRules{restrictMountsDir: "/deployments"}); err == nil {
			t.Fatalf("expected env_file %q to be rejected", path)
		}
	}
}

func TestCheckEnvFiles_ResolvesLoadedComposePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path resolution is OS-native; only meaningful on a Unix-like runner matching production")
	}
	tests := map[string]struct {
		envFile    string
		wantReject bool
	}{
		"string":          {"env_file: .env.app", false},
		"list":            {"env_file:\n      - .env.app", false},
		"optional":        {"env_file:\n      - path: missing.env\n        required: false", false},
		"traversal":       {"env_file: ../../other/.env", true},
		"absolute":        {"env_file: /proc/self/environ", true},
		"absolute option": {"env_file:\n      - path: /etc/shadow\n        required: false", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			yaml := "name: demo\nservices:\n  app:\n    image: img:1\n    " + tt.envFile + "\n"
			project := loadComposeProject(t, "/deployments/demo", yaml)
			err := checkDeployPolicy(project, pathRules{restrictMountsDir: "/deployments"})
			if tt.wantReject && err == nil {
				t.Fatal("expected env_file to be rejected")
			}
			if !tt.wantReject && err != nil {
				t.Fatalf("expected env_file to be allowed, got: %v", err)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsVolumeDriverOpts(t *testing.T) {
	project := &composetypes.Project{
		Services: composetypes.Services{"app": composetypes.ServiceConfig{}},
		Volumes: composetypes.Volumes{
			"host": composetypes.VolumeConfig{
				Driver:     "local",
				DriverOpts: map[string]string{"type": "none", "o": "bind", "device": "/"},
			},
		},
	}
	if err := checkDeployPolicy(project, pathRules{}); err == nil {
		t.Fatal("expected volume driver_opts to be rejected")
	}
}

func TestCheckDeployPolicy_AllowsVolumeWithoutDriverOpts(t *testing.T) {
	project := &composetypes.Project{
		Services: composetypes.Services{"app": composetypes.ServiceConfig{}},
		Volumes:  composetypes.Volumes{"data": composetypes.VolumeConfig{}},
	}
	if err := checkDeployPolicy(project, pathRules{}); err != nil {
		t.Fatalf("expected plain named volume to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsDockerDataDirs(t *testing.T) {
	tests := []string{"/var/lib/docker", "/var/lib/docker/volumes", "/var/lib/containerd"}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected", source)
			}
		})
	}
}

func TestCheckDeployPolicy_RestrictMountsDirRejectsOutsideMounts(t *testing.T) {
	tests := []string{"/data", "/home/user/app-data", "/deploymentsbutnotquite", "/"}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc), pathRules{restrictMountsDir: "/deployments"}); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected outside restricted mounts dir", source)
			}
		})
	}
}

func TestCheckDeployPolicy_RestrictMountsDirAllowsMountsInside(t *testing.T) {
	tests := []string{"/deployments", "/deployments/app", "/deployments/app/data"}
	for _, source := range tests {
		t.Run(source, func(t *testing.T) {
			svc := composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
				},
			}
			if err := checkDeployPolicy(projectWithService(svc), pathRules{restrictMountsDir: "/deployments"}); err != nil {
				t.Fatalf("expected bind mount of %q to be allowed inside restricted mounts dir, got: %v", source, err)
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
	err := checkDeployPolicy(project, pathRules{})
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

func TestCheckDeployPolicy_ChecksSecretAndConfigFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path resolution is OS-native; only meaningful on a Unix-like runner matching production")
	}
	tests := map[string]struct {
		file              string
		restrictMountsDir string
		wantReject        bool
	}{
		"docker socket":             {"/var/run/docker.sock", "", true},
		"sensitive host file":       {"/etc/shadow", "", true},
		"ssh key under home":        {"/home/alice/.ssh/id_ed25519", "", true},
		"other host file":           {"/srv/certs/tls.crt", "", false},
		"relative file":             {"./secret.txt", "", false},
		"restrict: inside app dir":  {"./secret.txt", "/deployments", false},
		"restrict: outside":         {"/srv/certs/tls.crt", "/deployments", true},
		"restrict: traversal":       {"../../etc/shadow", "/deployments", true},
		"restrict: docker socket":   {"/var/run/docker.sock", "/deployments", true},
		"restrict: similar prefix":  {"/deploymentsbutnotquite/secret", "/deployments", true},
		"restrict: deployments dir": {"/deployments/demo/certs/tls.crt", "/deployments", false},
	}
	for name, tt := range tests {
		for _, kind := range []string{"secrets", "configs"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				yaml := fmt.Sprintf(`
name: demo
services:
  app:
    image: ghcr.io/orcacd/app:1.0.0
    %[1]s: [data]
%[1]s:
  data:
    file: %[2]s
`, kind, tt.file)
				project := loadComposeProject(t, "/deployments/demo", yaml)
				err := checkDeployPolicy(project, pathRules{restrictMountsDir: tt.restrictMountsDir})
				if tt.wantReject && err == nil {
					t.Fatalf("expected %s file %q to be rejected", kind, tt.file)
				}
				if !tt.wantReject && err != nil {
					t.Fatalf("expected %s file %q to be allowed, got: %v", kind, tt.file, err)
				}
			})
		}
	}
}

func TestCheckDeployPolicy_AllowsSecretsWithoutFileSource(t *testing.T) {
	project := &composetypes.Project{
		Secrets: composetypes.Secrets{"token": {Environment: "TOKEN"}},
		Configs: composetypes.Configs{"settings": {Content: "debug: false"}},
	}
	if err := checkDeployPolicy(project, pathRules{restrictMountsDir: "/deployments"}); err != nil {
		t.Fatalf("expected secrets and configs without a file source to be allowed, got: %v", err)
	}
}

func bindMountProject(source string) *composetypes.Project {
	return projectWithService(composetypes.ServiceConfig{
		Volumes: []composetypes.ServiceVolumeConfig{
			{Type: composetypes.VolumeTypeBind, Source: source, Target: "/mnt"},
		},
	})
}

func TestCheckDeployPolicy_CleansPathsBeforeDenyListCheck(t *testing.T) {
	for _, source := range []string{"/./etc", "//etc", "/srv/../etc", "/var/run/./docker.sock", "/data/../root/.bashrc"} {
		t.Run(source, func(t *testing.T) {
			if err := checkDeployPolicy(bindMountProject(source), pathRules{}); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected", source)
			}
			if err := checkDeployPolicy(envFileProject(source), pathRules{}); err == nil {
				t.Fatalf("expected env_file %q to be rejected", source)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsParentsOfSensitivePaths(t *testing.T) {
	for _, source := range []string{"/var", "/var/lib", "/home", "/home/alice", "/home/alice/"} {
		t.Run(source, func(t *testing.T) {
			if err := checkDeployPolicy(bindMountProject(source), pathRules{}); err == nil {
				t.Fatalf("expected bind mount of %q to be rejected", source)
			}
		})
	}
	for _, source := range []string{"/srv", "/srv/app", "/var/lib/app-data", "/home/alice/app-data", "/opt"} {
		t.Run(source, func(t *testing.T) {
			if err := checkDeployPolicy(bindMountProject(source), pathRules{}); err != nil {
				t.Fatalf("expected bind mount of %q to be allowed, got: %v", source, err)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsRelativePaths(t *testing.T) {
	if err := checkDeployPolicy(bindMountProject("data"), pathRules{}); err == nil {
		t.Fatal("expected an unresolved relative bind mount source to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsProviderServices(t *testing.T) {
	yaml := `
name: demo
services:
  app:
    provider:
      type: example
      options:
        key: value
`
	project := loadComposeProject(t, "/deployments/demo", yaml)
	if project.Services["app"].Provider == nil {
		t.Fatal("expected the loader to parse the provider")
	}
	if err := checkDeployPolicy(project, pathRules{}); err == nil {
		t.Fatal("expected a provider service to be rejected")
	}
}

func TestCheckDeployPolicy_RejectsContainerNamespaceModes(t *testing.T) {
	tests := []struct {
		name string
		svc  composetypes.ServiceConfig
	}{
		{"network_mode", composetypes.ServiceConfig{NetworkMode: "container:orca-agent"}},
		{"pid", composetypes.ServiceConfig{Pid: "container:orca-agent"}},
		{"ipc", composetypes.ServiceConfig{Ipc: "container:orca-agent"}},
		{"uts", composetypes.ServiceConfig{Uts: "container:orca-agent"}},
		{"pid uppercase", composetypes.ServiceConfig{Pid: " Container:orca-agent"}},
		{"network_mode host uppercase", composetypes.ServiceConfig{NetworkMode: "HOST"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkDeployPolicy(projectWithService(tt.svc), pathRules{}); err == nil {
				t.Fatalf("expected %+v to be rejected", tt.svc)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsProjectNamespaceModes(t *testing.T) {
	for _, svc := range []composetypes.ServiceConfig{
		{NetworkMode: "bridge"},
		{NetworkMode: "none"},
		{NetworkMode: "service:db"},
		{Ipc: "shareable"},
		{Ipc: "private"},
		{Ipc: "service:db"},
	} {
		if err := checkDeployPolicy(projectWithService(svc), pathRules{}); err != nil {
			t.Fatalf("expected %+v to be allowed, got: %v", svc, err)
		}
	}
}

func TestCheckDeployPolicy_RejectsUnsafePreStartHooks(t *testing.T) {
	tests := []struct {
		name string
		hook string
	}{
		{"privileged", "privileged: true"},
		{"pid host", "pid: host"},
		{"network_mode host", "network_mode: host"},
		{"cap_add", "cap_add: [SYS_ADMIN]"},
		{"use_api_socket", "use_api_socket: true"},
		{"devices", "devices: [/dev/sda:/dev/sda]"},
		{"security_opt", "security_opt: [seccomp=unconfined]"},
		{"bind mount", "volumes: [/:/host]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
name: demo
services:
  app:
    image: alpine
    pre_start:
      - command: ["true"]
        %s
`, tt.hook)
			project := loadComposeProject(t, "/deployments/demo", yaml)
			if len(project.Services["app"].PreStart) != 1 {
				t.Fatal("expected the loader to parse the pre_start hook")
			}
			if err := checkDeployPolicy(project, pathRules{}); err == nil {
				t.Fatalf("expected a pre_start hook with %q to be rejected", tt.hook)
			}
		})
	}
}

func TestCheckDeployPolicy_RejectsPrivilegedExecHooks(t *testing.T) {
	for _, hookType := range []string{"post_start", "pre_stop"} {
		t.Run(hookType, func(t *testing.T) {
			yaml := fmt.Sprintf(`
name: demo
services:
  app:
    image: alpine
    %s:
      - command: ["true"]
        privileged: true
`, hookType)
			project := loadComposeProject(t, "/deployments/demo", yaml)
			if err := checkDeployPolicy(project, pathRules{}); err == nil {
				t.Fatalf("expected a privileged %s hook to be rejected", hookType)
			}
		})
	}
}

func TestCheckDeployPolicy_AllowsSafeHooks(t *testing.T) {
	yaml := `
name: demo
services:
  app:
    image: alpine
    pre_start:
      - image: busybox
        command: ["sh", "-c", "echo migrate"]
        volumes: [app-data:/data]
    post_start:
      - command: ["echo", "started"]
        user: root
    pre_stop:
      - command: ["echo", "stopping"]
volumes:
  app-data:
`
	project := loadComposeProject(t, "/deployments/demo", yaml)
	if err := checkDeployPolicy(project, pathRules{}); err != nil {
		t.Fatalf("expected unprivileged hooks to be allowed, got: %v", err)
	}
}

func TestCheckDeployPolicy_RejectsJobs(t *testing.T) {
	yaml := `
name: demo
services:
  app:
    image: alpine
jobs:
  migrate:
    image: alpine
    privileged: true
    triggers:
      schedule: ["0 * * * *"]
`
	project := loadComposeProject(t, "/deployments/demo", yaml)
	if len(project.AllJobs()) != 1 {
		t.Fatal("expected the loader to parse the job")
	}
	if err := checkDeployPolicy(project, pathRules{}); err == nil {
		t.Fatal("expected a job to be rejected")
	}
}

// The policy only checks the fields it knows about, so a compose-go update that
// adds a field can silently add a way around it (pre_start hooks arrived this
// way). When this test fails, decide whether the new field needs a check in
// policy.go before adding it to the reviewed list.
func TestCheckDeployPolicy_ComposeFieldsAreReviewed(t *testing.T) {
	reviewed := map[reflect.Type][]string{
		reflect.TypeFor[composetypes.Project](): {
			"Name", "WorkingDir", "Services", "Jobs", "Networks", "Volumes", "Secrets", "Configs", "Models",
			"Extensions", "ComposeFiles", "Environment", "DisabledServices", "DisabledJobs", "Profiles",
		},
		reflect.TypeFor[composetypes.ServiceConfig](): {
			"Name", "Profiles", "Deploy", "Develop", "Restart", "Scale", "Attach", "ContainerName", "Provider",
			"Extends", "ExternalLinks", "Links", "Net", "PreStart", "PostStart", "PreStop", "ContainerSpec",
			"WorkloadSpec", "Extensions",
		},
		reflect.TypeFor[composetypes.ContainerSpec](): {
			"Annotations", "BlkioConfig", "CapAdd", "CapDrop", "CgroupParent", "Cgroup", "CPUCount", "CPUPercent",
			"CPUPeriod", "CPUQuota", "CPURTPeriod", "CPURTRuntime", "CPUS", "CPUSet", "CPUShares", "Command",
			"Configs", "CredentialSpec", "DeviceCgroupRules", "Devices", "DNS", "DNSOpts", "DNSSearch",
			"DomainName", "Entrypoint", "Environment", "EnvFiles", "ExtraHosts", "GroupAdd", "Gpus", "Hostname",
			"Image", "Init", "Ipc", "Isolation", "Labels", "LabelFiles", "CustomLabels", "Logging", "LogDriver",
			"LogOpt", "MemLimit", "MemReservation", "MemSwapLimit", "MemSwappiness", "MacAddress", "Models",
			"NetworkMode", "Networks", "OomKillDisable", "OomScoreAdj", "Pid", "PidsLimit", "Platform",
			"Privileged", "PullPolicy", "PullRefreshAfter", "ReadOnly", "Runtime", "Secrets", "SecurityOpt",
			"ShmSize", "StopGracePeriod", "StopSignal", "StorageOpt", "Sysctls", "Tmpfs", "Ulimits", "Umask",
			"UseAPISocket", "User", "UserNSMode", "Uts", "VolumeDriver", "Volumes", "VolumesFrom", "WorkingDir",
		},
		reflect.TypeFor[composetypes.WorkloadSpec](): {
			"Build", "DependsOn", "Dockerfile", "Expose", "HealthCheck", "Ports", "StdinOpen", "Tty",
		},
		reflect.TypeFor[composetypes.PreStartHook](): {"ContainerSpec", "PerReplica", "Extensions"},
		reflect.TypeFor[composetypes.ServiceHook]():  {"Command", "User", "Privileged", "WorkingDir", "Environment", "Extensions"},
	}
	for typ, fields := range reviewed {
		var actual []string
		for field := range typ.Fields() {
			actual = append(actual, field.Name)
		}
		slices.Sort(actual)
		slices.Sort(fields)
		if !slices.Equal(actual, fields) {
			t.Errorf("compose-go %s fields changed, review them for the deploy policy:\n got: %v\nwant: %v", typ.Name(), actual, fields)
		}
	}
}
