package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	agentdocker "github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ containerStreamer = (*agentdocker.Client)(nil)

type logsFunc func(ctx context.Context, req agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error
type statsFunc func(ctx context.Context, appID, containerID string, started func(), emit func(*messages.ContainerStatsSample) error) error

type stubStreamer struct {
	stubDeployer

	logs  logsFunc
	stats statsFunc
}

func (s *stubStreamer) StreamLogs(ctx context.Context, req agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
	return s.logs(ctx, req, started, emit)
}

func (s *stubStreamer) StreamStats(ctx context.Context, appID, containerID string, started func(), emit func(*messages.ContainerStatsSample) error) error {
	return s.stats(ctx, appID, containerID, started, emit)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestStreamManager() (*streamManager, *fakeClock) {
	clock := &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	m := newStreamManager()
	m.now = clock.Now
	m.flushInterval = 5 * time.Millisecond
	return m, clock
}

func newStreamSender() *stubSender {
	return &stubSender{sent: make(chan *messages.ClientMessage, 512)}
}

func logRequest(id string) *messages.ContainerLogsRequest {
	return &messages.ContainerLogsRequest{RequestId: id, ApplicationId: "app-1", ContainerId: "c1", Tail: 100}
}

func statsRequest(id string) *messages.ContainerStatsRequest {
	return &messages.ContainerStatsRequest{RequestId: id, ApplicationId: "app-1", ContainerId: "c1"}
}

func logLine(text string) *messages.LogLine {
	return &messages.LogLine{Timestamp: timestamppb.Now(), Stream: messages.LogStream_LOG_STREAM_STDOUT, Text: text}
}

// nextStreamMessage returns the next message and fails unless it is of the wanted kind.
func nextStreamMessage(t *testing.T, sender *stubSender) *messages.ClientMessage {
	t.Helper()
	return receiveMessage(t, sender)
}

func expectStarted(t *testing.T, sender *stubSender, id string) {
	t.Helper()
	started := nextStreamMessage(t, sender).GetStreamStarted()
	if started == nil || started.RequestId != id {
		t.Fatalf("expected StreamStarted for %q, got %v", id, started)
	}
}

func expectEnd(t *testing.T, sender *stubSender, id string, reason messages.StreamEndReason) *messages.StreamEnd {
	t.Helper()
	end := nextStreamMessage(t, sender).GetStreamEnd()
	if end == nil {
		t.Fatal("expected StreamEnd")
	}
	if end.RequestId != id || end.Reason != reason {
		t.Fatalf("StreamEnd = %v, want request %q with reason %v", end, id, reason)
	}
	return end
}

// readLogs collects lines until the stream ends and returns the end message.
func readLogs(t *testing.T, sender *stubSender, id string) (lines []*messages.LogLine, chunks []*messages.ContainerLogsChunk, end *messages.StreamEnd) {
	t.Helper()
	for {
		msg := nextStreamMessage(t, sender)
		if chunk := msg.GetContainerLogsChunk(); chunk != nil {
			if chunk.RequestId != id {
				t.Fatalf("chunk for request %q, want %q", chunk.RequestId, id)
			}
			chunks = append(chunks, chunk)
			lines = append(lines, chunk.Lines...)
			continue
		}
		if end = msg.GetStreamEnd(); end != nil {
			return lines, chunks, end
		}
		t.Fatalf("unexpected message %T", msg.Payload)
	}
}

func waitForStreamCount(t *testing.T, m *streamManager, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for m.count() != want {
		if time.Now().After(deadline) {
			t.Fatalf("stream count = %d, want %d", m.count(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func blockUntilDone(ctx context.Context, started func()) {
	started()
	<-ctx.Done()
}

func TestStreamManager_LogsAreStartedBatchedAndEnded(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for _, text := range []string{"a", "b", "c"} {
			if err := emit(logLine(text)); err != nil {
				return err
			}
		}
		return nil
	}}

	m.StartLogs(t.Context(), sender, streamer, logRequest("r1"))

	expectStarted(t, sender, "r1")
	lines, _, end := readLogs(t, sender, "r1")
	if end.Reason != messages.StreamEndReason_STREAM_END_REASON_COMPLETED || end.ErrorMessage != "" {
		t.Errorf("end = %v", end)
	}
	if len(lines) != 3 || lines[0].Text != "a" || lines[1].Text != "b" || lines[2].Text != "c" {
		t.Errorf("lines = %v", lines)
	}
	waitForStreamCount(t, m, 0)
}

func TestStreamManager_FollowEndsWithContainerGone(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		started()
		return nil
	}}
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE)
}

func TestStreamManager_FlushesPeriodicallyWhileRunning(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		if err := emit(logLine("live")); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	chunk := nextStreamMessage(t, sender).GetContainerLogsChunk()
	if chunk == nil || len(chunk.Lines) != 1 || chunk.Lines[0].Text != "live" {
		t.Fatalf("chunk = %v", chunk)
	}

	m.Cancel("r1")
	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_CANCELLED)
	waitForStreamCount(t, m, 0)
}

func TestStreamManager_ChunksStayBelowTheLimit(t *testing.T) {
	m, _ := newTestStreamManager()
	m.flushInterval = time.Hour
	sender := newStreamSender()
	text := strings.Repeat("x", 10<<10)
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for i := range 40 {
			if err := emit(logLine(fmt.Sprintf("%02d%s", i, text))); err != nil {
				return err
			}
		}
		return nil
	}}

	m.StartLogs(t.Context(), sender, streamer, logRequest("r1"))

	expectStarted(t, sender, "r1")
	lines, chunks, _ := readLogs(t, sender, "r1")
	if len(lines) != 40 {
		t.Fatalf("got %d lines, want 40", len(lines))
	}
	for i, line := range lines {
		if !strings.HasPrefix(line.Text, fmt.Sprintf("%02d", i)) {
			t.Fatalf("line %d out of order: %.2s", i, line.Text)
		}
	}
	if len(chunks) < 3 {
		t.Errorf("expected the lines to be split into several chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		size := 0
		for _, line := range chunk.Lines {
			size += len(line.Text)
		}
		if size > maxLogChunkBytes {
			t.Errorf("chunk of %d bytes exceeds %d", size, maxLogChunkBytes)
		}
	}
}

