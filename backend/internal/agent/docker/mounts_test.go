package docker

import (
	"context"
	"errors"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

type fakeContainerInspector struct {
	result       client.ContainerInspectResult
	err          error
	inspect      func(string) (client.ContainerInspectResult, error)
	listResult   client.ContainerListResult
	listErr      error
	calls        int
	listCalls    int
	gotID        string
	inspectedIDs []string
}

func (f *fakeContainerInspector) ContainerInspect(_ context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.calls++
	f.gotID = containerID
	f.inspectedIDs = append(f.inspectedIDs, containerID)
	if f.inspect != nil {
		return f.inspect(containerID)
	}
	return f.result, f.err
}

func (f *fakeContainerInspector) ContainerList(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
	f.listCalls++
	return f.listResult, f.listErr
}

func TestDetectHostDeploymentsDir(t *testing.T) {
	inspector := &fakeContainerInspector{result: client.ContainerInspectResult{
		Container: containerapi.InspectResponse{ID: "abc123def4567890", Mounts: []containerapi.MountPoint{
			{Type: mount.TypeBind, Source: "/srv/orcacd/deployments", Destination: "/deployments/"},
		}},
	}}

	got, err := detectHostDeploymentsDir(t.Context(), inspector, func() (string, error) {
		return "abc123def456", nil
	}, "/deployments")
	if err != nil {
		t.Fatalf("detectHostDeploymentsDir: %v", err)
	}
	if got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want %q", got, "/srv/orcacd/deployments")
	}
	if inspector.calls != 1 || inspector.gotID != "abc123def456" {
		t.Errorf("ContainerInspect calls = %d, id = %q", inspector.calls, inspector.gotID)
	}
	if inspector.listCalls != 0 {
		t.Errorf("ContainerList calls = %d, want 0", inspector.listCalls)
	}
}

func TestDetectHostDeploymentsDirFallsBackForCustomHostname(t *testing.T) {
	inspector := &fakeContainerInspector{
		listResult: client.ContainerListResult{Items: []containerapi.Summary{
			{
				ID: "real-container-id",
				Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "/srv/orcacd/deployments", Destination: "/deployments"},
				},
			},
		}},
		inspect: func(containerID string) (client.ContainerInspectResult, error) {
			switch containerID {
			case "custom-agent-hostname":
				return client.ContainerInspectResult{}, errors.New("container not found")
			case "real-container-id":
				return client.ContainerInspectResult{Container: containerapi.InspectResponse{
					Config: &containerapi.Config{Hostname: "custom-agent-hostname"},
					Mounts: []containerapi.MountPoint{
						{Type: mount.TypeBind, Source: "/srv/orcacd/deployments", Destination: "/deployments"},
					},
				}}, nil
			default:
				return client.ContainerInspectResult{}, errors.New("unexpected container")
			}
		},
	}

	got, err := detectHostDeploymentsDir(t.Context(), inspector, func() (string, error) {
		return "custom-agent-hostname", nil
	}, "/deployments")
	if err != nil {
		t.Fatalf("detectHostDeploymentsDir: %v", err)
	}
	if got != "/srv/orcacd/deployments" {
		t.Errorf("host deployments dir = %q, want %q", got, "/srv/orcacd/deployments")
	}
	if inspector.listCalls != 1 {
		t.Errorf("ContainerList calls = %d, want 1", inspector.listCalls)
	}
	if len(inspector.inspectedIDs) != 2 || inspector.inspectedIDs[0] != "custom-agent-hostname" || inspector.inspectedIDs[1] != "real-container-id" {
		t.Errorf("inspected container ids = %#v", inspector.inspectedIDs)
	}
}

func TestDetectHostDeploymentsDirRejectsDirectLookupNameCollision(t *testing.T) {
	inspector := &fakeContainerInspector{
		listResult: client.ContainerListResult{Items: []containerapi.Summary{
			{
				ID: "real-agent-container-id",
				Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "/srv/correct/deployments", Destination: "/deployments"},
				},
			},
		}},
		inspect: func(containerID string) (client.ContainerInspectResult, error) {
			switch containerID {
			case "custom-agent-hostname":
				return client.ContainerInspectResult{Container: containerapi.InspectResponse{
					ID:     "unrelated-container-id",
					Config: &containerapi.Config{Hostname: "unrelated-hostname"},
					Mounts: []containerapi.MountPoint{
						{Type: mount.TypeBind, Source: "/srv/wrong/deployments", Destination: "/deployments"},
					},
				}}, nil
			case "real-agent-container-id":
				return client.ContainerInspectResult{Container: containerapi.InspectResponse{
					ID:     "real-agent-container-id",
					Config: &containerapi.Config{Hostname: "custom-agent-hostname"},
					Mounts: []containerapi.MountPoint{
						{Type: mount.TypeBind, Source: "/srv/correct/deployments", Destination: "/deployments"},
					},
				}}, nil
			default:
				return client.ContainerInspectResult{}, errors.New("unexpected container")
			}
		},
	}

	got, err := detectHostDeploymentsDir(t.Context(), inspector, func() (string, error) {
		return "custom-agent-hostname", nil
	}, "/deployments")
	if err != nil {
		t.Fatalf("detectHostDeploymentsDir: %v", err)
	}
	if got != "/srv/correct/deployments" {
		t.Errorf("host deployments dir = %q, want %q", got, "/srv/correct/deployments")
	}
}

func TestDetectHostDeploymentsDirErrors(t *testing.T) {
	tests := []struct {
		name      string
		hostname  func() (string, error)
		inspector *fakeContainerInspector
	}{
		{
			name:      "hostname",
			hostname:  func() (string, error) { return "", errors.New("hostname failed") },
			inspector: &fakeContainerInspector{},
		},
		{
			name:      "inspect",
			hostname:  func() (string, error) { return "container-id", nil },
			inspector: &fakeContainerInspector{err: errors.New("inspect failed")},
		},
		{
			name:     "mount missing",
			hostname: func() (string, error) { return "container-id", nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "/srv/other", Destination: "/other"},
				}},
			}},
		},
		{
			name:     "not a bind mount",
			hostname: func() (string, error) { return "container-id", nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{Mounts: []containerapi.MountPoint{
					{Type: mount.TypeVolume, Source: "/var/lib/docker/volumes/data/_data", Destination: "/deployments"},
				}},
			}},
		},
		{
			name:     "relative source",
			hostname: func() (string, error) { return "container-id", nil },
			inspector: &fakeContainerInspector{result: client.ContainerInspectResult{
				Container: containerapi.InspectResponse{Mounts: []containerapi.MountPoint{
					{Type: mount.TypeBind, Source: "relative/deployments", Destination: "/deployments"},
				}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := detectHostDeploymentsDir(t.Context(), tt.inspector, tt.hostname, "/deployments"); err == nil {
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
