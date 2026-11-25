package routes

import (
	"bufio"
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

// A user per test, so the stream slots of a test that is still closing do not leak into the next one.
var streamTestUserID string

type streamTestAgent struct {
	hub    *websocket.Hub
	client *websocket.Client

	mu         sync.Mutex
	reject     *messages.StreamEnd
	logReqs    chan *messages.ContainerLogsRequest
	statsReqs  chan *messages.ContainerStatsRequest
	cancels    chan string
	keepAlives chan string
}

func withStreamAgent(t *testing.T, agentID string) *streamTestAgent {
	t.Helper()
	nop := zerolog.Nop()
	hub := websocket.NewHub(&nop)
	websocket.DefaultHub = hub
	t.Cleanup(func() { websocket.DefaultHub = nil })

	client, err := hub.Register(agentID, nil)
	if err != nil {
		t.Fatalf("failed to register fake agent: %v", err)
	}
	client.SetCapabilities(monitoringCapabilities)

	agent := &streamTestAgent{
		hub:        hub,
		client:     client,
		logReqs:    make(chan *messages.ContainerLogsRequest, 16),
		statsReqs:  make(chan *messages.ContainerStatsRequest, 16),
		cancels:    make(chan string, 16),
		keepAlives: make(chan string, 16),
	}
	go func() {
		for msg := range client.Send {
			switch {
			case msg.GetContainerLogsRequest() != nil:
				req := msg.GetContainerLogsRequest()
				if agent.answer(req.RequestId) {
					agent.logReqs <- req
				}
			case msg.GetContainerStatsRequest() != nil:
				req := msg.GetContainerStatsRequest()
				if agent.answer(req.RequestId) {
					agent.statsReqs <- req
				}
			case msg.GetStreamCancel() != nil:
				agent.cancels <- msg.GetStreamCancel().RequestId
			case msg.GetStreamKeepAlive() != nil:
				agent.keepAlives <- msg.GetStreamKeepAlive().RequestId
			}
		}
	}()
	t.Cleanup(client.Close)
	return agent
}

// answer acknowledges a request, or rejects it when a rejection is queued.
func (a *streamTestAgent) answer(requestID string) bool {
	a.mu.Lock()
	reject := a.reject
	a.mu.Unlock()
	if reject != nil {
		a.hub.DeliverStreamEnd(a.client, &messages.StreamEnd{RequestId: requestID, Reason: reject.Reason, ErrorMessage: reject.ErrorMessage})
		return false
	}
	a.hub.ResolveStreamStarted(a.client, &messages.StreamStarted{RequestId: requestID})
	return true
}

func (a *streamTestAgent) rejectWith(reason messages.StreamEndReason) {
	a.mu.Lock()
	a.reject = &messages.StreamEnd{Reason: reason, ErrorMessage: "rejected"}
	a.mu.Unlock()
}

func (a *streamTestAgent) sendLogs(requestID string, dropped int64, lines ...*messages.LogLine) {
	a.hub.DeliverLogs(a.client, &messages.ContainerLogsChunk{RequestId: requestID, Lines: lines, DroppedLines: dropped})
}

func (a *streamTestAgent) sendStats(requestID string, sample *messages.ContainerStatsSample) {
	sample.RequestId = requestID
	a.hub.DeliverStats(a.client, sample)
}

func (a *streamTestAgent) end(requestID string, reason messages.StreamEndReason) {
	a.hub.DeliverStreamEnd(a.client, &messages.StreamEnd{RequestId: requestID, Reason: reason})
}

func (a *streamTestAgent) nextLogs(t *testing.T) *messages.ContainerLogsRequest {
	t.Helper()
	select {
	case req := <-a.logReqs:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not receive a logs request")
		return nil
	}
}

func (a *streamTestAgent) nextStats(t *testing.T) *messages.ContainerStatsRequest {
	t.Helper()
	select {
	case req := <-a.statsReqs:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not receive a stats request")
		return nil
	}
}

func (a *streamTestAgent) expectCancel(t *testing.T, requestID string) {
	t.Helper()
	select {
	case got := <-a.cancels:
		if got != requestID {
			t.Fatalf("cancelled %q, want %q", got, requestID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not receive a cancel")
	}
}

func logLine(text string, stream messages.LogStream) *messages.LogLine {
	return &messages.LogLine{
		Timestamp: timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)),
		Stream:    stream,
		Text:      text,
	}
}

func setupStreamTest(t *testing.T) (models.Application, *streamTestAgent) {
	t.Helper()
	app := setupContainerTest(t)
	streamTestUserID = "stream-user-" + uuid.NewString()
	if err := gorm.G[models.User](db.DB).Create(t.Context(), &models.User{Id: streamTestUserID, Name: "Stream User", Email: "stream@example.com"}); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := gorm.G[models.UserRole](db.DB).Create(t.Context(), &models.UserRole{UserId: streamTestUserID, RoleId: rbac.AdminRoleID, Source: models.UserRoleSourceManual}); err != nil {
		t.Fatalf("failed to assign role: %v", err)
	}

	original := sseKeepAliveInterval
	t.Cleanup(func() { sseKeepAliveInterval = original })
	sseKeepAliveInterval = time.Hour

	return app, withStreamAgent(t, app.AgentId)
}

func streamRouter(perms *rbac.Permissions) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		auth.SetClaims(c, &auth.UserClaims{Subject: streamTestUserID, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))})
		auth.SetUser(c, &models.User{Id: streamTestUserID, Name: "Stream User", Email: "stream@example.com"})
		rbac.Set(c, perms)
		c.Next()
	})
	app := func(action models.PermissionAction) gin.HandlerFunc {
		return middleware.RequirePermission(models.ResourceApplication, action)
	}
	router.GET("/applications/:id/containers/:containerId/logs/stream", app(models.ActionInspect), ContainerLogsStreamHandler)
	router.GET("/applications/:id/containers/:containerId/stats/stream", app(models.ActionRead), ContainerStatsStreamHandler)
	router.GET("/applications/:id/containers/:containerId/logs/download", app(models.ActionInspect), ContainerLogsDownloadHandler)
	return router
}

