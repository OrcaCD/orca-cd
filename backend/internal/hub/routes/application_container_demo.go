package routes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	demoWorkerAppID = "demo-app-worker"

	demoHostCPUs        = 4
	demoHostMemoryBytes = 8 << 30
)

var demoStatsInterval = 2 * time.Second

var demoContainersEpoch = time.Now().UTC().Truncate(time.Second)

type demoPort struct {
	public, private uint32
}

type demoLogLine struct {
	after  time.Duration
	stderr bool
	text   string
}

type demoContainer struct {
	applicationID string
	project       string
	service       string
	image         string
	digest        string
	command       []string
	healthcheck   bool
	uptime        time.Duration
	ip            string
	ports         []demoPort
	mounts        []containerMountResponse
	cpuPercent    float64
	memoryBytes   float64
	rxPerSecond   float64
	logs          []demoLogLine
}

func demoContainers(applicationID string) []demoContainer {
	const day = 24 * time.Hour

	volume := func(name, destination string) containerMountResponse {
		return containerMountResponse{Type: "volume", Name: name, Destination: destination}
	}
	config := func(destination string) containerMountResponse {
		return containerMountResponse{Type: "bind", Destination: destination, ReadOnly: true}
	}

	switch applicationID {
	case demoEnvWebAppID:
		return []demoContainer{{
			applicationID: applicationID,
			project:       "demo-web-app",
			service:       "web",
			image:         "ghcr.io/orcacd/demo-app:1.4.2",
			digest:        "b2d8f4a0c6e1937d5b2f8a4c0e6d1b7f3a9c5e2d8b4f0a6c1e7d3b9f5a2c8e4d",
			command:       []string{"node", "server.js"},
			uptime:        3 * time.Hour,
			ip:            "172.18.0.2",
			ports:         []demoPort{{public: 8080, private: 80}},
			cpuPercent:    6,
			memoryBytes:   142 << 20,
			rxPerSecond:   24_000,
			logs: []demoLogLine{
				{after: time.Second, text: "INFO demo-app 1.4.2 starting"},
				{after: 2 * time.Second, text: "INFO connected to the database"},
				{after: 2 * time.Second, text: "INFO listening on port 80"},
				{after: 14 * time.Minute, text: "INFO GET / 200 12ms"},
				{after: 52 * time.Minute, text: "INFO GET /api/orders 200 38ms"},
				{after: 88 * time.Minute, stderr: true, text: "WARN slow query took 1240ms: GET /api/reports"},
				{after: 131 * time.Minute, text: "INFO POST /api/orders 201 54ms"},
				{after: 164 * time.Minute, stderr: true, text: "ERROR payment provider timed out, retrying"},
				{after: 164*time.Minute + 2*time.Second, text: "INFO payment provider reachable again"},
				{after: 179 * time.Minute, text: "INFO GET /healthz 200 1ms"},
			},
		}}
	case demoEnvMonitoringAppID:
		return []demoContainer{
			{
				applicationID: applicationID,
				project:       "monitoring-stack",
				service:       "alertmanager",
				image:         "prom/alertmanager:v0.28.1",
				digest:        "6f1b3d5a7c9e0f2b4d6a8c1e3f5b7d9a0c2e4f6b8d1a3c5e7f9b0d2a4c6e8f1b",
				command:       []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/alertmanager"},
				uptime:        4 * day,
				ip:            "172.19.0.3",
				ports:         []demoPort{{public: 9093, private: 9093}},
				mounts: []containerMountResponse{
					volume("monitoring-stack_alertmanager-data", "/alertmanager"),
					config("/etc/alertmanager/alertmanager.yml"),
				},
				cpuPercent:  1.5,
				memoryBytes: 38 << 20,
				rxPerSecond: 1_800,
				logs: []demoLogLine{
					{after: time.Second, stderr: true, text: `level=info msg="Starting Alertmanager" version=0.28.1`},
					{after: 2 * time.Second, stderr: true, text: `level=info msg="Completed loading of configuration file"`},
					{after: 2 * time.Second, stderr: true, text: `level=info msg="Listening on" address=[::]:9093`},
					{after: 2 * day, stderr: true, text: `level=warn msg="Notify attempt failed, will retry later" receiver=team-chat`},
					{after: 2*day + 30*time.Second, stderr: true, text: `level=info msg="Notify success" receiver=team-chat`},
				},
			},
			{
				applicationID: applicationID,
				project:       "monitoring-stack",
				service:       "blackbox-exporter",
				image:         "prom/blackbox-exporter:v0.27.0",
				digest:        "8a2c4e6f0b1d3a5c7e9f2b4d6a8c0e1f3b5d7a9c2e4f6b8d0a1c3e5f7b9d2a4c",
				command:       []string{"--config.file=/etc/blackbox/blackbox.yml"},
				uptime:        4 * day,
				ip:            "172.19.0.4",
				mounts:        []containerMountResponse{config("/etc/blackbox/blackbox.yml")},
				cpuPercent:    0.8,
				memoryBytes:   21 << 20,
				rxPerSecond:   5_200,
				logs: []demoLogLine{
					{after: time.Second, stderr: true, text: `level=info msg="Starting blackbox_exporter" version=0.27.0`},
					{after: time.Second, stderr: true, text: `level=info msg="Loaded config file"`},
					{after: 2 * time.Second, stderr: true, text: `level=info msg="Listening on" address=[::]:9115`},
					{after: day, stderr: true, text: `level=error msg="Error for HTTP request" target=https://shop.example.com err="context deadline exceeded"`},
				},
			},
			{
				applicationID: applicationID,
				project:       "monitoring-stack",
				service:       "prometheus",
				image:         "prom/prometheus:v3.5.0",
				digest:        "e5a1c7d3b9f2084a6e1c7d3b9f5a2e8c4d0b6a1f7c3e9d5b2a8f4c0e6d1b7a3f",
				command: []string{
					"--config.file=/etc/prometheus/prometheus.yml",
					"--storage.tsdb.path=/prometheus",
					"--storage.tsdb.retention.time=15d",
					"--web.enable-lifecycle",
				},
				healthcheck: true,
				// Recreated by the rollback
				uptime: 46 * time.Minute,
				ip:     "172.19.0.2",
				ports:  []demoPort{{public: 9090, private: 9090}},
				mounts: []containerMountResponse{
					config("/etc/prometheus/prometheus.yml"),
					volume("monitoring-stack_prometheus-data", "/prometheus"),
				},
				cpuPercent:  14,
				memoryBytes: 412 << 20,
				rxPerSecond: 86_000,
				logs: []demoLogLine{
					{after: time.Second, stderr: true, text: `level=INFO msg="Starting Prometheus Server" version=3.5.0`},
					{after: 3 * time.Second, stderr: true, text: `level=INFO msg="TSDB started"`},
					{after: 3 * time.Second, stderr: true, text: `level=INFO msg="Completed loading of configuration file"`},
					{after: 4 * time.Second, stderr: true, text: `level=INFO msg="Server is ready to receive web requests."`},
					{after: 12 * time.Minute, stderr: true, text: `level=WARN msg="Scrape failed" target=grafana:3000 err="connection refused"`},
					{after: 31 * time.Minute, stderr: true, text: `level=INFO msg="Head GC completed" duration=18ms`},
				},
			},
		}
	default:
		return nil
	}
}