func TestStreamManager_FollowDropsLinesOverTheByteBudget(t *testing.T) {
	m, _ := newTestStreamManager()
	m.logBurstBytes = 2000
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 50 {
			if err := emit(logLine(strings.Repeat("x", 100))); err != nil {
				return err
			}
		}
		return nil
	}}
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	lines, chunks, _ := readLogs(t, sender, "r1")
	var dropped int64
	for _, chunk := range chunks {
		dropped += chunk.DroppedLines
	}
	if dropped == 0 || len(lines) == 0 {
		t.Fatalf("lines = %d, dropped = %d", len(lines), dropped)
	}
	if int64(len(lines))+dropped != 50 {
		t.Errorf("every line must be delivered or counted: %d + %d != 50", len(lines), dropped)
	}
}

func TestStreamManager_FollowBudgetRefillsOverTime(t *testing.T) {
	m, clock := newTestStreamManager()
	m.logBurstBytes = 1000
	m.logBytesPerSecond = 1000
	sender := newStreamSender()
	burstDone := make(chan struct{})
	proceed := make(chan struct{})
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 20 {
			if err := emit(logLine(strings.Repeat("x", 100))); err != nil {
				return err
			}
		}
		close(burstDone)
		<-proceed
		for range 3 {
			if err := emit(logLine(strings.Repeat("y", 100))); err != nil {
				return err
			}
		}
		return nil
	}}
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(t.Context(), sender, streamer, req)
	expectStarted(t, sender, "r1")
	<-burstDone
	clock.Advance(10 * time.Second)
	close(proceed)

	lines, _, _ := readLogs(t, sender, "r1")
	var late int
	for _, line := range lines {
		if strings.HasPrefix(line.Text, "y") {
			late++
		}
	}
	if late != 3 {
		t.Errorf("lines after the pause were dropped: %d of 3 delivered", late)
	}
}