func streamURL(server *httptest.Server, app models.Application, suffix string) string {
	return server.URL + "/applications/" + app.Id + "/containers/" + testContainerID + "/" + suffix
}

// streamResponse leaves the closing of the body to openStream.
type streamResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.Reader
}

func openStream(t *testing.T, server *httptest.Server, url string) (streamResponse, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("failed to create request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return streamResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, cancel
}

type sseEvent struct {
	Name string
	Data string
}

// readEvent returns the next event and skips comments.
func readEvent(t *testing.T, scanner *bufio.Scanner) sseEvent {
	t.Helper()
	var event sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			event.Data = strings.TrimPrefix(line, "data: ")
		case line == "" && event.Name != "":
			return event
		}
	}
	t.Fatal("stream ended before the next event")
	return event
}

func newStreamServer(t *testing.T, perms *rbac.Permissions) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(streamRouter(perms))
	t.Cleanup(server.Close)
	return server
}

func TestContainerLogsStream_EmitsLinesAndEnd(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q", got)
	}
	scanner := bufio.NewScanner(resp.Body)

	req := agent.nextLogs(t)
	agent.sendLogs(req.RequestId, 3, logLine("hello", messages.LogStream_LOG_STREAM_STDOUT), logLine("boom", messages.LogStream_LOG_STREAM_STDERR))

	event := readEvent(t, scanner)
	if event.Name != "log" {
		t.Fatalf("expected log event, got %q", event.Name)
	}
	want := `{"lines":[{"timestamp":"2026-01-02T03:04:05.123456789Z","stream":"stdout","text":"hello","truncated":false},{"timestamp":"2026-01-02T03:04:05.123456789Z","stream":"stderr","text":"boom","truncated":false}],"droppedLines":3}`
	if event.Data != want {
		t.Errorf("log data = %s\nwant       %s", event.Data, want)
	}

	agent.end(req.RequestId, messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE)
	event = readEvent(t, scanner)
	if event.Name != "end" || event.Data != `{"reason":"container_gone","error":""}` {
		t.Errorf("end event = %+v", event)
	}
	if scanner.Scan() {
		t.Errorf("expected the response to end, got %q", scanner.Text())
	}
}

