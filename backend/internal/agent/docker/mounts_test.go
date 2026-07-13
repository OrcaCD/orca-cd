package docker

import (
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestTranslateBindMountSources_TranslatesDeploymentBindMount(t *testing.T) {
	project := &composetypes.Project{Services: composetypes.Services{
		"app": composetypes.ServiceConfig{Volumes: []composetypes.ServiceVolumeConfig{
			{
				Type:     composetypes.VolumeTypeBind,
				Source:   "/deployments/app/data",
				Target:   "/data",
				ReadOnly: true,
				Bind:     &composetypes.ServiceVolumeBind{CreateHostPath: true},
			},
			{Type: composetypes.VolumeTypeVolume, Source: "cache", Target: "/cache"},
		}},
	}}

	if err := translateBindMountSources(project, "/deployments", "/srv/orca/deployments"); err != nil {
		t.Fatalf("translateBindMountSources: %v", err)
	}

	volumes := project.Services["app"].Volumes
	if volumes[0].Source != "/srv/orca/deployments/app/data" {
		t.Errorf("bind source = %q, want %q", volumes[0].Source, "/srv/orca/deployments/app/data")
	}
	if volumes[0].Target != "/data" || !volumes[0].ReadOnly || !bool(volumes[0].Bind.CreateHostPath) {
		t.Errorf("bind mount options changed: %#v", volumes[0])
	}
	if volumes[1].Source != "cache" || volumes[1].Type != composetypes.VolumeTypeVolume {
		t.Errorf("named volume changed: %#v", volumes[1])
	}
}

func TestTranslateBindMountSources_TranslatesDeploymentRoot(t *testing.T) {
	project := projectWithBindMount("/deployments")

	if err := translateBindMountSources(project, "/deployments", "/srv/orca/deployments"); err != nil {
		t.Fatalf("translateBindMountSources: %v", err)
	}

	if got := project.Services["app"].Volumes[0].Source; got != "/srv/orca/deployments" {
		t.Errorf("bind source = %q, want %q", got, "/srv/orca/deployments")
	}
}

func TestTranslateBindMountSources_LeavesExternalSourcesUnchanged(t *testing.T) {
	for _, source := range []string{"/srv/shared", "/deployments-old/app/data"} {
		t.Run(source, func(t *testing.T) {
			project := projectWithBindMount(source)

			if err := translateBindMountSources(project, "/deployments", "/srv/orca/deployments"); err != nil {
				t.Fatalf("translateBindMountSources: %v", err)
			}

			if got := project.Services["app"].Volumes[0].Source; got != source {
				t.Errorf("bind source = %q, want unchanged %q", got, source)
			}
		})
	}
}

func TestTranslateBindMountSources_NoHostMappingIsNoOp(t *testing.T) {
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
