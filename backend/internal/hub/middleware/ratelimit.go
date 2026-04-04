package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	staleEntryAfter = 10 * time.Minute
	cleanupInterval = 5 * time.Minute
	defaultInterval = 1 * time.Second
	defaultBurst    = 1
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	limiters  map[string]*ipLimiter
	r         rate.Limit
	burst     int
	lastSweep time.Time
}

func newRateLimiter(r rate.Limit, burst int) *rateLimiter {
	return &rateLimiter{
		limiters:  make(map[string]*ipLimiter),
		r:         r,
		burst:     burst,
		lastSweep: time.Now(),
	}
}

func (rl *rateLimiter) get(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.cleanupStaleLocked(time.Now())

	entry, ok := rl.limiters[ip]
	if !ok {
		entry = &ipLimiter{limiter: rate.NewLimiter(rl.r, rl.burst)}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

// cleanupStaleLocked removes IP entries not seen recently.
// Callers must hold the mutex
func (rl *rateLimiter) cleanupStaleLocked(now time.Time) {
	if now.Sub(rl.lastSweep) < cleanupInterval {
		return
	}
	for ip, entry := range rl.limiters {
		if now.Sub(entry.lastSeen) > staleEntryAfter {
			delete(rl.limiters, ip)
		}
	}
	rl.lastSweep = now
}

// RateLimit returns a per-IP token-bucket rate limiting middleware.
// interval is the time between token refills; burst is the maximum burst size.
func RateLimit(interval time.Duration, burst int) gin.HandlerFunc {
	if interval <= 0 {
		interval = defaultInterval
	}
	if burst <= 0 {
		burst = defaultBurst
	}

	rl := newRateLimiter(rate.Every(interval), burst)

	return func(c *gin.Context) {
		if !rl.get(c.ClientIP()).Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, please try again later"})
			c.Abort()
			return
		}
		c.Next()
	}
}
