package websocket

import (
	"context"
	"errors"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
)

type openOutcome struct {
	stream *Stream
	err    error
}

func startOpen(open func() (*Stream, error)) <-chan openOutcome {
	done := make(chan openOutcome, 1)
	go func() {
		stream, err := open()
		done <- openOutcome{stream, err}
	}()
	return done
}

func waitOpen(t *testing.T, done <-chan openOutcome) openOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(time.Second):
		t.Fatal("open did not return")
		return openOutcome{}
	}
}

func openLogsStream(t *testing.T, h *Hub, client *Client) (*Stream, string) {
	t.Helper()
	done := startOpen(func() (*Stream, error) {
		return h.OpenLogs(context.Background(), client.Id, "app-1", "abc123", LogsOptions{Tail: 100, Follow: true})
	})
	req := receiveServerMessage(t, client).GetContainerLogsRequest()
	if req == nil {
		t.Fatal("expected ContainerLogsRequest")
	}
	if !h.ResolveStreamStarted(client, &messages.StreamStarted{RequestId: req.RequestId}) {
		t.Fatal("expected the started ack to be accepted")
	}
	outcome := waitOpen(t, done)
	if outcome.err != nil {
		t.Fatalf("OpenLogs: %v", outcome.err)
	}
	return outcome.stream, req.RequestId
}

func openStatsStream(t *testing.T, h *Hub, client *Client) (*Stream, string) {
	t.Helper()
	done := startOpen(func() (*Stream, error) {
		return h.OpenStats(context.Background(), client.Id, "app-1", "abc123")
	})
	req := receiveServerMessage(t, client).GetContainerStatsRequest()
	if req == nil {
		t.Fatal("expected ContainerStatsRequest")
	}
	h.ResolveStreamStarted(client, &messages.StreamStarted{RequestId: req.RequestId})
	outcome := waitOpen(t, done)
	if outcome.err != nil {
		t.Fatalf("OpenStats: %v", outcome.err)
	}
	return outcome.stream, req.RequestId
}

func receiveFrame(t *testing.T, stream *Stream) Frame {
	t.Helper()
	select {
	case frame, ok := <-stream.Frames():
		if !ok {
			t.Fatal("frames closed")
		}
		return frame
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
		return Frame{}
	}
}

