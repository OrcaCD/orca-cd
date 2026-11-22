package docker

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	cerrdefs "github.com/containerd/errdefs"
)

const (
	webID   = "aaaaaaaaaaaa1111111111111111111111111111111111111111111111111111"
	dbID    = "bbbbbbbbbbbb2222222222222222222222222222222222222222222222222222"
	oneOff  = "cccccccccccc3333333333333333333333333333333333333333333333333333"
	foreign = "dddddddddddd4444444444444444444444444444444444444444444444444444"
)

type fakeContainerAPI struct {
	client.APIClient

	results map[string]client.ContainerInspectResult
	errs    map[string]error
	calls   []string
}

func (f *fakeContainerAPI) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	f.calls = append(f.calls, id)
	if err := f.errs[id]; err != nil {
		return client.ContainerInspectResult{}, err
	}
	result, ok := f.results[id]
	if !ok {
		return client.ContainerInspectResult{}, cerrdefs.ErrNotFound
	}
	return result, nil
}

func appSummary(id, service, state string) containerapi.Summary {
	return containerapi.Summary{
		ID:      id,
		Names:   []string{"/proj-" + service + "-1"},
		Image:   "ghcr.io/org/" + service + ":1",
		ImageID: "sha256:" + service,
		State:   containerapi.ContainerState(state),
		Labels:  map[string]string{api.ServiceLabel: service},
	}
}

func webInspect() client.ContainerInspectResult {
	return client.ContainerInspectResult{
		Container: containerapi.InspectResponse{
			ID:           webID,
			Name:         "/proj-web-1",
			Created:      "2026-01-02T03:04:05.5Z",
			RestartCount: 4,
			State: &containerapi.State{
				Status:     containerapi.StateRunning,
				ExitCode:   137,
				OOMKilled:  true,
				Error:      "boom",
				StartedAt:  "2026-01-02T03:04:06Z",
				FinishedAt: "0001-01-01T00:00:00Z",
				Health:     &containerapi.Health{Status: containerapi.Unhealthy, FailingStreak: 3},
			},
			HostConfig: &containerapi.HostConfig{
				RestartPolicy: containerapi.RestartPolicy{Name: "on-failure", MaximumRetryCount: 5},
				LogConfig:     containerapi.LogConfig{Type: "json-file"},
			},
			Mounts: []containerapi.MountPoint{
				{Type: mount.TypeVolume, Name: "data", Source: "/var/lib/docker/volumes/data/_data", Destination: "/var/lib/data", RW: true},
				{Type: mount.TypeBind, Source: "/srv/secret-host-path", Destination: "/config", RW: false},
			},
			NetworkSettings: &containerapi.NetworkSettings{
				Ports: network.PortMap{
					network.MustParsePort("8080/tcp"): {
						{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: "80"},
						{HostIP: netip.MustParseAddr("::"), HostPort: "80"},
					},
					network.MustParsePort("9090/tcp"): nil,
				},
				Networks: map[string]*network.EndpointSettings{
					"proj_default": {IPAddress: netip.MustParseAddr("172.18.0.3"), Aliases: []string{"web", "web-1"}},
					"edge":         {IPAddress: netip.MustParseAddr("172.19.0.2")},
					"empty":        nil,
				},
			},
		},
	}
}

func dbInspect() client.ContainerInspectResult {
	return client.ContainerInspectResult{
		Container: containerapi.InspectResponse{
			ID:      dbID,
			Name:    "/proj-db-1",
			Created: "2026-01-01T00:00:00Z",
			State:   &containerapi.State{Status: containerapi.StateExited, ExitCode: 1, FinishedAt: "2026-01-02T00:00:00Z"},
		},
	}
}

func stubContainers(t *testing.T, items []containerapi.Summary) {
	t.Helper()
	stubServiceImageLookups(t, items, map[string][]string{
		"sha256:web": {"ghcr.io/org/web@sha256:" + digestAaa},
		"sha256:db":  nil,
	})
}

