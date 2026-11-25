package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const MaxLogLineBytes = 16 << 10 // 16 KiB

// Room for the RFC 3339 timestamp and its separator in front of each line.
const logTimestampRoom = 64

type LogsRequest struct {
	ApplicationID string
	ContainerID   string
	// Tail is the number of existing lines to return, negative for all.
	Tail   int32
	Since  time.Time
	Follow bool
}

// StreamLogs blocks until the logs end or ctx is cancelled. started is called
// once the logs are open, a container that cannot be read fails before that.
func (c *Client) StreamLogs(ctx context.Context, req LogsRequest, started func(), emit func(*messages.LogLine) error) error {
	if !c.Ready() {
		return ErrDaemonUnreachable
	}
	return streamLogs(ctx, c.cli.Client(), req, started, emit)
}

func streamLogs(ctx context.Context, cli client.APIClient, req LogsRequest, started func(), emit func(*messages.LogLine) error) error {
	item, err := resolveContainer(ctx, cli, req.ApplicationID, req.ContainerID)
	if err != nil {
		return err
	}
	inspected, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return ErrContainerNotFound
		}
		return fmt.Errorf("inspect container: %w", err)
	}
	tty := inspected.Container.Config != nil && inspected.Container.Config.Tty

	opts := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     req.Follow,
		Tail:       "all",
	}
	if req.Tail >= 0 {
		opts.Tail = strconv.Itoa(int(req.Tail))
	}
	if !req.Since.IsZero() {
		opts.Since = fmt.Sprintf("%d.%09d", req.Since.Unix(), req.Since.Nanosecond())
	}

	logs, err := cli.ContainerLogs(ctx, item.ID, opts)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return ErrContainerNotFound
		}
		return fmt.Errorf("read logs: %w", err)
	}
	defer func() { _ = logs.Close() }()
	started()

	stdout := &lineWriter{stream: messages.LogStream_LOG_STREAM_STDOUT, emit: emit}
	stderr := &lineWriter{stream: messages.LogStream_LOG_STREAM_STDERR, emit: emit}
	if tty {
		_, err = io.Copy(stdout, logs)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, logs)
	}
	if err != nil {
		return err
	}
	// Stdout and stderr each end with a line that may lack a newline.
	return errors.Join(stdout.flush(), stderr.flush())
}

// lineWriter turns the bytes of one stream into lines. Docker prefixes every
// line with a timestamp, and a line can arrive spread over several writes.
type lineWriter struct {
	stream    messages.LogStream
	emit      func(*messages.LogLine) error
	buf       []byte
	truncated bool
}

func (w *lineWriter) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		if end < 0 {
			w.append(p)
			break
		}
		w.append(p[:end])
		if err := w.emitLine(); err != nil {
			return 0, err
		}
		p = p[end+1:]
	}
	return written, nil
}

func (w *lineWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	return w.emitLine()
}

// append keeps at most one line's worth of bytes, so a line without a newline
// cannot grow the buffer without bound.
func (w *lineWriter) append(p []byte) {
	room := MaxLogLineBytes + logTimestampRoom - len(w.buf)
	if len(p) > room {
		p = p[:room]
		w.truncated = true
	}
	w.buf = append(w.buf, p...)
}

func (w *lineWriter) emitLine() error {
	line, truncated := w.buf, w.truncated
	w.buf, w.truncated = w.buf[:0], false

	timestamp, text := splitLogTimestamp(line)
	text = bytes.TrimSuffix(text, []byte("\r"))
	if len(text) > MaxLogLineBytes {
		text, truncated = text[:MaxLogLineBytes], true
	}
	return w.emit(&messages.LogLine{
		Timestamp: timestamppb.New(timestamp),
		Stream:    w.stream,
		// Protobuf strings must be valid UTF-8, container output does not have to be.
		Text:      strings.ToValidUTF8(string(text), "�"),
		Truncated: truncated,
	})
}

func splitLogTimestamp(line []byte) (time.Time, []byte) {
	if sep := bytes.IndexByte(line, ' '); sep > 0 {
		if timestamp, err := time.Parse(time.RFC3339Nano, string(line[:sep])); err == nil {
			return timestamp, line[sep+1:]
		}
	}
	return time.Now(), line
}
