package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/websocket"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"
)

const (
	maxContainerStreamsPerUser = 6
	maxLogDownloadsPerAgent    = 2
	maxLogDownloadBytes        = 64 << 20 // 64 MiB
	defaultLogTail             = 1000
	maxLiveLogTail             = 5000
)

var containerStreamMaxDuration = 2 * time.Hour

var (
	containerStreamSlots = newSlotCounter(maxContainerStreamsPerUser)
	logDownloadSlots     = newSlotCounter(maxLogDownloadsPerAgent)
)

type slotCounter struct {
	mu     sync.Mutex
	limit  int
	counts map[string]int
}

func newSlotCounter(limit int) *slotCounter {
	return &slotCounter{limit: limit, counts: make(map[string]int)}
}

func (s *slotCounter) acquire(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts[key] >= s.limit {
		return false
	}
	s.counts[key]++
	return true
}

func (s *slotCounter) release(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts[key] <= 1 {
		delete(s.counts, key)
		return
	}
	s.counts[key]--
}

type logLineEvent struct {
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

type logsEvent struct {
	Lines        []logLineEvent `json:"lines"`
	DroppedLines int64          `json:"droppedLines"`
}

type statsEvent struct {
	TimestampMs   int64   `json:"timestampMs"`
	CpuPercent    float64 `json:"cpuPercent"`
	MemUsageBytes uint64  `json:"memUsageBytes"`
	MemLimitBytes uint64  `json:"memLimitBytes"`
	NetRxBytes    uint64  `json:"netRxBytes"`
	NetTxBytes    uint64  `json:"netTxBytes"`
	Pids          uint64  `json:"pids"`
	OnlineCpus    uint32  `json:"onlineCpus"`
}

type streamEndEvent struct {
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

type containerStreamSpec struct {
	action models.PermissionAction
	// download marks a read of the whole log, which only a few can run at once per agent.
	download bool
	open     func(ctx context.Context, application models.Application, containerID string) (*websocket.Stream, error)
}

func ContainerLogsStreamHandler(c *gin.Context) {
	opts, ok := parseLogsQuery(c, true)
	if !ok {
		return
	}
	serveContainerEvents(c, containerStreamSpec{
		action:   models.ActionInspect,
		download: !opts.Follow,
		open:     openLogs(opts),
	})
}

func ContainerStatsStreamHandler(c *gin.Context) {
	serveContainerEvents(c, containerStreamSpec{
		action: models.ActionRead,
		open: func(ctx context.Context, application models.Application, containerID string) (*websocket.Stream, error) {
			return websocket.DefaultHub.OpenStats(ctx, application.AgentId, application.Id, containerID)
		},
	})
}

func ContainerLogsDownloadHandler(c *gin.Context) {
	opts, ok := parseLogsQuery(c, false)
	if !ok {
		return
	}
	stream, _, release, ok := beginContainerStream(c, containerStreamSpec{
		action:   models.ActionInspect,
		download: true,
		open:     openLogs(opts),
	})
	if !ok {
		return
	}
	defer stream.Close()
	defer release()

	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", attachmentDisposition("container-"+c.Param("containerId")[:12]+".log"))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)
	c.Writer.Flush()

	writeLogDownload(c, stream)
}

func attachmentDisposition(filename string) string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": filename})
}

func writeLogDownload(c *gin.Context, stream *websocket.Stream) {
	lease := time.NewTicker(sseKeepAliveInterval)
	defer lease.Stop()
	limit := time.NewTimer(containerStreamMaxDuration)
	defer limit.Stop()

	incomplete := func(reason string) {
		_, _ = fmt.Fprintf(c.Writer, "# download incomplete: %s\n", reason)
		c.Writer.Flush()
	}

	for {
		select {
		case frame, open := <-stream.Frames():
			if !open {
				incomplete(endReasonOfClosedStream(stream))
				return
			}
			if frame.End != nil {
				if frame.End.GetReason() != messages.StreamEndReason_STREAM_END_REASON_COMPLETED {
					incomplete(streamEndReason(frame.End.GetReason()))
				}
				return
			}
			if frame.Logs == nil {
				continue
			}
			var out strings.Builder
			for _, line := range toLogLineEvents(frame.Logs) {
				out.WriteString(line.Timestamp)
				out.WriteByte(' ')
				out.WriteString(line.Text)
				out.WriteByte('\n')
			}
			if _, err := c.Writer.WriteString(out.String()); err != nil {
				return
			}
			c.Writer.Flush()
		case <-lease.C:
			stream.Refresh()
		case <-limit.C:
			incomplete("limit")
			return
		case <-c.Request.Context().Done():
			return
		}
	}
}

func openLogs(opts websocket.LogsOptions) func(ctx context.Context, application models.Application, containerID string) (*websocket.Stream, error) {
	return func(ctx context.Context, application models.Application, containerID string) (*websocket.Stream, error) {
		return websocket.DefaultHub.OpenLogs(ctx, application.AgentId, application.Id, containerID, opts)
	}
}

// parseLogsQuery reads tail, since and follow. Only the download may read the whole log.
func parseLogsQuery(c *gin.Context, live bool) (websocket.LogsOptions, bool) {
	opts := websocket.LogsOptions{Tail: defaultLogTail, Follow: live}
	if !live {
		opts.Tail = -1
	}
	badRequest := func(message string) (websocket.LogsOptions, bool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return opts, false
	}

	if raw := c.Query("tail"); raw != "" {
		tail, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return badRequest("invalid tail")
		}
		opts.Tail = int32(tail)
	}
	if raw := c.Query("since"); raw != "" {
		since, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return badRequest("invalid since")
		}
		opts.SinceUnixMs = since.UnixMilli()
	}
	if raw := c.Query("follow"); live && raw != "" {
		follow, err := strconv.ParseBool(raw)
		if err != nil {
			return badRequest("invalid follow")
		}
		opts.Follow = follow
	}

	switch {
	case !live:
		opts.Tail = max(opts.Tail, -1)
		opts.MaxBytes = maxLogDownloadBytes
	case opts.Tail < 0 || opts.Tail > maxLiveLogTail:
		opts.Tail = maxLiveLogTail
	}
	if live && !opts.Follow {
		opts.MaxBytes = maxLogDownloadBytes
	}
	return opts, true
}