func TestContainerLogsStream_RequestOptions(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	tests := []struct {
		name       string
		query      string
		wantTail   int32
		wantSince  int64
		wantFollow bool
	}{
		{"defaults", "", defaultLogTail, 0, true},
		{"explicit", "?tail=50&since=2026-01-02T03:04:05.123456789Z", 50, time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC).UnixMilli(), true},
		{"tail is clamped", "?tail=100000", maxLiveLogTail, 0, true},
		{"all lines are clamped", "?tail=-1", maxLiveLogTail, 0, true},
		{"no existing lines", "?tail=0", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream")+tt.query)
			defer cancel()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
			req := agent.nextLogs(t)
			if req.Tail != tt.wantTail || req.SinceUnixMs != tt.wantSince || req.Follow != tt.wantFollow {
				t.Errorf("request = tail %d since %d follow %v, want tail %d since %d follow %v",
					req.Tail, req.SinceUnixMs, req.Follow, tt.wantTail, tt.wantSince, tt.wantFollow)
			}
			if req.ApplicationId != app.Id || req.ContainerId != testContainerID {
				t.Errorf("request targets %s/%s", req.ApplicationId, req.ContainerId)
			}
			cancel()
			agent.expectCancel(t, req.RequestId)
		})
	}
}

func TestContainerLogsStream_ClientDisconnectCancelsAgentStream(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	_, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	req := agent.nextLogs(t)

	cancel()
	agent.expectCancel(t, req.RequestId)
}

func TestContainerLogsStream_RejectedBeforeStreaming(t *testing.T) {
	tests := []struct {
		name  string
		query string
		id    string
		setup func(*streamTestAgent)
		want  int
	}{
		{"invalid container id", "", "nope", nil, http.StatusBadRequest},
		{"invalid tail", "?tail=abc", testContainerID, nil, http.StatusBadRequest},
		{"invalid since", "?since=yesterday", testContainerID, nil, http.StatusBadRequest},
		{"invalid follow", "?follow=maybe", testContainerID, nil, http.StatusBadRequest},
		{"container not found", "", testContainerID, func(a *streamTestAgent) { a.rejectWith(messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND) }, http.StatusNotFound},
		{"agent limit", "", testContainerID, func(a *streamTestAgent) { a.rejectWith(messages.StreamEndReason_STREAM_END_REASON_LIMIT) }, http.StatusTooManyRequests},
		{"agent error", "", testContainerID, func(a *streamTestAgent) { a.rejectWith(messages.StreamEndReason_STREAM_END_REASON_ERROR) }, http.StatusBadGateway},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, agent := setupStreamTest(t)
			if tt.setup != nil {
				tt.setup(agent)
			}
			server := newStreamServer(t, rbac.Admin())

			resp, cancel := openStream(t, server, server.URL+"/applications/"+app.Id+"/containers/"+tt.id+"/logs/stream"+tt.query)
			defer cancel()
			if resp.StatusCode != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("expected a JSON error, got Content-Type %q", ct)
			}
		})
	}
}

func TestContainerLogsStream_AgentOfflineOrUnsupported(t *testing.T) {
	app := setupContainerTest(t)
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	cancel()
	if resp.StatusCode != http.StatusInternalServerError {
		// The hub is not initialized in this setup.
		t.Fatalf("expected 500 without a hub, got %d", resp.StatusCode)
	}

	nop := zerolog.Nop()
	websocket.DefaultHub = websocket.NewHub(&nop)
	t.Cleanup(func() { websocket.DefaultHub = nil })

	resp, cancel = openStream(t, server, streamURL(server, app, "logs/stream"))
	cancel()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for an offline agent, got %d", resp.StatusCode)
	}

	client, err := websocket.DefaultHub.Register(app.AgentId, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	resp, cancel = openStream(t, server, streamURL(server, app, "logs/stream"))
	cancel()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 without the capability, got %d", resp.StatusCode)
	}
}