func (d demoContainer) id() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(d.applicationID+"/"+d.service)))
}

func (d demoContainer) name() string {
	return d.project + "-" + d.service + "-1"
}

func (d demoContainer) startedAt() time.Time {
	return demoContainersEpoch.Add(-d.uptime)
}

func (d demoContainer) response() containerResponse {
	startedAt := d.startedAt()
	createdAt := startedAt.Add(-time.Second)
	repository, _, _ := strings.Cut(d.image, ":")

	response := containerResponse{
		Id:              d.id(),
		Name:            d.name(),
		Service:         d.service,
		ContainerNumber: 1,
		State:           "running",
		Health:          models.Healthy,
		Image:           d.image,
		ImageId:         "sha256:" + d.digest,
		ImageDigest:     repository + "@sha256:" + d.digest,
		RestartPolicy:   "unless-stopped",
		LogDriver:       "json-file",
		CreatedAt:       &createdAt,
		StartedAt:       &startedAt,
		Ports:           []portBindingResponse{},
		Networks: []containerNetworkResponse{
			{Name: d.project + "_default", IpAddress: d.ip, Aliases: []string{d.name(), d.service}},
		},
		Mounts: append([]containerMountResponse{}, d.mounts...),
	}
	if d.healthcheck {
		response.HealthRaw = "healthy"
	}
	for _, port := range d.ports {
		for _, ip := range []string{"0.0.0.0", "::"} {
			response.Ports = append(response.Ports, portBindingResponse{
				Ip: ip, PublicPort: port.public, PrivatePort: port.private, Protocol: "tcp",
			})
		}
	}
	return response
}

