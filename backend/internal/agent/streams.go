package agent

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

const capabilityContainerMonitoring = "container-monitoring"

const (
	maxStreams         = 16
	leaseTTL           = 90 * time.Second
	leaseCheckInterval = 10 * time.Second

	logFlushInterval  = 500 * time.Millisecond
	logBytesPerSecond = 1 << 20 // 1 MiB/s
	logBurstBytes     = 2 << 20 // 2 MiB

	// Shared by all follow streams of the agent.
	totalLogBytesPerSecond = 4 << 20 // 4 MiB/s
	totalLogBurstBytes     = 8 << 20 // 8 MiB

	maxFollowTail    = 5000     // lines
	maxDownloadBytes = 64 << 20 // 64 MiB
)

var (
	errStreamCancelled  = errors.New("stream cancelled by the hub")
	errLeaseExpired     = errors.New("stream lease expired")
	errConnectionClosed = errors.New("hub connection closed")
	errLogLimitReached  = errors.New("log size limit reached")
)

type containerStreamer interface {
	StreamLogs(ctx context.Context, req docker.LogsRequest, started func(), emit func(*messages.LogLine) error) error
	StreamStats(ctx context.Context, appID, containerID string, started func(), emit func(*messages.ContainerStatsSample) error) error
}

type stream struct {
	cancel      context.CancelCauseFunc
	lastRefresh time.Time
}

// streamManager runs the log and stats streams the hub opened. The hub keeps a
// stream alive by refreshing its lease, so a hub that vanished without
// cancelling cannot leave streams running.
type streamManager struct {
	mu      sync.Mutex
	streams map[string]*stream
	wg      sync.WaitGroup

	now               func() time.Time
	flushInterval     time.Duration
	logBytesPerSecond float64
	logBurstBytes     float64
	totalBudget       *byteBudget
}

func newStreamManager() *streamManager {
	return &streamManager{
		streams:           make(map[string]*stream),
		now:               time.Now,
		flushInterval:     logFlushInterval,
		logBytesPerSecond: logBytesPerSecond,
		logBurstBytes:     logBurstBytes,
		totalBudget:       newByteBudget(totalLogBytesPerSecond, totalLogBurstBytes),
	}
}

func (m *streamManager) run(ctx context.Context) {
	ticker := time.NewTicker(leaseCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.expireLeases()
		case <-ctx.Done():
			return
		}
	}
}

func (m *streamManager) StartLogs(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.ContainerLogsRequest) {
	streamer, ok := deployer.(containerStreamer)
	if !ok {
		go sendStreamEnd(sender, req.RequestId, messages.StreamEndReason_STREAM_END_REASON_ERROR, errContainersUnsupported)
		return
	}

	logs := docker.LogsRequest{
		ApplicationID: req.ApplicationId,
		ContainerID:   req.ContainerId,
		Tail:          max(req.Tail, -1),
		Follow:        req.Follow,
	}
	if req.SinceUnixMs > 0 {
		logs.Since = time.UnixMilli(req.SinceUnixMs)
	}

	onEnd := messages.StreamEndReason_STREAM_END_REASON_COMPLETED
	var maxBytes int64
	if req.Follow {
		onEnd = messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE
		if logs.Tail < 0 || logs.Tail > maxFollowTail {
			logs.Tail = maxFollowTail
		}
	} else if maxBytes = req.MaxBytes; maxBytes <= 0 || maxBytes > maxDownloadBytes {
		maxBytes = maxDownloadBytes
	}

	m.start(ctx, sender, req.RequestId, onEnd, func(ctx context.Context, started func()) error {
		return m.streamLogs(ctx, sender, streamer, req.RequestId, logs, maxBytes, started)
	})
}

func (m *streamManager) StartStats(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.ContainerStatsRequest) {
	streamer, ok := deployer.(containerStreamer)
	if !ok {
		go sendStreamEnd(sender, req.RequestId, messages.StreamEndReason_STREAM_END_REASON_ERROR, errContainersUnsupported)
		return
	}

	m.start(ctx, sender, req.RequestId, messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE, func(ctx context.Context, started func()) error {
		return streamer.StreamStats(ctx, req.ApplicationId, req.ContainerId, started, func(sample *messages.ContainerStatsSample) error {
			sample.RequestId = req.RequestId
			return sender.SendMessage(&messages.ClientMessage{
				Payload: &messages.ClientMessage_ContainerStatsSample{ContainerStatsSample: sample},
			})
		})
	})
}

func (m *streamManager) Cancel(requestID string) {
	m.mu.Lock()
	s := m.streams[requestID]
	m.mu.Unlock()
	if s != nil {
		s.cancel(errStreamCancelled)
	}
}

func (m *streamManager) KeepAlive(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.streams[requestID]; s != nil {
		s.lastRefresh = m.now()
	}
}