func TestContainerLogsStream_PerUserLimit(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	for range maxContainerStreamsPerUser {
		resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
		defer cancel()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 within the limit, got %d", resp.StatusCode)
		}
		agent.nextLogs(t)
	}

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 beyond the limit, got %d", resp.StatusCode)
	}

	resp, cancel = openStream(t, server, streamURL(server, app, "stats/stream"))
	defer cancel()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected the limit to cover stats streams as well, got %d", resp.StatusCode)
	}
}

func TestContainerLogsStream_ReleasesSlotWhenClosed(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	for range maxContainerStreamsPerUser * 2 {
		_, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
		req := agent.nextLogs(t)
		cancel()
		agent.expectCancel(t, req.RequestId)
	}
}

func TestContainerLogsStream_KeepAliveRefreshesLease(t *testing.T) {
	app, agent := setupStreamTest(t)
	sseKeepAliveInterval = 20 * time.Millisecond
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)
	req := agent.nextLogs(t)

	select {
	case id := <-agent.keepAlives:
		if id != req.RequestId {
			t.Fatalf("lease refreshed for %q, want %q", id, req.RequestId)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent lease was not refreshed")
	}
	for scanner.Scan() {
		if scanner.Text() == ": ping" {
			return
		}
	}
	t.Fatal("expected a keep-alive ping")
}

func TestContainerLogsStream_EndsWhenUserDeleted(t *testing.T) {
	app, agent := setupStreamTest(t)
	sseKeepAliveInterval = 20 * time.Millisecond
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)
	req := agent.nextLogs(t)

	if _, err := gorm.G[models.User](db.DB).Where("id = ?", streamTestUserID).Delete(t.Context()); err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}

	if event := readEvent(t, scanner); event.Name != "unauthorized" {
		t.Fatalf("expected unauthorized event, got %+v", event)
	}
	agent.expectCancel(t, req.RequestId)
}

func TestContainerLogsStream_EndsWhenPermissionIsRemoved(t *testing.T) {
	app, agent := setupStreamTest(t)
	sseKeepAliveInterval = 20 * time.Millisecond
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)
	req := agent.nextLogs(t)

	if _, err := gorm.G[models.UserRole](db.DB).Where("user_id = ?", streamTestUserID).Delete(t.Context()); err != nil {
		t.Fatalf("failed to remove role: %v", err)
	}

	if event := readEvent(t, scanner); event.Name != "unauthorized" {
		t.Fatalf("expected unauthorized event, got %+v", event)
	}
	agent.expectCancel(t, req.RequestId)
}

func TestContainerLogsStream_EndsAtMaxDuration(t *testing.T) {
	app, agent := setupStreamTest(t)
	original := containerStreamMaxDuration
	containerStreamMaxDuration = 50 * time.Millisecond
	t.Cleanup(func() { containerStreamMaxDuration = original })
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)
	req := agent.nextLogs(t)

	if event := readEvent(t, scanner); event.Name != "end" || event.Data != `{"reason":"limit","error":""}` {
		t.Fatalf("expected end event with reason limit, got %+v", event)
	}
	agent.expectCancel(t, req.RequestId)
}

func TestContainerLogsStream_EndsWhenAgentDisconnects(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	scanner := bufio.NewScanner(resp.Body)
	agent.nextLogs(t)

	agent.hub.FinishDisconnect(agent.client)

	if event := readEvent(t, scanner); event.Name != "end" || event.Data != `{"reason":"agent_offline","error":""}` {
		t.Fatalf("expected end event with reason agent_offline, got %+v", event)
	}
}

