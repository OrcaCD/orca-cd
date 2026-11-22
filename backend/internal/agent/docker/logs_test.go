package docker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	containerapi "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fakeLogsAPI struct {
	fakeContainerAPI

	body      io.Reader
	logsErr   error
	logsOpts  client.ContainerLogsOptions
	logsCalls []string
}

type logsBody struct{ io.Reader }

func (logsBody) Close() error { return nil }

func (f *fakeLogsAPI) ContainerLogs(_ context.Context, id string, opts client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	f.logsCalls = append(f.logsCalls, id)
	f.logsOpts = opts
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return logsBody{f.body}, nil
}

func muxFrame(streamType byte, payload string) []byte {
	n := len(payload)
	header := []byte{streamType, 0, 0, 0, byte(n >> 24 & 0xff), byte(n >> 16 & 0xff), byte(n >> 8 & 0xff), byte(n & 0xff)}
	return append(header, payload...)
}

func newLogsAPI(tty bool, body io.Reader) *fakeLogsAPI {
	inspect := webInspect()
	inspect.Container.Config = &containerapi.Config{Tty: tty}
	return &fakeLogsAPI{
		results: map[string]client.ContainerInspectResult{webID: inspect},
		body:    body,
	}
}

func collectLogs(t *testing.T, cli client.APIClient, req LogsRequest) ([]*messages.LogLine, bool, error) {
	t.Helper()
	var lines []*messages.LogLine
	started := false
	err := streamLogs(t.Context(), cli, req, func() { started = true }, func(line *messages.LogLine) error {
		lines = append(lines, line)
		return nil
	})
	return lines, started, err
}

func TestStreamLogs_DemuxesStdoutAndStderr(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	var body bytes.Buffer
	body.Write(muxFrame(1, "2026-01-02T03:04:05.123456789Z hello\n"))
	body.Write(muxFrame(2, "2026-01-02T03:04:06.5Z oops\n"))
	cli := newLogsAPI(false, &body)

	lines, started, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID[:12], Tail: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Error("started was not called")
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	if lines[0].Text != "hello" || lines[0].Stream != messages.LogStream_LOG_STREAM_STDOUT {
		t.Errorf("first line = %v", lines[0])
	}
	if got := lines[0].Timestamp.AsTime(); got.Nanosecond() != 123456789 {
		t.Errorf("timestamp lost its nanoseconds: %v", got)
	}
	if lines[1].Text != "oops" || lines[1].Stream != messages.LogStream_LOG_STREAM_STDERR {
		t.Errorf("second line = %v", lines[1])
	}
	if len(cli.logsCalls) != 1 || cli.logsCalls[0] != webID {
		t.Errorf("logs must be read with the resolved full ID, got %v", cli.logsCalls)
	}
}

func TestStreamLogs_TTYIsNotMultiplexed(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader("2026-01-02T03:04:05Z one\r\n2026-01-02T03:04:06Z two\n"))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Text != "one" || lines[1].Text != "two" {
		t.Errorf("lines = %v", lines)
	}
	if lines[0].Stream != messages.LogStream_LOG_STREAM_STDOUT {
		t.Errorf("tty output must be reported as stdout, got %v", lines[0].Stream)
	}
}

func TestStreamLogs_JoinsLinesSplitAcrossFrames(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	var body bytes.Buffer
	body.Write(muxFrame(1, "2026-01-02T03:04:05Z par"))
	body.Write(muxFrame(2, "2026-01-02T03:04:05Z err\n"))
	body.Write(muxFrame(1, "tial\n"))
	cli := newLogsAPI(false, &body)

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Text != "err" || lines[1].Text != "partial" {
		t.Errorf("lines = %v", lines)
	}
}

func TestStreamLogs_EmitsTrailingLineWithoutNewline(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(false, bytes.NewReader(muxFrame(1, "2026-01-02T03:04:05Z last")))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Text != "last" {
		t.Errorf("lines = %v", lines)
	}
}

