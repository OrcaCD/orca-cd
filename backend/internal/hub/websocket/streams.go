package websocket

import (
	"context"
	"errors"
	"fmt"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/google/uuid"
)

var (
	ErrContainerNotFound = errors.New("container not found")
	ErrStreamLimit       = errors.New("too many open streams")
	ErrStreamFailed      = errors.New("agent could not open the stream")
	ErrSlowConsumer      = errors.New("stream consumer too slow")
)

const (
	// streamBufferSize is the number of data frames a stream buffers before its
	// consumer counts as too slow.
	streamBufferSize = 64
	// maxStreamsPerAgent stays below the limit of the agent, so the hub rejects
	// requests before the agent has to.
	maxStreamsPerAgent = 12
	streamStartTimeout = 15 * time.Second
)

type Frame struct {
	Logs  *messages.ContainerLogsChunk
	Stats *messages.ContainerStatsSample
	End   *messages.StreamEnd
}

type LogsOptions struct {
	// Tail is the number of existing lines to send, -1 for all and 0 for none.
	Tail        int32
	SinceUnixMs int64
	Follow      bool
	MaxBytes    int64
}

type streamKind int

const (
	streamKindLogs streamKind = iota
	streamKindStats
)

type streamEntry struct {
	agentID       string
	applicationID string
	kind          streamKind
	client        *Client
	stream        *Stream
	started       bool
	startCh       chan error
}

type Stream struct {
	hub       *Hub
	requestID string
	frames    chan Frame
	err       error
}

func (s *Stream) Frames() <-chan Frame {
	return s.frames
}

// Err is nil when the stream ended regularly or was closed by the consumer.
// It must only be read once Frames is closed.
func (s *Stream) Err() error {
	return s.err
}

// Refresh renews the lease the agent holds the stream open for.
func (s *Stream) Refresh() {
	s.hub.streamMu.Lock()
	entry, ok := s.hub.streams[s.requestID]
	s.hub.streamMu.Unlock()
	if !ok {
		return
	}
	s.hub.Send(entry.agentID, &messages.ServerMessage{
		Payload: &messages.ServerMessage_StreamKeepAlive{
			StreamKeepAlive: &messages.StreamKeepAlive{RequestId: s.requestID},
		},
	})
}

func (s *Stream) Close() {
	s.hub.closeStream(s.requestID)
}

func (h *Hub) OpenLogs(ctx context.Context, agentID, applicationID, containerID string, opts LogsOptions) (*Stream, error) {
	return h.openStream(ctx, agentID, applicationID, streamKindLogs, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_ContainerLogsRequest{
				ContainerLogsRequest: &messages.ContainerLogsRequest{
					RequestId:     requestID,
					ApplicationId: applicationID,
					ContainerId:   containerID,
					Tail:          opts.Tail,
					SinceUnixMs:   opts.SinceUnixMs,
					Follow:        opts.Follow,
					MaxBytes:      opts.MaxBytes,
				},
			},
		}
	})
}

func (h *Hub) OpenStats(ctx context.Context, agentID, applicationID, containerID string) (*Stream, error) {
	return h.openStream(ctx, agentID, applicationID, streamKindStats, func(requestID string) *messages.ServerMessage {
		return &messages.ServerMessage{
			Payload: &messages.ServerMessage_ContainerStatsRequest{
				ContainerStatsRequest: &messages.ContainerStatsRequest{
					RequestId:     requestID,
					ApplicationId: applicationID,
					ContainerId:   containerID,
				},
			},
		}
	})
}

func (h *Hub) openStream(
	ctx context.Context,
	agentID, applicationID string,
	kind streamKind,
	build func(requestID string) *messages.ServerMessage,
) (*Stream, error) {
	requestID := uuid.NewString()
	entry := &streamEntry{
		agentID:       agentID,
		applicationID: applicationID,
		kind:          kind,
		stream: &Stream{
			hub:       h,
			requestID: requestID,
			// One slot beyond streamBufferSize is reserved for the end frame.
			frames: make(chan Frame, streamBufferSize+1),
		},
		startCh: make(chan error, 1),
	}

	h.streamMu.Lock()
	if h.countStreamsLocked(agentID) >= maxStreamsPerAgent {
		h.streamMu.Unlock()
		return nil, ErrStreamLimit
	}
	h.streams[requestID] = entry
	h.streamMu.Unlock()

	bind := func(c *Client) {
		h.streamMu.Lock()
		entry.client = c
		h.streamMu.Unlock()
	}
	if !h.send(agentID, build(requestID), bind) {
		h.closeStream(requestID)
		return nil, ErrAgentOffline
	}

	ctx, cancel := context.WithTimeout(ctx, streamStartTimeout)
	defer cancel()
	select {
	case err := <-entry.startCh:
		if err != nil {
			return nil, err
		}
		return entry.stream, nil
	case <-ctx.Done():
		h.closeStream(requestID)
		return nil, ctx.Err()
	}
}

