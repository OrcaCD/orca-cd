package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTimeoutMiddleware_SetsDeadlineOnRequestContext(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(TimeoutMiddleware(100 * time.Millisecond))
	router.GET("/", func(c *gin.Context) {
		deadline, ok := c.Request.Context().Deadline()
		if !ok {
			t.Fatal("expected context deadline to be set")
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("expected positive remaining deadline, got %v", remaining)
		}
		if remaining > 100*time.Millisecond {
			t.Fatalf("expected remaining deadline <= timeout, got %v", remaining)
		}

		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTimeoutMiddleware_ContextExpiresForSlowHandler(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(TimeoutMiddleware(20 * time.Millisecond))
	router.GET("/", func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			if c.Request.Context().Err() != context.DeadlineExceeded {
				t.Fatalf("expected deadline exceeded, got %v", c.Request.Context().Err())
			}
			c.Status(http.StatusGatewayTimeout)
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected request context to timeout before handler sleep finished")
		}
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}
}

func TestTimeoutMiddleware_SkipsWebSocketRequests(t *testing.T) {
	t.Parallel()

	for _, upgradeVal := range []string{"websocket", "WebSocket", "WEBSOCKET"} {
		t.Run(upgradeVal, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.Use(TimeoutMiddleware(100 * time.Millisecond))
			router.GET("/ws", func(c *gin.Context) {
				_, ok := c.Request.Context().Deadline()
				if ok {
					t.Fatal("expected no deadline for WebSocket request")
				}
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.Header.Set("Upgrade", upgradeVal)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
		})
	}
}

func TestTimeoutMiddleware_SkipsSSERequests(t *testing.T) {
	t.Parallel()

	for _, acceptVal := range []string{"text/event-stream", "text/event-stream, text/html"} {
		t.Run(acceptVal, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.Use(TimeoutMiddleware(100 * time.Millisecond))
			router.GET("/events", func(c *gin.Context) {
				_, ok := c.Request.Context().Deadline()
				if ok {
					t.Fatal("expected no deadline for SSE request")
				}
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/events", nil)
			req.Header.Set("Accept", acceptVal)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
		})
	}
}

func TestTimeoutMiddleware_CancelsContextAfterHandlerReturns(t *testing.T) {
	t.Parallel()

	var reqCtx context.Context

	router := gin.New()
	router.Use(TimeoutMiddleware(time.Second))
	router.GET("/", func(c *gin.Context) {
		reqCtx = c.Request.Context()
		if err := reqCtx.Err(); err != nil {
			t.Fatalf("did not expect canceled context inside handler, got %v", err)
		}
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if reqCtx == nil {
		t.Fatal("expected request context to be captured")
	}
	if err := reqCtx.Err(); err != context.Canceled {
		t.Fatalf("expected canceled context after handler return, got %v", err)
	}
}
