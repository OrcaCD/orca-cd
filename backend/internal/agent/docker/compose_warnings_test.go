package docker

import (
	"reflect"
	"testing"

	"github.com/OrcaCD/orca-cd/internal/shared/composewarnings"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestAnalyzeComposeWarnings(t *testing.T) {
	project := &composetypes.Project{
		Services: composetypes.Services{
			"app": {Image: "ghcr.io/example/app"},
			"db": {
				Image:       "postgres:16",
				Privileged:  true,
				NetworkMode: " host ",
				Volumes: []composetypes.ServiceVolumeConfig{
					{Type: composetypes.VolumeTypeBind, Target: "/docker-entrypoint-initdb.d/init.sql"},
				},
			},
			"memcached": {Image: "memcached:1.6", Restart: "unless-stopped"},
			"redis": {
				Image:   "redis:7",
				Volumes: []composetypes.ServiceVolumeConfig{{Type: composetypes.VolumeTypeVolume, Target: "/data"}},
			},
			"web": {Image: "nginx:latest", Restart: "unless-stopped"},
		},
	}

	got := analyzeComposeWarnings(project)
	want := []composewarnings.Warning{
		{Code: "missing_restart_policy", Service: "app"},
		{Code: "floating_image_reference", Service: "app"},
		{Code: "missing_restart_policy", Service: "db"},
		{Code: "database_without_volume", Service: "db"},
		{Code: "privileged_container", Service: "db"},
		{Code: "host_network", Service: "db"},
		{Code: "missing_restart_policy", Service: "redis"},
		{Code: "floating_image_reference", Service: "web"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("analyzeComposeWarnings() = %#v, want %#v", got, want)
	}
}

func TestHasPersistentDataMount(t *testing.T) {
	tests := []struct {
		name    string
		volumes []composetypes.ServiceVolumeConfig
		want    bool
	}{
		{
			name: "unrelated bind mount",
			volumes: []composetypes.ServiceVolumeConfig{{
				Type: composetypes.VolumeTypeBind, Target: "/docker-entrypoint-initdb.d/init.sql",
			}},
		},
		{
			name: "tmpfs data mount",
			volumes: []composetypes.ServiceVolumeConfig{{
				Type: composetypes.VolumeTypeTmpfs, Target: "/var/lib/postgresql/data",
			}},
		},
		{
			name: "parent named volume",
			volumes: []composetypes.ServiceVolumeConfig{{
				Type: composetypes.VolumeTypeVolume, Target: "/var/lib/postgresql",
			}},
			want: true,
		},
		{
			name: "data bind mount",
			volumes: []composetypes.ServiceVolumeConfig{{
				Type: composetypes.VolumeTypeBind, Target: "/var/lib/postgresql/data",
			}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasPersistentDataMount("postgres:16", tt.volumes)
			if got != tt.want {
				t.Fatalf("hasPersistentDataMount() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestDatabaseImageName(t *testing.T) {
	tests := []struct {
		image string
		want  string
	}{
		{image: "postgres:16", want: "postgres"},
		{image: "registry.example:5000/team/postgres@sha256:0123456789abcdef", want: "postgres"},
		{image: "redis@sha256:0123456789abcdef", want: "redis"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := databaseImageName(tt.image); got != tt.want {
				t.Errorf("databaseImageName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestHasFloatingImageReference(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{image: "nginx", want: true},
		{image: "nginx:latest", want: true},
		{image: "nginx:", want: true},
		{image: "nginx:1.27", want: false},
		{image: "registry.example:5000/team/app", want: true},
		{image: "registry.example:5000/team/app:1.2", want: false},
		{image: "nginx@sha256:0123456789abcdef", want: false},
		{image: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			if got := hasFloatingImageReference(tt.image); got != tt.want {
				t.Errorf("hasFloatingImageReference(%q) = %t, want %t", tt.image, got, tt.want)
			}
		})
	}
}

func TestAnalyzeComposeWarningsRecognizesHostNamespaces(t *testing.T) {
	project := &composetypes.Project{
		Services: composetypes.Services{
			"app": {
				Image:      "nginx:1.27",
				Pid:        " HOST ",
				Ipc:        "container:database",
				Uts:        "host",
				UserNSMode: "container:other",
				Cgroup:     "host",
			},
		},
	}

	want := []composewarnings.Warning{
		{Code: "missing_restart_policy", Service: "app"},
		{Code: "host_namespace", Service: "app", Field: "pid"},
		{Code: "host_namespace", Service: "app", Field: "ipc"},
		{Code: "host_namespace", Service: "app", Field: "uts"},
		{Code: "host_namespace", Service: "app", Field: "userns_mode"},
		{Code: "host_namespace", Service: "app", Field: "cgroup"},
	}
	if got := analyzeComposeWarnings(project); !reflect.DeepEqual(got, want) {
		t.Fatalf("analyzeComposeWarnings() = %#v, want %#v", got, want)
	}
}