func (h *Hub) countStreamsLocked(agentID string) int {
	count := 0
	for _, entry := range h.streams {
		if entry.agentID == agentID {
			count++
		}
	}
	return count
}

func (h *Hub) endStreamLocked(entry *streamEntry, err error) {
	delete(h.streams, entry.stream.requestID)
	entry.stream.err = err
	close(entry.stream.frames)
	if !entry.started {
		entry.started = true
		entry.startCh <- err
	}
}

func (h *Hub) closeStream(requestID string) {
	h.streamMu.Lock()
	entry, ok := h.streams[requestID]
	if ok {
		h.endStreamLocked(entry, nil)
	}
	h.streamMu.Unlock()

	if ok {
		h.sendStreamCancel(entry)
	}
}

// sendStreamCancel must not be called with streamMu held: Send takes the hub
// lock, which the binding of a stream takes before streamMu.
func (h *Hub) sendStreamCancel(entry *streamEntry) {
	h.Send(entry.agentID, &messages.ServerMessage{
		Payload: &messages.ServerMessage_StreamCancel{
			StreamCancel: &messages.StreamCancel{RequestId: entry.stream.requestID},
		},
	})
}

func (h *Hub) abortStreams(c *Client) {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	for _, entry := range h.streams {
		if entry.client == c {
			h.endStreamLocked(entry, ErrAgentOffline)
		}
	}
}

func (h *Hub) ResolveStreamStarted(c *Client, started *messages.StreamStarted) bool {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	entry, ok := h.streams[started.GetRequestId()]
	if !ok || entry.client != c {
		return false
	}
	if !entry.started {
		entry.started = true
		entry.startCh <- nil
	}
	return true
}

func (h *Hub) DeliverLogs(c *Client, chunk *messages.ContainerLogsChunk) bool {
	return h.deliverFrame(c, chunk.GetRequestId(), streamKindLogs, Frame{Logs: chunk})
}

func (h *Hub) DeliverStats(c *Client, sample *messages.ContainerStatsSample) bool {
	return h.deliverFrame(c, sample.GetRequestId(), streamKindStats, Frame{Stats: sample})
}

// deliverFrame never blocks, as it runs on the read loop of the agent's
// connection. A consumer that cannot keep up loses its stream instead.
func (h *Hub) deliverFrame(c *Client, requestID string, kind streamKind, frame Frame) bool {
	h.streamMu.Lock()
	entry, ok := h.streams[requestID]
	if !ok || entry.client != c || entry.kind != kind || !entry.started {
		h.streamMu.Unlock()
		return false
	}
	if len(entry.stream.frames) >= streamBufferSize {
		h.endStreamLocked(entry, ErrSlowConsumer)
		h.streamMu.Unlock()
		h.log.Warn().Str("client", c.Id).Str("request_id", requestID).Msg("Ending stream with a slow consumer")
		h.sendStreamCancel(entry)
		return false
	}
	entry.stream.frames <- frame
	h.streamMu.Unlock()
	return true
}

func (h *Hub) DeliverStreamEnd(c *Client, end *messages.StreamEnd) bool {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	entry, ok := h.streams[end.GetRequestId()]
	if !ok || entry.client != c {
		return false
	}
	if !entry.started {
		h.endStreamLocked(entry, streamRejection(end))
		return true
	}
	// The slot reserved for the end frame keeps this from blocking.
	entry.stream.frames <- Frame{End: end}
	h.endStreamLocked(entry, nil)
	return true
}

func streamRejection(end *messages.StreamEnd) error {
	switch end.GetReason() {
	case messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND:
		return ErrContainerNotFound
	case messages.StreamEndReason_STREAM_END_REASON_LIMIT:
		return ErrStreamLimit
	default:
		if msg := end.GetErrorMessage(); msg != "" {
			return fmt.Errorf("%w: %s", ErrStreamFailed, msg)
		}
		return ErrStreamFailed
	}
}