func TestStreamLogs_KeepsEmptyLines(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader("2026-01-02T03:04:05Z \n2026-01-02T03:04:06Z x\n"))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Text != "" || lines[1].Text != "x" {
		t.Errorf("lines = %v", lines)
	}
}

func TestStreamLogs_TruncatesOversizedLines(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	huge := strings.Repeat("a", 5*MaxLogLineBytes)
	cli := newLogsAPI(true, strings.NewReader("2026-01-02T03:04:05Z "+huge+"\n2026-01-02T03:04:06Z next\n"))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected the rest of the oversized line to be discarded, got %d lines", len(lines))
	}
	if len(lines[0].Text) != MaxLogLineBytes || !lines[0].Truncated {
		t.Errorf("len = %d, truncated = %v", len(lines[0].Text), lines[0].Truncated)
	}
	if lines[1].Text != "next" || lines[1].Truncated {
		t.Errorf("following line = %v", lines[1])
	}
}

func TestStreamLogs_ReplacesInvalidUTF8(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader("2026-01-02T03:04:05Z bad \xff\xfe bytes\n"))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !utf8.ValidString(lines[0].Text) || !strings.Contains(lines[0].Text, "�") {
		t.Errorf("lines = %q", lines)
	}
}

func TestStreamLogs_LineWithoutTimestampKeepsText(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader("notatimestamp rest\n"))

	lines, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Text != "notatimestamp rest" || lines[0].Timestamp == nil {
		t.Errorf("lines = %v", lines)
	}
}

func TestStreamLogs_Options(t *testing.T) {
	since := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	tests := []struct {
		name      string
		req       LogsRequest
		wantTail  string
		wantSince string
		follow    bool
	}{
		{"all lines", LogsRequest{Tail: -1}, "all", "", false},
		{"no existing lines", LogsRequest{Tail: 0, Follow: true}, "0", "", true},
		{"tail", LogsRequest{Tail: 250}, "250", "", false},
		{"since", LogsRequest{Tail: -1, Since: since, Follow: true}, "all", "1767323045.006000000", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
			cli := newLogsAPI(true, strings.NewReader(""))
			tt.req.ApplicationID = "app-1"
			tt.req.ContainerID = webID

			if _, _, err := collectLogs(t, cli, tt.req); err != nil {
				t.Fatal(err)
			}
			opts := cli.logsOpts
			if opts.Tail != tt.wantTail || opts.Since != tt.wantSince || opts.Follow != tt.follow {
				t.Errorf("opts = %+v", opts)
			}
			if !opts.ShowStdout || !opts.ShowStderr || !opts.Timestamps {
				t.Errorf("stdout, stderr and timestamps must be requested: %+v", opts)
			}
		})
	}
}

func TestStreamLogs_ContainerNotOwnedByApplication(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader(""))

	_, started, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: foreign})
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("err = %v", err)
	}
	if started || len(cli.logsCalls) != 0 {
		t.Error("logs of a foreign container must never be requested")
	}
}

func TestStreamLogs_ContainerRemovedBeforeInspect(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader(""))
	delete(cli.results, webID)

	if _, _, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID}); !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestStreamLogs_OpenError(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, nil)
	cli.logsErr = errors.New("configured logging driver does not support reading")

	_, started, err := collectLogs(t, cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID})
	if err == nil || !strings.Contains(err.Error(), "does not support reading") {
		t.Errorf("err = %v", err)
	}
	if started {
		t.Error("started must not be called when the logs could not be opened")
	}
}

func TestStreamLogs_EmitErrorStopsReading(t *testing.T) {
	stubContainers(t, []containerapi.Summary{appSummary(webID, "web", "running")})
	cli := newLogsAPI(true, strings.NewReader("2026-01-02T03:04:05Z a\n2026-01-02T03:04:06Z b\n"))
	stop := errors.New("stop")
	calls := 0

	err := streamLogs(t.Context(), cli, LogsRequest{ApplicationID: "app-1", ContainerID: webID}, func() {}, func(*messages.LogLine) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("err = %v, calls = %d", err, calls)
	}
}
