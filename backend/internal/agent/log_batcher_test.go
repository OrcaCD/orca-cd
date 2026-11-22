package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

type chunkRecorder struct {
	mu     sync.Mutex
	chunks []*messages.ContainerLogsChunk
	err    error
}

func (r *chunkRecorder) SendMessage(msg *messages.ClientMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.chunks = append(r.chunks, msg.GetContainerLogsChunk())
	return nil
}

func (r *chunkRecorder) sent() []*messages.ContainerLogsChunk {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*messages.ContainerLogsChunk(nil), r.chunks...)
}

func fixedNow() time.Time { return budgetEpoch }

// lineCost is what a 100 byte line is charged.
const lineCost = 100 + logLineOverhead

func hundredBytes(label string) *messages.LogLine {
	return logLine(label + strings.Repeat("x", 100-len(label)))
}

func TestLogBatcher_FlushSendsOneChunkWithTheRequestID(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}

	for _, text := range []string{"a", "b", "c"} {
		if err := b.add(logLine(text)); err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.sent()) != 0 {
		t.Fatal("nothing may be sent before the flush")
	}
	if err := b.flush(); err != nil {
		t.Fatal(err)
	}

	chunks := sender.sent()
	if len(chunks) != 1 || chunks[0].RequestId != "r1" || len(chunks[0].Lines) != 3 {
		t.Fatalf("chunks = %v", chunks)
	}
	for i, want := range []string{"a", "b", "c"} {
		if chunks[0].Lines[i].Text != want {
			t.Errorf("line %d = %q, want %q", i, chunks[0].Lines[i].Text, want)
		}
	}
}

func TestLogBatcher_FlushWithoutLinesSendsNothing(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}

	if err := b.flush(); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent()) != 0 {
		t.Errorf("chunks = %v", sender.sent())
	}
}

func TestLogBatcher_FlushStartsANewChunk(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}

	_ = b.add(logLine("first"))
	_ = b.flush()
	_ = b.flush()
	_ = b.add(logLine("second"))
	_ = b.flush()

	chunks := sender.sent()
	if len(chunks) != 2 || chunks[0].Lines[0].Text != "first" || chunks[1].Lines[0].Text != "second" {
		t.Fatalf("chunks = %v", chunks)
	}
	if len(chunks[1].Lines) != 1 {
		t.Errorf("a sent line must not be sent again: %v", chunks[1].Lines)
	}
}

func TestLogBatcher_SplitsChunksBeforeTheyGetTooLarge(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}
	perChunk := maxLogChunkBytes / lineCost

	for i := range perChunk + 1 {
		if err := b.add(hundredBytes(fmt.Sprintf("%05d", i))); err != nil {
			t.Fatal(err)
		}
	}

	chunks := sender.sent()
	if len(chunks) != 1 || len(chunks[0].Lines) != perChunk {
		t.Fatalf("expected one full chunk of %d lines before the flush, got %d chunks", perChunk, len(chunks))
	}
	_ = b.flush()
	chunks = sender.sent()
	if len(chunks) != 2 || len(chunks[1].Lines) != 1 || !strings.HasPrefix(chunks[1].Lines[0].Text, fmt.Sprintf("%05d", perChunk)) {
		t.Errorf("the line that did not fit must start the next chunk: %v", chunks[1].Lines)
	}
}

func TestLogBatcher_LineLargerThanAChunkIsSentOnItsOwn(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}

	_ = b.add(logLine("small"))
	_ = b.add(logLine(strings.Repeat("x", maxLogChunkBytes+1)))
	_ = b.flush()

	chunks := sender.sent()
	if len(chunks) != 2 || len(chunks[0].Lines) != 1 || len(chunks[1].Lines) != 1 {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestLogBatcher_WithoutABudgetNothingIsDropped(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}

	for range 1000 {
		_ = b.add(hundredBytes("x"))
	}
	_ = b.flush()

	var lines int
	for _, chunk := range sender.sent() {
		lines += len(chunk.Lines)
		if chunk.DroppedLines != 0 {
			t.Errorf("dropped = %d", chunk.DroppedLines)
		}
	}
	if lines != 1000 {
		t.Errorf("lines = %d", lines)
	}
}