func TestStreamManager_FollowStreamsShareATotalBudget(t *testing.T) {
	m, _ := newTestStreamManager()
	m.totalBudget = newByteBudget(1000, 3000)
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 50 {
			if err := emit(logLine(strings.Repeat("x", 100))); err != nil {
				return err
			}
		}
		return nil
	}}

	var delivered, dropped int64
	for _, id := range []string{"r1", "r2"} {
		req := logRequest(id)
		req.Follow = true
		m.StartLogs(t.Context(), sender, streamer, req)
		expectStarted(t, sender, id)
		lines, chunks, _ := readLogs(t, sender, id)
		var streamDropped int64
		for _, chunk := range chunks {
			streamDropped += chunk.DroppedLines
		}
		if int64(len(lines))+streamDropped != 50 {
			t.Errorf("%s: every line must be delivered or counted: %d + %d != 50", id, len(lines), streamDropped)
		}
		delivered += int64(len(lines))
		dropped += streamDropped
	}

	// 3000 bytes of budget, 132 bytes per line.
	if delivered != 22 || dropped != 78 {
		t.Errorf("delivered = %d, dropped = %d, want 22 and 78", delivered, dropped)
	}
}

func TestStreamManager_TotalBudgetDoesNotLimitDownloads(t *testing.T) {
	m, _ := newTestStreamManager()
	m.totalBudget = newByteBudget(0, 0)
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 20 {
			if err := emit(logLine(strings.Repeat("x", 100))); err != nil {
				return err
			}
		}
		return nil
	}}
	req := logRequest("r1")
	req.Tail = -1

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	if lines, _, _ := readLogs(t, sender, "r1"); len(lines) != 20 {
		t.Errorf("a download must be complete, got %d lines", len(lines))
	}
}

func TestStreamManager_NonFollowDoesNotDropLines(t *testing.T) {
	m, _ := newTestStreamManager()
	m.logBurstBytes = 100
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 50 {
			if err := emit(logLine(strings.Repeat("x", 100))); err != nil {
				return err
			}
		}
		return nil
	}}
	req := logRequest("r1")
	req.Tail = -1

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	lines, chunks, _ := readLogs(t, sender, "r1")
	if len(lines) != 50 {
		t.Errorf("a download must be complete, got %d lines", len(lines))
	}
	for _, chunk := range chunks {
		if chunk.DroppedLines != 0 {
			t.Errorf("dropped = %d", chunk.DroppedLines)
		}
	}
}

func TestStreamManager_MaxBytesEndsNonFollowRead(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	var emitErr error
	streamer := &stubStreamer{logs: func(_ context.Context, _ agentdocker.LogsRequest, started func(), emit func(*messages.LogLine) error) error {
		started()
		for range 100 {
			if emitErr = emit(logLine(strings.Repeat("x", 100))); emitErr != nil {
				return emitErr
			}
		}
		return nil
	}}
	req := logRequest("r1")
	req.Tail = -1
	req.MaxBytes = 1000

	m.StartLogs(t.Context(), sender, streamer, req)

	expectStarted(t, sender, "r1")
	lines, _, end := readLogs(t, sender, "r1")
	if end.Reason != messages.StreamEndReason_STREAM_END_REASON_LIMIT {
		t.Errorf("end = %v", end)
	}
	size := 0
	for _, line := range lines {
		size += len(line.Text)
	}
	if size == 0 || size > 1000 {
		t.Errorf("delivered %d bytes with a limit of 1000", size)
	}
}