func TestListContainerInfos(t *testing.T) {
	web := appSummary(webID, "web", "running")
	web.Labels[api.ContainerNumberLabel] = "2"
	db := appSummary(dbID, "db", "exited")
	oneOffItem := appSummary(oneOff, "web", "exited")
	oneOffItem.Labels[api.OneoffLabel] = "True"
	stubContainers(t, []containerapi.Summary{web, oneOffItem, db})

	cli := &fakeContainerAPI{results: map[string]client.ContainerInspectResult{webID: webInspect(), dbID: dbInspect()}}
	infos, err := listContainerInfos(t.Context(), cli, "app-1")
	if err != nil {
		t.Fatalf("listContainerInfos: %v", err)
	}

	if len(infos) != 2 || infos[0].Service != "db" || infos[1].Service != "web" {
		t.Fatalf("expected db then web, got %v", infos)
	}
	for _, id := range cli.calls {
		if id == oneOff {
			t.Error("one-off container must not be inspected")
		}
	}

	got := infos[1]
	if got.Id != webID || got.Name != "proj-web-1" || got.State != "running" {
		t.Errorf("identity fields: %+v", got)
	}
	if got.Health != messages.HealthStatus_HEALTH_STATUS_UNHEALTHY || got.HealthRaw != "unhealthy" || got.HealthFailingStreak != 3 {
		t.Errorf("health fields: %v %q %d", got.Health, got.HealthRaw, got.HealthFailingStreak)
	}
	if got.Image != "ghcr.io/org/web:1" || got.ImageId != "sha256:web" || got.ImageDigest != "ghcr.io/org/web@sha256:"+digestAaa {
		t.Errorf("image fields: %q %q %q", got.Image, got.ImageId, got.ImageDigest)
	}
	if got.RestartCount != 4 || got.ExitCode != 137 || !got.OomKilled || got.Error != "boom" {
		t.Errorf("state fields: %+v", got)
	}
	if got.CreatedAtMs != 1767323045500 || got.StartedAtMs != 1767323046000 || got.FinishedAtMs != 0 {
		t.Errorf("timestamps: %d %d %d", got.CreatedAtMs, got.StartedAtMs, got.FinishedAtMs)
	}
	if got.ContainerNumber != 2 || got.RestartPolicy != "on-failure:5" || got.LogDriver != "json-file" {
		t.Errorf("config fields: %d %q %q", got.ContainerNumber, got.RestartPolicy, got.LogDriver)
	}

	wantPorts := []*messages.PortBinding{
		{Ip: "0.0.0.0", PublicPort: 80, PrivatePort: 8080, Protocol: "tcp"},
		{Ip: "::", PublicPort: 80, PrivatePort: 8080, Protocol: "tcp"},
		{PrivatePort: 9090, Protocol: "tcp"},
	}
	if len(got.Ports) != len(wantPorts) {
		t.Fatalf("ports = %v, want %v", got.Ports, wantPorts)
	}
	for i, want := range wantPorts {
		p := got.Ports[i]
		if p.Ip != want.Ip || p.PublicPort != want.PublicPort || p.PrivatePort != want.PrivatePort || p.Protocol != want.Protocol {
			t.Errorf("port %d = %v, want %v", i, p, want)
		}
	}

	if len(got.Networks) != 2 || got.Networks[0].Name != "edge" || got.Networks[1].Name != "proj_default" ||
		got.Networks[1].IpAddress != "172.18.0.3" || strings.Join(got.Networks[1].Aliases, ",") != "web,web-1" {
		t.Errorf("networks = %v", got.Networks)
	}

	if len(got.Mounts) != 2 || got.Mounts[0].Destination != "/config" || !got.Mounts[0].ReadOnly || got.Mounts[0].Type != "bind" ||
		got.Mounts[1].Name != "data" || got.Mounts[1].ReadOnly {
		t.Errorf("mounts = %v", got.Mounts)
	}
	for _, m := range got.Mounts {
		if strings.Contains(m.String(), "secret-host-path") {
			t.Error("host paths must not be part of the container list")
		}
	}

	stopped := infos[0]
	if stopped.Health != messages.HealthStatus_HEALTH_STATUS_STOPPED || stopped.HealthRaw != "" || stopped.FinishedAtMs == 0 {
		t.Errorf("stopped container: %+v", stopped)
	}
	if stopped.ImageDigest != "" {
		t.Errorf("image without a registry digest must report none, got %q", stopped.ImageDigest)
	}
}

func TestListContainerInfos_SkipsContainersRemovedMeanwhile(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running"), appSummary(dbID, "db", "exited")})
	cli := &fakeContainerAPI{results: map[string]client.ContainerInspectResult{webID: webInspect()}}

	infos, err := listContainerInfos(t.Context(), cli, "app-1")
	if err != nil {
		t.Fatalf("listContainerInfos: %v", err)
	}
	if len(infos) != 1 || infos[0].Service != "web" {
		t.Errorf("infos = %v", infos)
	}
}