func TestLogBatcher_DroppedLinesAreReportedWithTheNextChunk(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1", now: fixedNow, budget: newByteBudget(0, 2*lineCost)}

	for range 5 {
		_ = b.add(hundredBytes("x"))
	}
	_ = b.flush()
	_ = b.flush()

	chunks := sender.sent()
	if len(chunks) != 1 || len(chunks[0].Lines) != 2 || chunks[0].DroppedLines != 3 {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestLogBatcher_DroppedLinesAloneStillGetReported(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1", now: fixedNow, budget: newByteBudget(0, 0)}

	_ = b.add(hundredBytes("x"))
	_ = b.add(hundredBytes("x"))
	_ = b.flush()
	_ = b.flush()

	chunks := sender.sent()
	if len(chunks) != 1 || len(chunks[0].Lines) != 0 || chunks[0].DroppedLines != 2 {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestLogBatcher_AdmitChargesTheStreamBudget(t *testing.T) {
	b := &logBatcher{now: fixedNow, budget: newByteBudget(0, 2*lineCost)}

	for i := range 2 {
		if !b.admit(lineCost) {
			t.Fatalf("line %d fits the stream budget", i)
		}
	}
	if b.admit(lineCost) {
		t.Error("the third line must be refused")
	}
}

func TestLogBatcher_AdmitChargesTheSharedBudgetToo(t *testing.T) {
	total := newByteBudget(0, 3*lineCost)
	first := &logBatcher{now: fixedNow, budget: newByteBudget(0, 10*lineCost), total: total}
	second := &logBatcher{now: fixedNow, budget: newByteBudget(0, 10*lineCost), total: total}

	admitted := 0
	for range 5 {
		if first.admit(lineCost) {
			admitted++
		}
		if second.admit(lineCost) {
			admitted++
		}
	}

	if admitted != 3 {
		t.Errorf("admitted %d lines, the shared budget only has room for 3", admitted)
	}
}

func TestLogBatcher_LineRefusedByTheSharedBudgetIsNotChargedToTheStream(t *testing.T) {
	stream := newByteBudget(0, 2*lineCost)
	total := newByteBudget(0, 0)
	b := &logBatcher{now: fixedNow, budget: stream, total: total}

	for range 10 {
		if b.admit(lineCost) {
			t.Fatal("the shared budget is empty")
		}
	}

	for i := range 2 {
		if !stream.allow(budgetEpoch, lineCost) {
			t.Errorf("refused lines must leave the stream budget untouched, line %d was refused", i)
		}
	}
}

func TestLogBatcher_SendErrorsAreReturned(t *testing.T) {
	sendErr := errors.New("connection closed")

	t.Run("flush", func(t *testing.T) {
		b := &logBatcher{sender: &chunkRecorder{err: sendErr}, requestID: "r1"}
		_ = b.add(logLine("a"))
		if err := b.flush(); !errors.Is(err, sendErr) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("add when a full chunk is sent", func(t *testing.T) {
		b := &logBatcher{sender: &chunkRecorder{err: sendErr}, requestID: "r1"}
		_ = b.add(logLine(strings.Repeat("x", maxLogChunkBytes-logLineOverhead)))
		if err := b.add(logLine("one more")); !errors.Is(err, sendErr) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLogBatcher_ConcurrentAddAndFlushKeepOrderAndLoseNothing(t *testing.T) {
	sender := &chunkRecorder{}
	b := &logBatcher{sender: sender, requestID: "r1"}
	const total = 5000

	stop := make(chan struct{})
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		for {
			select {
			case <-stop:
				return
			default:
				_ = b.flush()
			}
		}
	}()
	for i := range total {
		if err := b.add(logLine(fmt.Sprintf("%05d", i))); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-flushed
	if err := b.flush(); err != nil {
		t.Fatal(err)
	}

	next := 0
	for _, chunk := range sender.sent() {
		for _, line := range chunk.Lines {
			if line.Text != fmt.Sprintf("%05d", next) {
				t.Fatalf("line %d arrived as %q", next, line.Text)
			}
			next++
		}
	}
	if next != total {
		t.Errorf("got %d lines, want %d", next, total)
	}
}