func TestStreamManager_RequestOptions(t *testing.T) {
	since := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	tests := []struct {
		name       string
		req        *messages.ContainerLogsRequest
		wantTail   int32
		wantSince  time.Time
		wantFollow bool
	}{
		{"all lines without follow", &messages.ContainerLogsRequest{Tail: -1}, -1, time.Time{}, false},
		{"all lines of a follow are clamped", &messages.ContainerLogsRequest{Tail: -1, Follow: true}, maxFollowTail, time.Time{}, true},
		{"follow tail is clamped", &messages.ContainerLogsRequest{Tail: 1_000_000, Follow: true}, maxFollowTail, time.Time{}, true},
		{"tail zero is kept", &messages.ContainerLogsRequest{Tail: 0, Follow: true}, 0, time.Time{}, true},
		{"small tail is kept", &messages.ContainerLogsRequest{Tail: 50, Follow: true}, 50, time.Time{}, true},
		{"since", &messages.ContainerLogsRequest{Tail: -1, Follow: true, SinceUnixMs: since.UnixMilli()}, maxFollowTail, since, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestStreamManager()
			sender := newStreamSender()
			got := make(chan agentdocker.LogsRequest, 1)
			streamer := &stubStreamer{logs: func(_ context.Context, req agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
				got <- req
				started()
				return nil
			}}
			tt.req.RequestId, tt.req.ApplicationId, tt.req.ContainerId = "r1", "app-1", "c1"

			m.StartLogs(t.Context(), sender, streamer, tt.req)

			req := <-got
			if req.Tail != tt.wantTail || !req.Since.Equal(tt.wantSince) || req.Follow != tt.wantFollow {
				t.Errorf("request = %+v", req)
			}
			if req.ApplicationID != "app-1" || req.ContainerID != "c1" {
				t.Errorf("request = %+v", req)
			}
		})
	}
}

func TestStreamManager_NotFoundAndErrorsEndTheStream(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantReason messages.StreamEndReason
		wantMsg    string
	}{
		{"unknown container", agentdocker.ErrContainerNotFound, messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND, ""},
		{"wrapped unknown container", fmt.Errorf("x: %w", agentdocker.ErrContainerNotFound), messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND, ""},
		{"other error", errors.New("logging driver does not support reading"), messages.StreamEndReason_STREAM_END_REASON_ERROR, "logging driver does not support reading"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestStreamManager()
			sender := newStreamSender()
			streamer := &stubStreamer{logs: func(context.Context, agentdocker.LogsRequest, func(), func(*messages.LogLine) error) error {
				return tt.err
			}}

			m.StartLogs(t.Context(), sender, streamer, logRequest("r1"))

			end := expectEnd(t, sender, "r1", tt.wantReason)
			if end.ErrorMessage != tt.wantMsg {
				t.Errorf("error message = %q, want %q", end.ErrorMessage, tt.wantMsg)
			}
			select {
			case msg := <-sender.sent:
				t.Errorf("unexpected message after the end: %T", msg.Payload)
			default:
			}
			waitForStreamCount(t, m, 0)
		})
	}
}

func TestStreamManager_CancelStopsTheSource(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	stopped := make(chan struct{})
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		close(stopped)
		return ctx.Err()
	}}
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(t.Context(), sender, streamer, req)
	expectStarted(t, sender, "r1")
	m.Cancel("r1")

	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_CANCELLED)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the source was not stopped")
	}
	waitForStreamCount(t, m, 0)
}

func TestStreamManager_CancelOfUnknownStreamIsIgnored(t *testing.T) {
	m, _ := newTestStreamManager()
	m.Cancel("nope")
	m.KeepAlive("nope")
}