func TestListContainerInfos_InspectError(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeContainerAPI{errs: map[string]error{webID: errors.New("daemon gone")}}

	if _, err := listContainerInfos(t.Context(), cli, "app-1"); err == nil || !strings.Contains(err.Error(), "daemon gone") {
		t.Errorf("expected the inspect error, got %v", err)
	}
}

func TestListContainerInfos_ListError(t *testing.T) {
	orig := listApplicationContainers
	t.Cleanup(func() { listApplicationContainers = orig })
	listApplicationContainers = func(context.Context, client.APIClient, string) ([]containerapi.Summary, error) {
		return nil, errors.New("list failed")
	}

	if _, err := listContainerInfos(t.Context(), &fakeContainerAPI{}, "app-1"); err == nil {
		t.Error("expected an error")
	}
}

func TestListContainerInfos_NoContainers(t *testing.T) {
	stubContainers(t, nil)
	infos, err := listContainerInfos(t.Context(), &fakeContainerAPI{}, "app-1")
	if err != nil || len(infos) != 0 {
		t.Errorf("infos = %v, err = %v", infos, err)
	}
}

func TestResolveContainer(t *testing.T) {
	oneOffItem := appSummary(oneOff, "web", "exited")
	oneOffItem.Labels[api.OneoffLabel] = "True"
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running"), appSummary(dbID, "db", "running"), oneOffItem})

	tests := []struct {
		name    string
		id      string
		wantID  string
		wantErr bool
	}{
		{"full id", webID, webID, false},
		{"short id", webID[:12], webID, false},
		{"too short prefix", webID[:6], "", true},
		{"unknown container of another application", foreign, "", true},
		{"unknown prefix", foreign[:12], "", true},
		{"one-off container", oneOff, "", true},
		{"name instead of id", "proj-web-1", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveContainer(t.Context(), &fakeContainerAPI{}, "app-1", tt.id)
			if tt.wantErr {
				if !errors.Is(err, ErrContainerNotFound) {
					t.Fatalf("err = %v, want ErrContainerNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveContainer: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}

func TestInspectSanitized(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeContainerAPI{results: map[string]client.ContainerInspectResult{
		webID: {Raw: []byte(`{"Id":"` + webID + `","Config":{"Env":["TOKEN=planted"],"Image":"web"}}`)},
	}}

	out, err := inspectSanitized(t.Context(), cli, "app-1", webID[:12])
	if err != nil {
		t.Fatalf("inspectSanitized: %v", err)
	}
	if strings.Contains(out, "planted") || strings.Contains(out, "Env") {
		t.Errorf("environment leaked: %s", out)
	}
	if !strings.Contains(out, `"Image": "web"`) {
		t.Errorf("unexpected output: %s", out)
	}
	if len(cli.calls) != 1 || cli.calls[0] != webID {
		t.Errorf("must inspect the resolved full id, calls = %v", cli.calls)
	}
}

func TestInspectSanitized_ForeignContainerIsNeverInspected(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := &fakeContainerAPI{results: map[string]client.ContainerInspectResult{foreign: {Raw: []byte(`{}`)}}}

	_, err := inspectSanitized(t.Context(), cli, "app-1", foreign)
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("err = %v, want ErrContainerNotFound", err)
	}
	if len(cli.calls) != 0 {
		t.Errorf("a container of another application was inspected: %v", cli.calls)
	}
}

func TestInspectSanitized_RemovedMeanwhile(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})

	_, err := inspectSanitized(t.Context(), &fakeContainerAPI{}, "app-1", webID)
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("err = %v, want ErrContainerNotFound", err)
	}
}

func TestClientContainerMethods_DaemonUnreachable(t *testing.T) {
	c := newTestClient(t)
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()

	if _, err := c.ListContainers(t.Context(), "app-1"); !errors.Is(err, ErrDaemonUnreachable) {
		t.Errorf("ListContainers err = %v, want ErrDaemonUnreachable", err)
	}
	if _, err := c.InspectContainer(t.Context(), "app-1", webID); !errors.Is(err, ErrDaemonUnreachable) {
		t.Errorf("InspectContainer err = %v, want ErrDaemonUnreachable", err)
	}
}
