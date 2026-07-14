package docker

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

const testContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestDetectContainerID(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
		want  string
	}{
		{
			name: "cgroup v2 systemd scope",
			files: map[string][]byte{
				"/proc/self/cgroup": []byte("0::/system.slice/docker-" + testContainerID + ".scope\n"),
			},
			want: testContainerID,
		},
		{
			name: "cgroup v1 docker path",
			files: map[string][]byte{
				"/proc/self/cgroup": []byte("12:memory:/docker/" + testContainerID + "\n"),
			},
			want: testContainerID,
		},
		{
			name: "mountinfo fallback",
			files: map[string][]byte{
				"/proc/self/cgroup": []byte("0::/\n"),
				"/proc/self/mountinfo": []byte(
					"100 99 8:1 /docker/containers/" + testContainerID + "/hostname /etc/hostname rw,relatime - ext4 /dev/sda rw\n",
				),
			},
			want: testContainerID,
		},
		{
			name: "host process",
			files: map[string][]byte{
				"/proc/self/cgroup":    []byte("0::/user.slice/user-1000.slice/session-2.scope\n"),
				"/proc/self/mountinfo": []byte("29 1 8:1 / / rw,relatime - ext4 /dev/sda rw\n"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectContainerID(func(path string) ([]byte, error) {
				if data, ok := tt.files[path]; ok {
					return data, nil
				}
				return nil, os.ErrNotExist
			})
			if tt.want == "" {
				if !errors.Is(err, errNotContainerized) {
					t.Fatalf("detectContainerID() error = %v, want errNotContainerized", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("detectContainerID: %v", err)
			}
			if got != tt.want {
				t.Fatalf("detectContainerID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectContainerIDWithoutProcIsNotContainerized(t *testing.T) {
	_, err := detectContainerID(func(string) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	if !errors.Is(err, errNotContainerized) {
		t.Fatalf("detectContainerID() error = %v, want errNotContainerized", err)
	}
}

func TestDetectContainerIDReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		readErr map[string]error
		want    string
	}{
		{
			name: "cgroup",
			readErr: map[string]error{
				"/proc/self/cgroup": errors.New("permission denied"),
			},
			want: "/proc/self/cgroup",
		},
		{
			name: "mountinfo",
			readErr: map[string]error{
				"/proc/self/mountinfo": errors.New("permission denied"),
			},
			want: "/proc/self/mountinfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := detectContainerID(func(path string) ([]byte, error) {
				if err, ok := tt.readErr[path]; ok {
					return nil, err
				}
				return []byte("0::/\n"), nil
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("detectContainerID() error = %v, want path %q", err, tt.want)
			}
		})
	}
}

func TestContainerIDParsersRejectNonDockerData(t *testing.T) {
	if got := containerIDFromCgroup([]byte("0::/docker/0123456789ab\n")); got != "" {
		t.Fatalf("containerIDFromCgroup() = %q, want empty", got)
	}
	mountInfo := []byte("100 99 8:1 /home/user/hostname /etc/hostname rw - ext4 /dev/sda rw\n")
	if got := containerIDFromMountInfo(mountInfo); got != "" {
		t.Fatalf("containerIDFromMountInfo() = %q, want empty", got)
	}
	if got := containerIDFromMountInfo([]byte("malformed\n")); got != "" {
		t.Fatalf("containerIDFromMountInfo() malformed = %q, want empty", got)
	}
}

type fakeContainerInspector struct {
	result client.ContainerInspectResult
	err    error
	calls  int
	gotID  string
}

func (f *fakeContainerInspector) ContainerInspect(_ context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.calls++
	f.gotID = containerID
	return f.result, f.err
}

func TestDetectHostDeploymentsDirUsesExactContainerID(t *testing.T) {
	inspector := &fakeContainerInspector{result: client.ContainerInspectResult{
		Container: containerapi.InspectResponse{
			ID:     testContainerID,
			Config: &containerapi.Config{Hostname: "custom-agent-hostname"},
			Mounts: []containerapi.MountPoint{
				{Type: mount.TypeBind, Source: "/srv/orcacd/deployments", Destination: "/deployments/"},
			},
		},
	}}

	got, err := detectHostDeploymentsDir(t.Context(), inspector, func() (string, error) {
		return testContainerID, nil
	}, "/deployments")
	if err != nil {
		t.Fatalf("detectHostDeploymentsDir: %v", err)
	}
	if got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want %q", got, "/srv/orcacd/deployments")
	}
	if inspector.calls != 1 || inspector.gotID != testContainerID {
		t.Errorf("ContainerInspect calls = %d, id = %q", inspector.calls, inspector.gotID)
	}
}

func TestDetectHostDeploymentsDirErrors(t *testing.T) {
	tests := []struct {
		name        string
		containerID func() (string, error)
		inspector   *fakeContainerInspector
	}{
		{
			name:        "identity",
			containerID: func() (string, error) { return "", errors.New("identity failed") },
			inspector:   &fakeContainerInspector{},
		},
		{
			name:        "inspect",
			containerID: func() (string, error) { return testContainerID, nil },
			inspector:   &fakeContainerInspector{err: errors.New("inspect failed")},
		},
		{
			name:        "empty container ID",
			containerID: func() (string, error) { return "", nil },
			inspector:   &fakeContainerInspector{},
		},
		{
			name:        "container ID mismatch",
			containerID: func() (string, error) { return testContainerID, nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{ID: "different-container-id"},
			}},
		},
		{
			name:        "mount missing",
			containerID: func() (string, error) { return testContainerID, nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{ID: testContainerID, Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "/srv/other", Destination: "/other"},
				}},
			}},
		},
		{
			name:        "not a bind mount",
			containerID: func() (string, error) { return testContainerID, nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{ID: testContainerID, Mounts: []containerapi.MountPoint{
					{Type: mount.TypeVolume, Source: "/var/lib/docker/volumes/data/_data", Destination: "/deployments"},
				}},
			}},
		},
		{
			name:        "relative source",
			containerID: func() (string, error) { return testContainerID, nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{ID: testContainerID, Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "relative/deployments", Destination: "/deployments"},
				}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := detectHostDeploymentsDir(t.Context(), tt.inspector, tt.containerID, "/deployments"); err == nil {
				t.Fatal("expected detection error")
			}
		})
	}
}