func TestStreamManager_LeaseExpiry(t *testing.T) {
	m, clock := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		return ctx.Err()
	}}
	for _, id := range []string{"stale", "refreshed"} {
		req := logRequest(id)
		req.Follow = true
		m.StartLogs(t.Context(), sender, streamer, req)
		expectStarted(t, sender, id)
	}

	clock.Advance(leaseTTL - time.Second)
	m.KeepAlive("refreshed")
	clock.Advance(2 * time.Second)
	m.expireLeases()

	expectEnd(t, sender, "stale", messages.StreamEndReason_STREAM_END_REASON_LEASE_EXPIRED)
	waitForStreamCount(t, m, 1)

	clock.Advance(leaseTTL + time.Second)
	m.expireLeases()
	expectEnd(t, sender, "refreshed", messages.StreamEndReason_STREAM_END_REASON_LEASE_EXPIRED)
	waitForStreamCount(t, m, 0)
}

func TestStreamManager_StopAllEndsStreamsSilently(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		return ctx.Err()
	}}
	for _, id := range []string{"r1", "r2"} {
		req := logRequest(id)
		req.Follow = true
		m.StartLogs(t.Context(), sender, streamer, req)
		expectStarted(t, sender, id)
	}

	m.StopAll()
	m.wg.Wait()

	if m.count() != 0 {
		t.Errorf("count = %d", m.count())
	}
	select {
	case msg := <-sender.sent:
		t.Errorf("the connection is gone, nothing may be sent: %T", msg.Payload)
	default:
	}
}

func TestStreamManager_ParentContextCancellationIsSilent(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		return ctx.Err()
	}}
	ctx, cancel := context.WithCancel(t.Context())
	req := logRequest("r1")
	req.Follow = true

	m.StartLogs(ctx, sender, streamer, req)
	expectStarted(t, sender, "r1")
	cancel()
	m.wg.Wait()

	select {
	case msg := <-sender.sent:
		t.Errorf("unexpected message on shutdown: %T", msg.Payload)
	default:
	}
}

func TestStreamManager_RejectsStreamsOverTheLimit(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		return ctx.Err()
	}}
	for i := range maxStreams {
		req := logRequest(fmt.Sprintf("r%d", i))
		req.Follow = true
		m.StartLogs(t.Context(), sender, streamer, req)
	}
	for range maxStreams {
		if nextStreamMessage(t, sender).GetStreamStarted() == nil {
			t.Fatal("expected StreamStarted")
		}
	}

	m.StartLogs(t.Context(), sender, streamer, logRequest("over"))
	expectEnd(t, sender, "over", messages.StreamEndReason_STREAM_END_REASON_LIMIT)
	if m.count() != maxStreams {
		t.Errorf("count = %d", m.count())
	}

	m.Cancel("r0")
	expectEnd(t, sender, "r0", messages.StreamEndReason_STREAM_END_REASON_CANCELLED)
	waitForStreamCount(t, m, maxStreams-1)
	req := logRequest("after")
	req.Follow = true
	m.StartLogs(t.Context(), sender, streamer, req)
	expectStarted(t, sender, "after")
}

func TestStreamManager_RejectsDuplicateRequestID(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		return ctx.Err()
	}}
	req := logRequest("r1")
	req.Follow = true
	m.StartLogs(t.Context(), sender, streamer, req)
	expectStarted(t, sender, "r1")

	m.StartLogs(t.Context(), sender, streamer, req)

	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_ERROR)
	if m.count() != 1 {
		t.Errorf("the running stream must survive, count = %d", m.count())
	}
}

func TestStreamManager_UnsupportedDeployer(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()

	m.StartLogs(t.Context(), sender, &stubDeployer{}, logRequest("r1"))
	end := expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_ERROR)
	if end.ErrorMessage == "" {
		t.Error("expected an error message")
	}

	m.StartStats(t.Context(), sender, &stubDeployer{}, statsRequest("r2"))
	expectEnd(t, sender, "r2", messages.StreamEndReason_STREAM_END_REASON_ERROR)
}

