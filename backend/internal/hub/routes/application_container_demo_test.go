package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	"gorm.io/gorm"
)

func setupDemoContainerTest(t *testing.T, appID string) {
	t.Helper()
	app, _ := setupStreamTest(t)
	if _, err := gorm.G[models.Application](db.DB).Where("id = ?", app.Id).Update(context.Background(), "id", appID); err != nil {
		t.Fatalf("rename application: %v", err)
	}
	DemoMode = true
	t.Cleanup(func() { DemoMode = false })
	// No hub and no connected agent, like in a real demo
	websocket.DefaultHub = nil
}

func demoList(t *testing.T, appID string) []containerResponse {
	t.Helper()
	w := containerRequest(t, ListApplicationContainersHandler, appID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body containerListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Containers == nil {
		t.Fatal("expected a containers array")
	}
	return body.Containers
}

func demoStreamRequest(t *testing.T, appID, containerID, suffix string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	streamRouter(rbac.Admin()).ServeHTTP(w,
		httptest.NewRequest(http.MethodGet, "/applications/"+appID+"/containers/"+containerID+"/"+suffix, nil))
	return w
}

func TestDemoContainers_List(t *testing.T) {
	for appID, services := range map[string][]string{
		demoEnvWebAppID:        {"web"},
		demoEnvMonitoringAppID: {"alertmanager", "blackbox-exporter", "prometheus"},
	} {
		t.Run(appID, func(t *testing.T) {
			setupDemoContainerTest(t, appID)

			containers := demoList(t, appID)
			got := make([]string, 0, len(containers))
			for _, container := range containers {
				got = append(got, container.Service)
				if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(container.Id) {
					t.Errorf("%s: invalid id %q", container.Service, container.Id)
				}
				if container.State != "running" || container.StartedAt == nil {
					t.Errorf("%s: expected a running container, got %+v", container.Service, container)
				}
				if container.Ports == nil || container.Mounts == nil || len(container.Networks) == 0 {
					t.Errorf("%s: expected arrays and a network, got %+v", container.Service, container)
				}
			}
			if !slices.Equal(got, services) {
				t.Fatalf("expected services %v, got %v", services, got)
			}

			// The UI reconnects its streams when the start time of a container changes.
			time.Sleep(10 * time.Millisecond)
			if again := demoList(t, appID); !again[0].StartedAt.Equal(*containers[0].StartedAt) {
				t.Errorf("start time changed from %v to %v", containers[0].StartedAt, again[0].StartedAt)
			}
		})
	}
}

func TestDemoContainers_ListErrors(t *testing.T) {
	setupDemoContainerTest(t, demoWorkerAppID)

	if w := containerRequest(t, ListApplicationContainersHandler, demoWorkerAppID, ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("offline agent: expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if w := containerRequest(t, ListApplicationContainersHandler, "does-not-exist", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown application: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDemoContainers_Inspect(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]

	w := containerRequest(t, InspectApplicationContainerHandler, demoEnvWebAppID, container.Id[:12])
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body containerInspectResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	var inspect struct {
		Id     string
		Config map[string]any
	}
	if err := json.Unmarshal([]byte(body.InspectJson), &inspect); err != nil {
		t.Fatalf("invalid inspect JSON: %v", err)
	}
	if inspect.Id != container.Id || inspect.Config["Image"] != container.Image {
		t.Errorf("inspect does not match the container: %s", body.InspectJson)
	}
	if _, ok := inspect.Config["Env"]; ok {
		t.Error("inspect must not contain the environment")
	}

	if w := containerRequest(t, InspectApplicationContainerHandler, demoEnvWebAppID, testContainerID); w.Code != http.StatusNotFound {
		t.Errorf("unknown container: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDemoContainers_LogsStreamEndsAfterTheCannedLog(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]

	w := demoStreamRequest(t, demoEnvWebAppID, container.Id, "logs/stream?follow=true")

	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected an event stream, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"event: log\n", `"stream":"stdout"`, `"stream":"stderr"`, "event: end\ndata: {\"reason\":\"completed\""} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
}

func TestDemoContainers_LogsDownload(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]

	w := demoStreamRequest(t, demoEnvWebAppID, container.Id, "logs/download")

	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("expected an attachment, got %d: %s", w.Code, w.Body.String())
	}
	if lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n"); len(lines) < 5 {
		t.Errorf("expected several log lines, got %q", w.Body.String())
	}
}

func TestDemoContainers_StatsStream(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]
	original := demoStatsInterval
	t.Cleanup(func() { demoStatsInterval = original })
	demoStatsInterval = 5 * time.Millisecond

	server := newStreamServer(t, rbac.Admin())
	resp, cancel := openStream(t, server, server.URL+"/applications/"+demoEnvWebAppID+"/containers/"+container.Id+"/stats/stream")
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)

	var previous statsEvent
	for range 3 {
		event := readEvent(t, scanner)
		if event.Name != "stats" {
			t.Fatalf("expected stats, got %+v", event)
		}
		var sample statsEvent
		if err := json.Unmarshal([]byte(event.Data), &sample); err != nil {
			t.Fatalf("invalid stats: %v", err)
		}
		if sample.CpuPercent <= 0 || sample.MemUsageBytes == 0 || sample.MemUsageBytes > sample.MemLimitBytes || sample.OnlineCpus == 0 {
			t.Fatalf("implausible sample: %+v", sample)
		}
		if sample.NetRxBytes < previous.NetRxBytes || sample.NetTxBytes < previous.NetTxBytes {
			t.Fatalf("network counters went backwards: %+v after %+v", sample, previous)
		}
		previous = sample
	}
}

