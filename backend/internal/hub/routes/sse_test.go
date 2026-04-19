package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"
)

func newTestSSEBroker(t *testing.T) *sse.Broker {
	t.Helper()
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	broker := sse.NewBroker(&log)
	sse.DefaultBroker = broker
	t.Cleanup(func() { sse.DefaultBroker = nil })
	return broker
}

// injectClaimsMiddleware sets valid auth claims so SSEHandler can proceed.
func injectClaimsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := &auth.UserClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Name:  "Test User",
			Email: "test@example.com",
			Role:  "admin",
		}
		auth.SetClaims(c, claims)
		c.Next()
	}
}

func newSSETestServer(t *testing.T) *httptest.Server {
	t.Helper()
	router := gin.New()
	router.GET("/api/v1/events", injectClaimsMiddleware(), SSEHandler)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server
}

// connectSSE opens an SSE connection and returns a line scanner and cancel func.
// Body cleanup is registered via t.Cleanup.
func connectSSE(t *testing.T, server *httptest.Server) (*bufio.Scanner, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("failed to create SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose
	if err != nil {
		cancel()
		t.Fatalf("failed to connect to SSE endpoint: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return bufio.NewScanner(resp.Body), cancel
}

// waitForInitialComment reads lines until the ": connected" SSE comment is seen.
func waitForInitialComment(t *testing.T, scanner *bufio.Scanner) {
	t.Helper()
	for scanner.Scan() {
		if scanner.Text() == ": connected" {
			return
		}
	}
	t.Fatal("did not receive initial SSE comment")
}

// TestSSEHandler_Headers uses httptest.ResponseRecorder so hop-by-hop headers
// like Connection are not stripped by the HTTP transport layer.
func TestSSEHandler_Headers(t *testing.T) {
	newTestSSEBroker(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := gin.New()
	router.GET("/api/v1/events", injectClaimsMiddleware(), SSEHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(w, req)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSEHandler did not exit after context cancel")
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type %q, got %q", "text/event-stream", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache, no-transform" {
		t.Errorf("expected Cache-Control %q, got %q", "no-cache, no-transform", cc)
	}
	if conn := w.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected Connection %q, got %q", "keep-alive", conn)
	}
	if buf := w.Header().Get("X-Accel-Buffering"); buf != "no" {
		t.Errorf("expected X-Accel-Buffering %q, got %q", "no", buf)
	}
}

func TestSSEHandler_InitialComment(t *testing.T) {
	newTestSSEBroker(t)
	server := newSSETestServer(t)

	scanner, cancel := connectSSE(t, server)
	defer cancel()

	waitForInitialComment(t, scanner)
}

func TestSSEHandler_ReceivesEvent(t *testing.T) {
	broker := newTestSSEBroker(t)
	server := newSSETestServer(t)

	scanner, cancel := connectSSE(t, server)
	defer cancel()

	waitForInitialComment(t, scanner)

	broker.Publish(sse.Event{Type: sse.EventTypeUpdate, URL: "http://example.com"})

	var eventType, eventData string
	for scanner.Scan() {
		line := scanner.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			eventData = v
			break
		}
	}

	if eventType != string(sse.EventTypeUpdate) {
		t.Errorf("expected event type %q, got %q", sse.EventTypeUpdate, eventType)
	}

	var event sse.Event
	if err := json.Unmarshal([]byte(eventData), &event); err != nil {
		t.Fatalf("failed to unmarshal SSE event data: %v", err)
	}
	if event.URL != "http://example.com" {
		t.Errorf("expected URL %q, got %q", "http://example.com", event.URL)
	}
}
