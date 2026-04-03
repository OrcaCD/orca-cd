package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

func TestRateLimit_AllowsWithinBurst(t *testing.T) {
	router := gin.New()
	router.POST("/login", RateLimit(time.Second, 5), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	for i := range 5 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimit_BlocksAfterBurst(t *testing.T) {
	// Interval of one hour means no refill occurs during the test.
	rl := newRateLimiter(rate.Every(time.Hour), 3)

	router := gin.New()
	router.POST("/login", func(c *gin.Context) {
		if !rl.get(c.ClientIP()).Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, please try again later"})
			c.Abort()
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	for range 3 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.2:1234"
		router.ServeHTTP(w, req)
	}

	// 4th request must be rate-limited.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "192.0.2.2:1234"
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimit_IsolatesIPs(t *testing.T) {
	rl := newRateLimiter(rate.Every(time.Hour), 1)

	router := gin.New()
	router.POST("/login", func(c *gin.Context) {
		if !rl.get(c.ClientIP()).Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many requests, please try again later"})
			c.Abort()
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	// Exhaust IP A.
	for range 2 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		router.ServeHTTP(w, req)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("IP A: expected 429, got %d", w.Code)
	}

	// IP B has its own independent bucket.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "192.0.2.11:1234"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("IP B: expected 200, got %d", w.Code)
	}
}

func TestRateLimit_InvalidIntervalFallsBackToDefault(t *testing.T) {
	router := gin.New()
	router.POST("/login", RateLimit(0, 5), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	for i := range 5 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "192.0.2.20:1234"
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "192.0.2.20:1234"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestRateLimit_InvalidBurstFallsBackToDefault(t *testing.T) {
	router := gin.New()
	router.POST("/login", RateLimit(time.Hour, 0), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/login", nil)
	firstReq.RemoteAddr = "192.0.2.21:1234"
	router.ServeHTTP(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/login", nil)
	secondReq.RemoteAddr = "192.0.2.21:1234"
	router.ServeHTTP(second, secondReq)
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", second.Code)
	}
}