func requireClosed(t *testing.T, stream *Stream) {
	t.Helper()
	select {
	case frame, ok := <-stream.Frames():
		if ok {
			t.Fatalf("expected closed frames, got %+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("frames not closed")
	}
}

func requireNoMessage(t *testing.T, client *Client) {
	t.Helper()
	select {
	case msg := <-client.Send:
		t.Fatalf("unexpected message: %+v", msg)
	default:
	}
}

func streamCount(h *Hub) int {
	h.streamMu.Lock()
	defer h.streamMu.Unlock()
	return len(h.streams)
}

func TestOpenLogs_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	done := startOpen(func() (*Stream, error) {
		return h.OpenLogs(context.Background(), "agent-1", "app-1", "abc123", LogsOptions{
			Tail:        50,
			SinceUnixMs: 1234,
			Follow:      true,
			MaxBytes:    4096,
		})
	})
	req := receiveServerMessage(t, client).GetContainerLogsRequest()
	if req == nil {
		t.Fatal("expected ContainerLogsRequest")
	}
	if req.ApplicationId != "app-1" || req.ContainerId != "abc123" || req.Tail != 50 ||
		req.SinceUnixMs != 1234 || !req.Follow || req.MaxBytes != 4096 {
		t.Fatalf("unexpected request: %+v", req)
	}

	h.ResolveStreamStarted(client, &messages.StreamStarted{RequestId: req.RequestId})
	stream := waitOpen(t, done).stream

	chunk := &messages.ContainerLogsChunk{RequestId: req.RequestId, Lines: []*messages.LogLine{{Text: "hello"}}}
	if !h.DeliverLogs(client, chunk) {
		t.Fatal("expected the chunk to be delivered")
	}
	if got := receiveFrame(t, stream).Logs; got == nil || got.Lines[0].Text != "hello" {
		t.Fatalf("unexpected frame: %+v", got)
	}

	end := &messages.StreamEnd{RequestId: req.RequestId, Reason: messages.StreamEndReason_STREAM_END_REASON_COMPLETED}
	if !h.DeliverStreamEnd(client, end) {
		t.Fatal("expected the end to be delivered")
	}
	if got := receiveFrame(t, stream).End; got == nil || got.Reason != messages.StreamEndReason_STREAM_END_REASON_COMPLETED {
		t.Fatalf("unexpected end frame: %+v", got)
	}
	requireClosed(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("Err = %v, want nil after a regular end", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}

	stream.Close()
	requireNoMessage(t, client)
}

func TestOpenStats_RoundTrip(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	stream, requestID := openStatsStream(t, h, client)

	if !h.DeliverStats(client, &messages.ContainerStatsSample{RequestId: requestID, CpuPercent: 12.5}) {
		t.Fatal("expected the sample to be delivered")
	}
	if got := receiveFrame(t, stream).Stats; got == nil || got.CpuPercent != 12.5 {
		t.Fatalf("unexpected frame: %+v", got)
	}
}

func TestOpenStream_AgentOffline(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	if _, err := h.OpenLogs(context.Background(), "missing", "app-1", "abc", LogsOptions{}); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("OpenLogs error = %v, want ErrAgentOffline", err)
	}
	if _, err := h.OpenStats(context.Background(), "missing", "app-1", "abc"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("OpenStats error = %v, want ErrAgentOffline", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}
}

func TestOpenStream_ContextCancelledSendsCancel(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := h.OpenStats(ctx, "agent-1", "app-1", "abc")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want DeadlineExceeded", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}

	req := receiveServerMessage(t, client).GetContainerStatsRequest()
	cancelMsg := receiveServerMessage(t, client).GetStreamCancel()
	if cancelMsg == nil || cancelMsg.RequestId != req.RequestId {
		t.Fatalf("expected a StreamCancel for the abandoned request, got %+v", cancelMsg)
	}
}

func TestOpenStream_RejectedByAgent(t *testing.T) {
	tests := []struct {
		name   string
		reason messages.StreamEndReason
		want   error
	}{
		{"not found", messages.StreamEndReason_STREAM_END_REASON_NOT_FOUND, ErrContainerNotFound},
		{"limit", messages.StreamEndReason_STREAM_END_REASON_LIMIT, ErrStreamLimit},
		{"error", messages.StreamEndReason_STREAM_END_REASON_ERROR, ErrStreamFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := testLogger()
			h := NewHub(&log)
			client := registerEnvTestClient(t, h)

			done := startOpen(func() (*Stream, error) {
				return h.OpenLogs(context.Background(), "agent-1", "app-1", "abc", LogsOptions{})
			})
			req := receiveServerMessage(t, client).GetContainerLogsRequest()
			h.DeliverStreamEnd(client, &messages.StreamEnd{RequestId: req.RequestId, Reason: tt.reason, ErrorMessage: "boom"})

			outcome := waitOpen(t, done)
			if !errors.Is(outcome.err, tt.want) || outcome.stream != nil {
				t.Fatalf("outcome = %+v, want error %v", outcome, tt.want)
			}
			if n := streamCount(h); n != 0 {
				t.Fatalf("stream leaked: %d", n)
			}
			requireNoMessage(t, client)
		})
	}
}

func TestStream_RejectsFramesFromOtherClients(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openLogsStream(t, h, client)

	other := &Client{Id: "agent-1"}
	if h.DeliverLogs(other, &messages.ContainerLogsChunk{RequestId: requestID}) {
		t.Error("accepted a chunk from a client the stream is not bound to")
	}
	if h.DeliverStreamEnd(other, &messages.StreamEnd{RequestId: requestID}) {
		t.Error("accepted an end from a client the stream is not bound to")
	}
	if h.DeliverLogs(client, &messages.ContainerLogsChunk{RequestId: "unknown"}) {
		t.Error("accepted a chunk for an unknown stream")
	}
	if h.DeliverStats(client, &messages.ContainerStatsSample{RequestId: requestID}) {
		t.Error("accepted a stats sample on a logs stream")
	}

	select {
	case frame := <-stream.Frames():
		t.Fatalf("unexpected frame: %+v", frame)
	default:
	}
}

func TestStream_AbortedOnDisconnect(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, _ := openLogsStream(t, h, client)

	h.BeginDisconnect(client)
	h.FinishDisconnect(client)

	requireClosed(t, stream)
	if err := stream.Err(); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("Err = %v, want ErrAgentOffline", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}
}

func TestStream_PendingOpenAbortedOnDisconnect(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	done := startOpen(func() (*Stream, error) {
		return h.OpenStats(context.Background(), "agent-1", "app-1", "abc")
	})
	receiveServerMessage(t, client)

	h.BeginDisconnect(client)
	h.FinishDisconnect(client)

	if outcome := waitOpen(t, done); !errors.Is(outcome.err, ErrAgentOffline) {
		t.Fatalf("error = %v, want ErrAgentOffline", outcome.err)
	}
}

func TestStream_NotAbortedByStaleDisconnect(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	oldClient := registerEnvTestClient(t, h)
	h.BeginDisconnect(oldClient)
	h.Unregister("agent-1")
	newClient := registerEnvTestClient(t, h)

	stream, requestID := openLogsStream(t, h, newClient)

	h.FinishDisconnect(oldClient)

	if !h.DeliverLogs(newClient, &messages.ContainerLogsChunk{RequestId: requestID}) {
		t.Fatal("stream of the new connection was aborted by the old connection's cleanup")
	}
	receiveFrame(t, stream)
}

func TestStream_SlowConsumerIsEnded(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openLogsStream(t, h, client)

	chunk := &messages.ContainerLogsChunk{RequestId: requestID, Lines: []*messages.LogLine{{Text: "x"}}}
	for i := range streamBufferSize {
		if !h.DeliverLogs(client, chunk) {
			t.Fatalf("chunk %d rejected before the buffer was full", i)
		}
	}

	delivered := make(chan bool, 1)
	go func() { delivered <- h.DeliverLogs(client, chunk) }()
	select {
	case ok := <-delivered:
		if ok {
			t.Fatal("expected the overflowing chunk to be rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("delivery blocked on a slow consumer")
	}

	cancelMsg := receiveServerMessage(t, client).GetStreamCancel()
	if cancelMsg == nil || cancelMsg.RequestId != requestID {
		t.Fatalf("expected a StreamCancel for the slow stream, got %+v", cancelMsg)
	}

	for range streamBufferSize {
		receiveFrame(t, stream)
	}
	requireClosed(t, stream)
	if err := stream.Err(); !errors.Is(err, ErrSlowConsumer) {
		t.Fatalf("Err = %v, want ErrSlowConsumer", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}
}

func TestStream_EndFitsIntoFullBuffer(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openLogsStream(t, h, client)

	chunk := &messages.ContainerLogsChunk{RequestId: requestID}
	for range streamBufferSize {
		h.DeliverLogs(client, chunk)
	}
	if !h.DeliverStreamEnd(client, &messages.StreamEnd{RequestId: requestID, Reason: messages.StreamEndReason_STREAM_END_REASON_CONTAINER_GONE}) {
		t.Fatal("end must be delivered even when the buffer is full")
	}

	for range streamBufferSize {
		receiveFrame(t, stream)
	}
	if end := receiveFrame(t, stream).End; end == nil {
		t.Fatal("expected the end frame last")
	}
	requireClosed(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func TestStream_CloseSendsCancelOnce(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openLogsStream(t, h, client)

	stream.Close()
	stream.Close()

	cancelMsg := receiveServerMessage(t, client).GetStreamCancel()
	if cancelMsg == nil || cancelMsg.RequestId != requestID {
		t.Fatalf("expected a StreamCancel, got %+v", cancelMsg)
	}
	requireNoMessage(t, client)
	requireClosed(t, stream)
	if err := stream.Err(); err != nil {
		t.Fatalf("Err = %v, want nil after Close", err)
	}
	if n := streamCount(h); n != 0 {
		t.Fatalf("stream leaked: %d", n)
	}
	if h.DeliverLogs(client, &messages.ContainerLogsChunk{RequestId: requestID}) {
		t.Error("accepted a chunk after Close")
	}
}

func TestStream_CloseAfterEndSendsNoCancel(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openLogsStream(t, h, client)

	h.DeliverStreamEnd(client, &messages.StreamEnd{RequestId: requestID, Reason: messages.StreamEndReason_STREAM_END_REASON_COMPLETED})
	stream.Close()

	requireNoMessage(t, client)
}

func TestStream_RefreshSendsKeepAlive(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)
	stream, requestID := openStatsStream(t, h, client)

	stream.Refresh()
	keepAlive := receiveServerMessage(t, client).GetStreamKeepAlive()
	if keepAlive == nil || keepAlive.RequestId != requestID {
		t.Fatalf("expected a StreamKeepAlive, got %+v", keepAlive)
	}

	stream.Close()
	receiveServerMessage(t, client)
	stream.Refresh()
	requireNoMessage(t, client)
}

func TestOpenStream_PerAgentLimit(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	client := registerEnvTestClient(t, h)

	streams := make([]*Stream, 0, maxStreamsPerAgent)
	for range maxStreamsPerAgent {
		stream, _ := openStatsStream(t, h, client)
		streams = append(streams, stream)
	}

	if _, err := h.OpenStats(context.Background(), "agent-1", "app-1", "abc"); !errors.Is(err, ErrStreamLimit) {
		t.Fatalf("error = %v, want ErrStreamLimit", err)
	}
	requireNoMessage(t, client)

	streams[0].Close()
	receiveServerMessage(t, client)
	openStatsStream(t, h, client)
}

func TestHandleClientMessage_StreamFrames(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)
	previous := DefaultHub
	DefaultHub = h
	t.Cleanup(func() { DefaultHub = previous })

	client := registerEnvTestClient(t, h)
	key := make([]byte, 32)
	hubSession, err := wscrypto.NewSession(key, wscrypto.RoleHub)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	client.session = hubSession
	agentSession, err := wscrypto.NewSession(key, wscrypto.RoleAgent)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	dispatch := func(inner *messages.ClientMessage) {
		t.Helper()
		env, err := agentSession.Encrypt(inner)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		handleClientMessage(t.Context(), client, &messages.ClientMessage{
			Payload: &messages.ClientMessage_EncryptedPayload{EncryptedPayload: env},
		}, &log)
	}

	done := startOpen(func() (*Stream, error) {
		return h.OpenLogs(context.Background(), "agent-1", "app-1", "abc", LogsOptions{Follow: true})
	})
	requestID := receiveServerMessage(t, client).GetContainerLogsRequest().RequestId

	dispatch(&messages.ClientMessage{Payload: &messages.ClientMessage_StreamStarted{
		StreamStarted: &messages.StreamStarted{RequestId: requestID},
	}})
	stream := waitOpen(t, done).stream
	if stream == nil {
		t.Fatal("expected the stream to open")
	}

	dispatch(&messages.ClientMessage{Payload: &messages.ClientMessage_ContainerLogsChunk{
		ContainerLogsChunk: &messages.ContainerLogsChunk{RequestId: requestID, Lines: []*messages.LogLine{{Text: "line"}}},
	}})
	if got := receiveFrame(t, stream).Logs; got == nil || got.Lines[0].Text != "line" {
		t.Fatalf("unexpected frame: %+v", got)
	}

	dispatch(&messages.ClientMessage{Payload: &messages.ClientMessage_StreamEnd{
		StreamEnd: &messages.StreamEnd{RequestId: requestID, Reason: messages.StreamEndReason_STREAM_END_REASON_COMPLETED},
	}})
	if end := receiveFrame(t, stream).End; end == nil {
		t.Fatal("expected an end frame")
	}
	requireClosed(t, stream)
}