func (d demoContainer) inspectJSON() (string, error) {
	response := d.response()

	state := map[string]any{
		"Status":     response.State,
		"Running":    true,
		"ExitCode":   0,
		"StartedAt":  response.StartedAt.Format(time.RFC3339Nano),
		"FinishedAt": "0001-01-01T00:00:00Z",
	}
	if d.healthcheck {
		state["Health"] = map[string]any{"Status": "healthy", "FailingStreak": 0}
	}

	portBindings := map[string]any{}
	for _, port := range d.ports {
		portBindings[strconv.FormatUint(uint64(port.private), 10)+"/tcp"] = []map[string]string{
			{"HostIp": "", "HostPort": strconv.FormatUint(uint64(port.public), 10)},
		}
	}
	mounts := []map[string]any{}
	for _, mount := range d.mounts {
		mounts = append(mounts, map[string]any{
			"Type": mount.Type, "Name": mount.Name, "Destination": mount.Destination, "RW": !mount.ReadOnly,
		})
	}

	data, err := json.MarshalIndent(map[string]any{
		"Id":           response.Id,
		"Name":         "/" + response.Name,
		"Created":      response.CreatedAt.Format(time.RFC3339Nano),
		"Image":        response.ImageId,
		"RestartCount": response.RestartCount,
		"State":        state,
		"Config": map[string]any{
			"Image": response.Image,
			"Cmd":   d.command,
			"Labels": map[string]string{
				"com.docker.compose.project": d.project,
				"com.docker.compose.service": d.service,
				"orca-cd.application-id":     d.applicationID,
			},
		},
		"HostConfig": map[string]any{
			"RestartPolicy": map[string]any{"Name": response.RestartPolicy, "MaximumRetryCount": 0},
			"LogConfig":     map[string]any{"Type": response.LogDriver},
			"PortBindings":  portBindings,
		},
		"Mounts": mounts,
		"NetworkSettings": map[string]any{
			"Networks": map[string]any{
				response.Networks[0].Name: map[string]any{"IPAddress": d.ip, "Aliases": response.Networks[0].Aliases},
			},
		},
	}, "", "  ")
	return string(data), err
}

func (d demoContainer) logLines() []logLineEvent {
	lines := make([]logLineEvent, 0, len(d.logs))
	for _, line := range d.logs {
		stream := "stdout"
		if line.stderr {
			stream = "stderr"
		}
		lines = append(lines, logLineEvent{
			Timestamp: d.startedAt().Add(line.after).Format(time.RFC3339Nano),
			Stream:    stream,
			Text:      line.text,
		})
	}
	return lines
}

func (d demoContainer) stats(now time.Time) statsEvent {
	elapsed := now.Sub(d.startedAt()).Seconds()
	phase := float64(len(d.service))
	received := d.rxPerSecond * (elapsed + 2.5*math.Sin(elapsed/5))

	return statsEvent{
		TimestampMs:   now.UnixMilli(),
		CpuPercent:    d.cpuPercent * (1 + 0.4*math.Sin(elapsed/7+phase)),
		MemUsageBytes: uint64(d.memoryBytes * (1 + 0.03*math.Sin(elapsed/23+phase))),
		MemLimitBytes: demoHostMemoryBytes,
		NetRxBytes:    uint64(received),
		NetTxBytes:    uint64(received * 0.4),
		Pids:          12,
		OnlineCpus:    demoHostCPUs,
	}
}