// beginContainerStream does everything that can still be answered with a JSON
// error. The caller must call release before closing the stream.
func beginContainerStream(c *gin.Context, spec containerStreamSpec) (*websocket.Stream, models.Application, func(), bool) {
	var none models.Application

	if !containerIDPattern.MatchString(c.Param("containerId")) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid container id"})
		return nil, none, nil, false
	}
	user, ok := auth.GetUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return nil, none, nil, false
	}

	perms, ok := currentPermissions(c)
	if !ok {
		return nil, none, nil, false
	}
	if !perms.Can(models.ResourceApplication, spec.action, c.Param("id")) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return nil, none, nil, false
	}

	if !containerStreamSlots.acquire(user.Id) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many open container streams"})
		return nil, none, nil, false
	}
	releaseUser := func() { containerStreamSlots.release(user.Id) }

	application, err := loadContainerTarget(c.Request.Context(), c.Param("id"))
	if err != nil {
		releaseUser()
		respondContainerError(c, err)
		return nil, none, nil, false
	}

	release := releaseUser
	if spec.download {
		if !logDownloadSlots.acquire(application.AgentId) {
			releaseUser()
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many log downloads are running on this agent, please try again later"})
			return nil, none, nil, false
		}
		release = func() {
			logDownloadSlots.release(application.AgentId)
			releaseUser()
		}
	}

	stream, err := spec.open(c.Request.Context(), application, c.Param("containerId"))
	if err != nil {
		release()
		respondStreamOpenError(c, application, err)
		return nil, none, nil, false
	}

	return stream, application, release, true
}

func respondStreamOpenError(c *gin.Context, application models.Application, err error) {
	switch {
	case errors.Is(err, websocket.ErrContainerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "container not found"})
	case errors.Is(err, websocket.ErrStreamLimit):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many open streams on this agent, please try again later"})
	case errors.Is(err, websocket.ErrStreamFailed):
		applicationContainersLog.Warn().Err(err).Str("agent_id", application.AgentId).Msg("agent rejected container stream")
		c.JSON(http.StatusBadGateway, gin.H{"error": "The agent could not open the stream."})
	default:
		respondContainerError(c, containerTransportError(application, err))
	}
}

