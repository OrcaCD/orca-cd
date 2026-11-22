package agent

import (
	"sync"
	"time"
)

// byteBudget is a token bucket, so a burst of output is let through but a
// container that logs without pause cannot saturate the connection.
type byteBudget struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newByteBudget(rate, burst float64) *byteBudget {
	return &byteBudget{rate: rate, burst: burst, tokens: burst}
}

func (b *byteBudget) allow(now time.Time, n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() {
		b.tokens = min(b.burst, b.tokens+max(now.Sub(b.last).Seconds(), 0)*b.rate)
	}
	b.last = now
	if float64(n) > b.tokens {
		return false
	}
	b.tokens -= float64(n)
	return true
}

func (b *byteBudget) refund(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.burst, b.tokens+float64(n))
}