func TestTranslateBindMountSources(t *testing.T) {
	project := &composetypes.Project{Services: composetypes.Services{
		"app": composetypes.ServiceConfig{Volumes: []composetypes.ServiceVolumeConfig{
			{Type: composetypes.VolumeTypeBind, Source: "/deployments/app/data", Target: "/data", ReadOnly: true},
			{Type: composetypes.VolumeTypeBind, Source: "/srv/shared", Target: "/shared"},
			{Type: composetypes.VolumeTypeBind, Source: "/deployments-old/data", Target: "/old"},
			{Type: composetypes.VolumeTypeVolume, Source: "cache", Target: "/cache"},
		}},
	}}

	if err := translateBindMountSources(project, "/deployments", "/srv/orcacd/deployments"); err != nil {
		t.Fatalf("translateBindMountSources: %v", err)
	}

	volumes := project.Services["app"].Volumes
	if volumes[0].Source != "/srv/orcacd/deployments/app/data" || volumes[0].Target != "/data" || !volumes[0].ReadOnly {
		t.Errorf("deployment bind mount = %#v", volumes[0])
	}
	if volumes[1].Source != "/srv/shared" {
		t.Errorf("external bind source = %q, want unchanged", volumes[1].Source)
	}
	if volumes[2].Source != "/deployments-old/data" {
		t.Errorf("similar-prefix bind source = %q, want unchanged", volumes[2].Source)
	}
	if volumes[3].Source != "cache" || volumes[3].Type != composetypes.VolumeTypeVolume {
		t.Errorf("named volume = %#v, want unchanged", volumes[3])
	}
}

func TestTranslateBindMountSourcesTranslatesRoot(t *testing.T) {
	project := projectWithBindMount("/deployments")

	if err := translateBindMountSources(project, "/deployments", "/srv/orcacd/deployments"); err != nil {
		t.Fatalf("translateBindMountSources: %v", err)
	}
	if got := project.Services["app"].Volumes[0].Source; got != "/srv/orcacd/deployments" {
		t.Errorf("bind source = %q, want %q", got, "/srv/orcacd/deployments")
	}
}

func TestTranslateBindMountSourcesWithoutHostBaseIsNoOp(t *testing.T) {
	project := projectWithBindMount("/deployments/app/data")

	if err := translateBindMountSources(project, "/deployments", ""); err != nil {
		t.Fatalf("translateBindMountSources: %v", err)
	}
	if got := project.Services["app"].Volumes[0].Source; got != "/deployments/app/data" {
		t.Errorf("bind source = %q, want unchanged", got)
	}
}

func projectWithBindMount(source string) *composetypes.Project {
	return &composetypes.Project{Services: composetypes.Services{
		"app": composetypes.ServiceConfig{Volumes: []composetypes.ServiceVolumeConfig{
			{Type: composetypes.VolumeTypeBind, Source: source, Target: "/data"},
		}},
	}}
}