func respondContainerError(c *gin.Context, err error) {
	if statusErr, ok := errors.AsType[huma.StatusError](err); ok {
		c.JSON(statusErr.GetStatus(), gin.H{"error": statusErr.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}

type sseStream struct {
	c *gin.Context
}

func (s sseStream) event(name string, payload any) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(s.c.Writer, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return false
	}
	s.c.Writer.Flush()
	return true
}

func serveContainerEvents(c *gin.Context, spec containerStreamSpec) {
	claims, ok := auth.GetClaims(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
		return
	}
	perms, ok := currentPermissions(c)
	if !ok {
		return
	}

	stream, application, release, ok := beginContainerStream(c, spec)
	if !ok {
		return
	}
	defer stream.Close()
	defer release()

	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	if _, err := fmt.Fprint(c.Writer, ": connected\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	out := sseStream{c}
	expiry := time.NewTimer(time.Until(claims.ExpiresAt.Time))
	defer expiry.Stop()
	limit := time.NewTimer(containerStreamMaxDuration)
	defer limit.Stop()
	keepAlive := time.NewTicker(sseKeepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case frame, open := <-stream.Frames():
			if !open {
				out.event("end", streamEndEvent{Reason: endReasonOfClosedStream(stream)})
				return
			}
			if !out.frame(frame) || frame.End != nil {
				return
			}
		case <-keepAlive.C:
			stream.Refresh()
			if sessionRevoked(c.Request.Context(), claims) {
				writeUnauthorizedEvent(c)
				return
			}
			perms = reloadPermissions(c.Request.Context(), claims.Subject, perms)
			if !perms.Can(models.ResourceApplication, spec.action, application.Id) {
				writeUnauthorizedEvent(c)
				return
			}
			if _, err := fmt.Fprint(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
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

func (s sseStream) frame(frame websocket.Frame) bool {
	switch {
	case frame.Logs != nil:
		return s.event("log", logsEvent{Lines: toLogLineEvents(frame.Logs), DroppedLines: frame.Logs.GetDroppedLines()})
	case frame.Stats != nil:
		return s.event("stats", toStatsEvent(frame.Stats))
	case frame.End != nil:
		return s.event("end", streamEndEvent{Reason: streamEndReason(frame.End.GetReason()), Error: frame.End.GetErrorMessage()})
	}
	return true
}

func toLogLineEvents(chunk *messages.ContainerLogsChunk) []logLineEvent {
	lines := make([]logLineEvent, 0, len(chunk.GetLines()))
	for _, line := range chunk.GetLines() {
		stream := "stdout"
		if line.GetStream() == messages.LogStream_LOG_STREAM_STDERR {
			stream = "stderr"
		}
		lines = append(lines, logLineEvent{
			Timestamp: line.GetTimestamp().AsTime().UTC().Format(time.RFC3339Nano),
			Stream:    stream,
			Text:      line.GetText(),
			Truncated: line.GetTruncated(),
		})
	}
	return lines
}

func toStatsEvent(sample *messages.ContainerStatsSample) statsEvent {
	cpu := sample.GetCpuPercent()
	if math.IsNaN(cpu) || math.IsInf(cpu, 0) {
		cpu = 0
	}
	return statsEvent{
		TimestampMs:   sample.GetTimestampMs(),
		CpuPercent:    cpu,
		MemUsageBytes: sample.GetMemUsageBytes(),
		MemLimitBytes: sample.GetMemLimitBytes(),
		NetRxBytes:    sample.GetNetRxBytes(),
		NetTxBytes:    sample.GetNetTxBytes(),
		Pids:          sample.GetPids(),
		OnlineCpus:    sample.GetOnlineCpus(),
	}
}

func streamEndReason(reason messages.StreamEndReason) string {
	switch reason {
	case messages.StreamEndReason_STREAM_END_REASON_COMPLETED:
		return "completed"
	case messages.StreamEndReason_STREAM_END_REASON_CANCELLED:
		return "cancelled"
	case messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE:
		return "container_gone"
	case messages.StreamEndReason_STREAM_END_REASON_LEASE_EXPIRED:
		return "lease_expired"
	case messages.StreamEndReason_STREAM_END_REASON_LIMIT:
		return "limit"
	case messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND:
		return "not_found"
	default:
		return "error"
	}
}

func endReasonOfClosedStream(stream *websocket.Stream) string {
	switch err := stream.Err(); {
	case errors.Is(err, websocket.ErrSlowConsumer):
		return "slow_consumer"
	case errors.Is(err, websocket.ErrAgentOffline):
		return "agent_offline"
	case err != nil:
		return "error"
	default:
		return "cancelled"
	}
}