func TestDemoContainers_StatsSlotsArePerAddress(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]

	const visitor = "192.0.2.1"
	for range maxContainerStreamsPerUser {
		if !containerStreamSlots.acquire(visitor) {
			t.Fatal("failed to fill the slots")
		}
		t.Cleanup(func() { containerStreamSlots.release(visitor) })
	}

	if w := demoStreamRequest(t, demoEnvWebAppID, container.Id, "stats/stream"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDemoContainers_StatsSlotsAreSharedByAnIPv6Prefix(t *testing.T) {
	setupDemoContainerTest(t, demoEnvWebAppID)
	container := demoList(t, demoEnvWebAppID)[0]

	for range maxContainerStreamsPerUser {
		if !containerStreamSlots.acquire("2001:db8::/64") {
			t.Fatal("failed to fill the slots")
		}
		t.Cleanup(func() { containerStreamSlots.release("2001:db8::/64") })
	}

	// Ends the stream in case it is accepted
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet,
		"/applications/"+demoEnvWebAppID+"/containers/"+container.Id+"/stats/stream", nil)
	req.RemoteAddr = "[2001:db8::abcd]:1234"
	w := httptest.NewRecorder()
	streamRouter(rbac.Admin()).ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDemoContainers_StreamErrors(t *testing.T) {
	setupDemoContainerTest(t, demoWorkerAppID)

	for _, suffix := range []string{"logs/stream", "logs/download", "stats/stream"} {
		if w := demoStreamRequest(t, demoWorkerAppID, testContainerID, suffix); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s offline agent: expected 503, got %d: %s", suffix, w.Code, w.Body.String())
		}
		if w := demoStreamRequest(t, "does-not-exist", testContainerID, suffix); w.Code != http.StatusNotFound {
			t.Errorf("%s unknown application: expected 404, got %d: %s", suffix, w.Code, w.Body.String())
		}
		if w := demoStreamRequest(t, demoWorkerAppID, "nope", suffix); w.Code != http.StatusBadRequest {
			t.Errorf("%s invalid id: expected 400, got %d: %s", suffix, w.Code, w.Body.String())
		}
	}
}
