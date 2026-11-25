package agent

import (
	"sync"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

const (
	maxLogChunkBytes = 128 << 10 // 128 KiB
	logLineOverhead  = 32        // bytes counted per line on top of its text
)

// logBatcher collects lines into chunks. The mutex is held while sending, which
// keeps the chunks in order and slows the reader down when the connection is slow.
type logBatcher struct {
	sender    outboundSender
	requestID string
	budget    *byteBudget // this stream
	total     *byteBudget // all streams of the agent
	now       func() time.Time

	mu      sync.Mutex
	lines   []*messages.LogLine
	size    int
	dropped int64
}

func (b *logBatcher) add(line *messages.LogLine) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cost := len(line.Text) + logLineOverhead
	if b.budget != nil && !b.admit(cost) {
		b.dropped++
		return nil
	}
	if len(b.lines) > 0 && b.size+cost > maxLogChunkBytes {
		if err := b.flushLocked(); err != nil {
			return err
		}
	}
	b.lines = append(b.lines, line)
	b.size += cost
	return nil
}

func (b *logBatcher) admit(cost int) bool {
	now := b.now()
	if !b.budget.allow(now, cost) {
		return false
	}
	if b.total != nil && !b.total.allow(now, cost) {
		b.budget.refund(cost)
		return false
	}
	return true
}

func (b *logBatcher) flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushLocked()
}

func (b *logBatcher) flushLocked() error {
	if len(b.lines) == 0 && b.dropped == 0 {
		return nil
	}
	chunk := &messages.ContainerLogsChunk{RequestId: b.requestID, Lines: b.lines, DroppedLines: b.dropped}
	b.lines, b.size, b.dropped = nil, 0, 0
	return b.sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ContainerLogsChunk{ContainerLogsChunk: chunk},
	})
}