func TestContainerStatsStream_EmitsSamples(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionRead)}))

	resp, cancel := openStream(t, server, streamURL(server, app, "stats/stream"))
	defer cancel()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	req := agent.nextStats(t)
	if req.ApplicationId != app.Id || req.ContainerId != testContainerID {
		t.Errorf("request targets %s/%s", req.ApplicationId, req.ContainerId)
	}

	agent.sendStats(req.RequestId, &messages.ContainerStatsSample{
		TimestampMs:   1700000000000,
		CpuPercent:    12.5,
		MemUsageBytes: 1024,
		MemLimitBytes: 4096,
		NetRxBytes:    10,
		NetTxBytes:    20,
		Pids:          3,
		OnlineCpus:    4,
	})
	event := readEvent(t, scanner)
	want := `{"timestampMs":1700000000000,"cpuPercent":12.5,"memUsageBytes":1024,"memLimitBytes":4096,"netRxBytes":10,"netTxBytes":20,"pids":3,"onlineCpus":4}`
	if event.Name != "stats" || event.Data != want {
		t.Errorf("stats event = %+v\nwant data %s", event, want)
	}

	agent.end(req.RequestId, messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE)
	if event := readEvent(t, scanner); event.Name != "end" || event.Data != `{"reason":"container_gone","error":""}` {
		t.Errorf("end event = %+v", event)
	}
}

func TestContainerStreams_Permissions(t *testing.T) {
	app, agent := setupStreamTest(t)

	read := rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionRead)})
	inspect := rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionInspect)})

	tests := []struct {
		name   string
		perms  *rbac.Permissions
		suffix string
		want   int
	}{
		{"logs with read", read, "logs/stream", http.StatusForbidden},
		{"download with read", read, "logs/download", http.StatusForbidden},
		{"stats with read", read, "stats/stream", http.StatusOK},
		{"logs with inspect", inspect, "logs/stream", http.StatusOK},
		{"download with inspect", inspect, "logs/download", http.StatusOK},
		{"stats without permissions", rbac.New(nil), "stats/stream", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newStreamServer(t, tt.perms)
			resp, cancel := openStream(t, server, streamURL(server, app, tt.suffix))
			defer cancel()
			if resp.StatusCode != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, resp.StatusCode)
			}
			if tt.want == http.StatusOK {
				select {
				case req := <-agent.logReqs:
					cancel()
					agent.expectCancel(t, req.RequestId)
				case req := <-agent.statsReqs:
					cancel()
					agent.expectCancel(t, req.RequestId)
				case <-time.After(2 * time.Second):
					t.Fatal("agent did not receive a request")
				}
			}
		})
	}
}

func TestContainerStreams_HandlersCheckPermissionThemselves(t *testing.T) {
	app, agent := setupStreamTest(t)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		auth.SetClaims(c, &auth.UserClaims{Subject: streamTestUserID, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))})
		auth.SetUser(c, &models.User{Id: streamTestUserID, Name: "Stream User", Email: "stream@example.com"})
		rbac.Set(c, rbac.New([]models.RolePermission{grantAll(models.ResourceApplication, models.ActionRead)}))
		c.Next()
	})
	router.GET("/applications/:id/containers/:containerId/logs/stream", ContainerLogsStreamHandler)
	router.GET("/applications/:id/containers/:containerId/logs/download", ContainerLogsDownloadHandler)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	for _, suffix := range []string{"logs/stream", "logs/download"} {
		resp, cancel := openStream(t, server, streamURL(server, app, suffix))
		cancel()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: expected 403 without the inspect permission, got %d", suffix, resp.StatusCode)
		}
	}
	if len(agent.logReqs) != 0 {
		t.Error("a forbidden request must not reach the agent")
	}
}

func TestAttachmentDisposition_IsSafeForAnyFilename(t *testing.T) {
	for _, name := range []string{
		"container-0123456789ab.log",
		`quote".log`,
		"back\\slash.log",
		"line\r\nSet-Cookie: x=1.log",
		"ümläut.log",
		"semi;colon=.log",
	} {
		header := attachmentDisposition(name)
		if strings.ContainsAny(header, "\r\n") {
			t.Errorf("%q: header contains a line break: %q", name, header)
		}
		mediaType, params, err := mime.ParseMediaType(header)
		if err != nil {
			t.Errorf("%q: header %q does not parse: %v", name, header, err)
			continue
		}
		if mediaType != "attachment" || params["filename"] != name || len(params) != 1 {
			t.Errorf("%q: header %q parsed as %s %v", name, header, mediaType, params)
		}
	}
}

