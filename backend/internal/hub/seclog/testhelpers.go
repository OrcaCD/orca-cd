package seclog

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

type Capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func CaptureForTest(t *testing.T) *Capture {
	t.Helper()

	c := &Capture{}
	previous := current.Load()
	Init(zerolog.New(c))
	t.Cleanup(func() { current.Store(previous) })
	return c
}

func (c *Capture) Has(event, reason string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for line := range bytes.SplitSeq(c.buf.Bytes(), []byte("\n")) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if entry["event"] == event && (reason == "" || entry["reason"] == reason) {
			return true
		}
	}
	return false
}

func (c *Capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