func (m *streamManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.streams {
		s.cancel(errConnectionClosed)
	}
}

func (m *streamManager) expireLeases() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, s := range m.streams {
		if now.Sub(s.lastRefresh) > leaseTTL {
			s.cancel(errLeaseExpired)
		}
	}
}

func (m *streamManager) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.streams)
}

// start registers the stream and runs it until it ends. A stream the agent
// cannot take is answered with a StreamEnd right away.
func (m *streamManager) start(parent context.Context, sender outboundSender, requestID string, onEnd messages.StreamEndReason, run func(ctx context.Context, started func()) error) {
	ctx, cancel := context.WithCancelCause(parent)
	s := &stream{cancel: cancel}

	m.mu.Lock()
	var rejection messages.StreamEndReason
	var rejectionMessage string
	switch {
	case m.streams[requestID] != nil:
		rejection, rejectionMessage = messages.StreamEndReason_STREAM_END_REASON_ERROR, "duplicate request ID"
	case len(m.streams) >= maxStreams:
		rejection = messages.StreamEndReason_STREAM_END_REASON_LIMIT
	default:
		s.lastRefresh = m.now()
		m.streams[requestID] = s
		m.wg.Add(1)
	}
	m.mu.Unlock()

	if rejection != messages.StreamEndReason_STREAM_END_REASON_UNSPECIFIED {
		cancel(nil)
		go sendStreamEnd(sender, requestID, rejection, rejectionMessage)
		return
	}

	go func() {
		defer m.wg.Done()
		err := run(ctx, func() { sendStreamStarted(sender, requestID) })

		m.mu.Lock()
		delete(m.streams, requestID)
		m.mu.Unlock()

		reason, message, send := streamEndReason(ctx, err, onEnd)
		cancel(nil)
		if send {
			sendStreamEnd(sender, requestID, reason, message)
		}
	}()
}

func (m *streamManager) streamLogs(ctx context.Context, sender outboundSender, streamer containerStreamer, requestID string, req docker.LogsRequest, maxBytes int64, started func()) error {
	batcher := &logBatcher{sender: sender, requestID: requestID}
	if req.Follow {
		batcher.budget = newByteBudget(m.logBytesPerSecond, m.logBurstBytes)
		batcher.total = m.totalBudget
		batcher.now = m.now
	}

	stopFlushing := make(chan struct{})
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		ticker := time.NewTicker(m.flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := batcher.flush(); err != nil {
					Log.Debug().Err(err).Str("request_id", requestID).Msg("failed to send log lines")
				}
			case <-stopFlushing:
				return
			}
		}
	}()

	var total int64
	err := streamer.StreamLogs(ctx, req, started, func(line *messages.LogLine) error {
		if maxBytes > 0 {
			total += int64(len(line.Text)) + 1
			if total > maxBytes {
				return errLogLimitReached
			}
		}
		return batcher.add(line)
	})
	close(stopFlushing)
	<-flushed

	if ctx.Err() == nil {
		if flushErr := batcher.flush(); err == nil {
			err = flushErr
		}
	}
	return err
}

// streamEndReason maps how a stream ended to what the hub is told. Nothing is
// sent when the hub connection is gone or the agent is shutting down.
func streamEndReason(ctx context.Context, err error, onEnd messages.StreamEndReason) (messages.StreamEndReason, string, bool) {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, errStreamCancelled):
		return messages.StreamEndReason_STREAM_END_REASON_CANCELLED, "", true
	case errors.Is(cause, errLeaseExpired):
		return messages.StreamEndReason_STREAM_END_REASON_LEASE_EXPIRED, "", true
	case ctx.Err() != nil:
		return 0, "", false
	}

	switch {
	case errors.Is(err, errLogLimitReached):
		return messages.StreamEndReason_STREAM_END_REASON_LIMIT, "", true
	case errors.Is(err, docker.ErrContainerNotFound):
		return messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND, "", true
	case err != nil:
		return messages.StreamEndReason_STREAM_END_REASON_ERROR, err.Error(), true
	}
	return onEnd, "", true
}

func sendStreamStarted(sender outboundSender, requestID string) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_StreamStarted{StreamStarted: &messages.StreamStarted{RequestId: requestID}},
	}); err != nil {
		Log.Error().Err(err).Str("request_id", requestID).Msg("failed to send stream started")
	}
}

func sendStreamEnd(sender outboundSender, requestID string, reason messages.StreamEndReason, message string) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_StreamEnd{StreamEnd: &messages.StreamEnd{
			RequestId:    requestID,
			Reason:       reason,
			ErrorMessage: message,
		}},
	}); err != nil {
		Log.Error().Err(err).Str("request_id", requestID).Msg("failed to send stream end")
	}
}