func TestContainerLogsDownload_StreamsText(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
	defer cancel()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, ".log") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	req := agent.nextLogs(t)
	if req.Follow || req.Tail != -1 || req.MaxBytes != maxLogDownloadBytes {
		t.Errorf("request = follow %v tail %d max %d", req.Follow, req.Tail, req.MaxBytes)
	}
	agent.sendLogs(req.RequestId, 0, logLine("one", messages.LogStream_LOG_STREAM_STDOUT))
	agent.sendLogs(req.RequestId, 0, logLine("two", messages.LogStream_LOG_STREAM_STDERR))
	agent.end(req.RequestId, messages.StreamEndReason_STREAM_END_REASON_COMPLETED)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read the download: %v", err)
	}
	want := "2026-01-02T03:04:05.123456789Z one\n2026-01-02T03:04:05.123456789Z two\n"
	if string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestContainerLogsDownload_MarksIncompleteDownloads(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
	defer cancel()
	req := agent.nextLogs(t)
	agent.sendLogs(req.RequestId, 0, logLine("one", messages.LogStream_LOG_STREAM_STDOUT))
	agent.end(req.RequestId, messages.StreamEndReason_STREAM_END_REASON_LIMIT)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read the download: %v", err)
	}
	want := "2026-01-02T03:04:05.123456789Z one\n# download incomplete: limit\n"
	if string(body) != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestContainerLogsDownload_ClientDisconnectCancelsAgentStream(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	_, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
	req := agent.nextLogs(t)

	cancel()
	agent.expectCancel(t, req.RequestId)
}

func TestContainerLogsDownload_RefreshesLease(t *testing.T) {
	app, agent := setupStreamTest(t)
	sseKeepAliveInterval = 20 * time.Millisecond
	server := newStreamServer(t, rbac.Admin())

	_, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
	defer cancel()
	req := agent.nextLogs(t)

	select {
	case id := <-agent.keepAlives:
		if id != req.RequestId {
			t.Fatalf("lease refreshed for %q, want %q", id, req.RequestId)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent lease was not refreshed")
	}
}

func TestContainerLogsDownload_ConcurrentLimitPerAgent(t *testing.T) {
	app, agent := setupStreamTest(t)
	server := newStreamServer(t, rbac.Admin())

	for range maxLogDownloadsPerAgent {
		_, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
		defer cancel()
		agent.nextLogs(t)
	}

	resp, cancel := openStream(t, server, streamURL(server, app, "logs/download"))
	defer cancel()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 beyond the download limit, got %d", resp.StatusCode)
	}
	if len(agent.logReqs) != 0 {
		t.Error("the rejected download must not reach the agent")
	}

	live, cancel := openStream(t, server, streamURL(server, app, "logs/stream"))
	defer cancel()
	if live.StatusCode != http.StatusOK {
		t.Fatalf("downloads must not block live streams, got %d", live.StatusCode)
	}
}

func TestContainerLogsDownload_Errors(t *testing.T) {
	tests := []struct {
		name  string
		query string
		setup func(*streamTestAgent)
		want  int
	}{
		{"invalid tail", "?tail=abc", nil, http.StatusBadRequest},
		{"container not found", "", func(a *streamTestAgent) { a.rejectWith(messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND) }, http.StatusNotFound},
		{"agent limit", "", func(a *streamTestAgent) { a.rejectWith(messages.StreamEndReason_STREAM_END_REASON_LIMIT) }, http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, agent := setupStreamTest(t)
			if tt.setup != nil {
				tt.setup(agent)
			}
			server := newStreamServer(t, rbac.Admin())

			resp, cancel := openStream(t, server, streamURL(server, app, "logs/download")+tt.query)
			defer cancel()
			if resp.StatusCode != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, resp.StatusCode)
			}
			if cd := resp.Header.Get("Content-Disposition"); cd != "" {
				t.Errorf("an error must not be sent as attachment, got %q", cd)
			}
		})
	}
}
