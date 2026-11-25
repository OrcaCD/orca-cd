package middleware

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
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

// ClientKey collapses IPv6 addresses to their /64 prefix, since a single
// subscriber typically controls a whole /64 and could rotate addresses freely.
func ClientKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if !addr.Is6() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return prefix.String()
}

func (rl *rateLimiter) get(ip string) *rate.Limiter {
	ip = ClientKey(ip)

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
	return rateLimitBy(interval, burst, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

func RateLimitPerUser(interval time.Duration, burst int) gin.HandlerFunc {
	limit := rateLimitBy(interval, burst, func(c *gin.Context) string {
		user, _ := auth.GetUser(c)
		return user.Id
	})

	return func(c *gin.Context) {
		if _, ok := auth.GetUser(c); !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authentication"})
			c.Abort()
			return
		}
		limit(c)
	}
}

func rateLimitBy(interval time.Duration, burst int, key func(*gin.Context) string) gin.HandlerFunc {
	if interval <= 0 {
		interval = defaultInterval
	}
	if burst <= 0 {
		burst = defaultBurst
	}

	rl := newRateLimiter(rate.Every(interval), burst)

	return func(c *gin.Context) {
		if !rl.get(key(c)).Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, please try again later"})
			c.Abort()
			return
		}
		c.Next()
	}
}