func loadDemoContainers(ctx context.Context, applicationID string) ([]demoContainer, error) {
	application, err := gorm.G[models.Application](db.DB).Where("id = ?", applicationID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("application not found")
		}
		return nil, internalError()
	}
	if application.Id == demoWorkerAppID {
		return nil, huma.Error503ServiceUnavailable(containerAgentOfflineMessage)
	}
	return demoContainers(application.Id), nil
}

func findDemoContainer(ctx context.Context, applicationID, containerID string) (demoContainer, error) {
	containers, err := loadDemoContainers(ctx, applicationID)
	if err != nil {
		return demoContainer{}, err
	}
	for _, container := range containers {
		if strings.HasPrefix(container.id(), containerID) {
			return container, nil
		}
	}
	return demoContainer{}, huma.Error404NotFound("container not found")
}

func listDemoApplicationContainers(ctx context.Context, applicationID string) (*containerListOutput, error) {
	containers, err := loadDemoContainers(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	response := containerListResponse{Containers: make([]containerResponse, 0, len(containers))}
	for _, container := range containers {
		response.Containers = append(response.Containers, container.response())
	}
	return &containerListOutput{Body: response}, nil
}

func inspectDemoApplicationContainer(ctx context.Context, applicationID, containerID string) (*containerInspectOutput, error) {
	container, err := findDemoContainer(ctx, applicationID, containerID)
	if err != nil {
		return nil, err
	}
	inspect, err := container.inspectJSON()
	if err != nil {
		return nil, internalError()
	}
	return &containerInspectOutput{Body: containerInspectResponse{InspectJson: inspect}}, nil
}

func demoStreamContainer(c *gin.Context) (demoContainer, bool) {
	if !containerIDPattern.MatchString(c.Param("containerId")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid container id"})
		return demoContainer{}, false
	}
	container, err := findDemoContainer(c.Request.Context(), c.Param("id"), c.Param("containerId"))
	if err != nil {
		respondContainerError(c, err)
		return demoContainer{}, false
	}
	return container, true
}

func serveDemoLogs(c *gin.Context) {
	container, ok := demoStreamContainer(c)
	if !ok || !startEventStream(c) {
		return
	}
	out := sseStream{c}
	if out.event("log", logsEvent{Lines: container.logLines()}) {
		out.event("end", streamEndEvent{Reason: "completed"})
	}
}

func serveDemoLogDownload(c *gin.Context) {
	container, ok := demoStreamContainer(c)
	if !ok {
		return
	}
	startLogDownload(c)
	_, _ = c.Writer.WriteString(formatLogDownload(container.logLines()))
}

func serveDemoStats(c *gin.Context) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}
	container, ok := demoStreamContainer(c)
	if !ok {
		return
	}

	address := middleware.ClientKey(c.ClientIP())
	if !containerStreamSlots.acquire(address) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many open container streams"})
		return
	}
	defer containerStreamSlots.release(address)

	if !startEventStream(c) {
		return
	}

	out := sseStream{c}
	expiry := time.NewTimer(time.Until(claims.ExpiresAt.Time))
	defer expiry.Stop()
	limit := time.NewTimer(containerStreamMaxDuration)
	defer limit.Stop()
	ticker := time.NewTicker(demoStatsInterval)
	defer ticker.Stop()

	for {
		if !out.event("stats", container.stats(time.Now())) {
			return
		}
		select {
		case <-ticker.C:
		case <-expiry.C:
			writeUnauthorizedEvent(c)
			return
		case <-limit.C:
			out.event("end", streamEndEvent{Reason: "limit"})
			return
		case <-c.Request.Context().Done():
			return
		}
	}
}