func TestStreamManager_StatsSamples(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{stats: func(_ context.Context, appID, containerID string, started func(), emit func(*messages.ContainerStatsSample) error) error {
		if appID != "app-1" || containerID != "c1" {
			return fmt.Errorf("unexpected target %s/%s", appID, containerID)
		}
		started()
		for i := range 3 {
			if err := emit(&messages.ContainerStatsSample{TimestampMs: int64(i), CpuPercent: float64(i)}); err != nil {
				return err
			}
		}
		return nil
	}}

	m.StartStats(t.Context(), sender, streamer, statsRequest("r1"))

	expectStarted(t, sender, "r1")
	for i := range 3 {
		sample := nextStreamMessage(t, sender).GetContainerStatsSample()
		if sample == nil || sample.RequestId != "r1" || sample.TimestampMs != int64(i) {
			t.Fatalf("sample %d = %v", i, sample)
		}
	}
	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE)
	waitForStreamCount(t, m, 0)
}

func TestStreamManager_StatsErrors(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	streamer := &stubStreamer{stats: func(context.Context, string, string, func(), func(*messages.ContainerStatsSample) error) error {
		return agentdocker.ErrContainerNotFound
	}}

	m.StartStats(t.Context(), sender, streamer, statsRequest("r1"))

	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND)
}

func TestStreamManager_SendFailureStopsTheStream(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := &stubSender{err: errors.New("connection closed")}
	stopped := make(chan struct{})
	streamer := &stubStreamer{stats: func(ctx context.Context, _, _ string, started func(), emit func(*messages.ContainerStatsSample) error) error {
		started()
		for ctx.Err() == nil {
			if err := emit(&messages.ContainerStatsSample{}); err != nil {
				close(stopped)
				return err
			}
		}
		return ctx.Err()
	}}

	m.StartStats(t.Context(), sender, streamer, statsRequest("r1"))

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("a failing connection must stop the stream")
	}
	waitForStreamCount(t, m, 0)
}

func TestHandleServerMessage_RoutesStreamMessages(t *testing.T) {
	m, _ := newTestStreamManager()
	sender := newStreamSender()
	stopped := make(chan struct{})
	streamer := &stubStreamer{logs: func(ctx context.Context, _ agentdocker.LogsRequest, started func(), _ func(*messages.LogLine) error) error {
		blockUntilDone(ctx, started)
		close(stopped)
		return ctx.Err()
	}, stats: func(context.Context, string, string, func(), func(*messages.ContainerStatsSample) error) error {
		return errors.New("stats unavailable")
	}}
	session, err := wscrypto.NewSession(make([]byte, 32), wscrypto.RoleAgent)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	hub := newHubTestSession(t)
	send := func(payload *messages.ServerMessage) {
		t.Helper()
		env, err := hub.Encrypt(payload)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		handleServerMessage(t.Context(), &messages.ServerMessage{
			Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
		}, session, sender, streamer, nil, m)
	}

	req := logRequest("r1")
	req.Follow = true
	send(&messages.ServerMessage{Payload: &messages.ServerMessage_ContainerLogsRequest{ContainerLogsRequest: req}})
	expectStarted(t, sender, "r1")

	send(&messages.ServerMessage{Payload: &messages.ServerMessage_StreamKeepAlive{StreamKeepAlive: &messages.StreamKeepAlive{RequestId: "r1"}}})
	send(&messages.ServerMessage{Payload: &messages.ServerMessage_StreamCancel{StreamCancel: &messages.StreamCancel{RequestId: "r1"}}})

	expectEnd(t, sender, "r1", messages.StreamEndReason_STREAM_END_REASON_CANCELLED)
	<-stopped

	send(&messages.ServerMessage{Payload: &messages.ServerMessage_ContainerStatsRequest{ContainerStatsRequest: statsRequest("r2")}})
	expectEnd(t, sender, "r2", messages.StreamEndReason_STREAM_END_REASON_ERROR)
}
