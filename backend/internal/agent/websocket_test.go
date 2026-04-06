package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// newTestServer starts a test WebSocket server. The handler is called for each
// accepted connection. The server is closed when the test ends.
func newTestServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		defer conn.Close() //nolint:errcheck
		handler(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dialServer(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck,gosec
	return conn
}

func marshalServer(t *testing.T, msg *messages.ServerMessage) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	return data
}

func TestHandleServerMessage_Ping(t *testing.T) {
	pingts := time.Now().UnixMilli()

	var receivedPong *messages.PongResponse
	done := make(chan struct{})

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		defer close(done)
		ping := &messages.ServerMessage{
			Payload: &messages.ServerMessage_Ping{
				Ping: &messages.PingRequest{Timestamp: pingts},
			},
		}
		if err := serverConn.WriteMessage(websocket.BinaryMessage, marshalServer(t, ping)); err != nil {
			t.Errorf("write ping: %v", err)
			return
		}
		if err := serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil { //nolint:errcheck
			t.Errorf("set read deadline: %v", err)
			return
		}
		_, data, err := serverConn.ReadMessage()
		if err != nil {
			t.Errorf("read pong: %v", err)
			return
		}
		resp := &messages.ClientMessage{}
		if err := proto.Unmarshal(data, resp); err != nil {
			t.Errorf("unmarshal pong: %v", err)
			return
		}
		pong, ok := resp.Payload.(*messages.ClientMessage_Pong)
		if !ok {
			t.Errorf("expected Pong payload, got %T", resp.Payload)
			return
		}
		receivedPong = pong.Pong
	})

	clientConn := dialServer(t, srv)
	defer clientConn.Close() //nolint:errcheck

	// Read the Ping on the client side and call the handler under test.
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, data, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("client read ping: %v", err)
	}
	msg := &messages.ServerMessage{}
	if err := proto.Unmarshal(data, msg); err != nil {
		t.Fatalf("unmarshal ping: %v", err)
	}
	handleServerMessage(msg, clientConn)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive pong")
	}

	if receivedPong == nil {
		t.Fatal("never received a Pong")
	}
	if receivedPong.Timestamp < pingts {
		t.Errorf("pong timestamp %d is before ping timestamp %d", receivedPong.Timestamp, pingts)
	}
}

func TestHandleServerMessage_UnknownPayload(t *testing.T) {
	writeReceived := make(chan struct{}, 1)

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		if err := serverConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			t.Errorf("set read deadline: %v", err)
			return
		}
		if _, _, err := serverConn.ReadMessage(); err == nil {
			writeReceived <- struct{}{}
		}
	})

	clientConn := dialServer(t, srv)
	defer clientConn.Close() //nolint:errcheck

	handleServerMessage(&messages.ServerMessage{}, clientConn)

	select {
	case <-writeReceived:
		t.Error("expected no response for unknown message, but server received one")
	case <-time.After(400 * time.Millisecond):
		// expected: nothing written
	}
}

func TestConnectWithRetry_Success(t *testing.T) {
	srv := newTestServer(t, func(conn *websocket.Conn) {
		time.Sleep(200 * time.Millisecond)
	})

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, err := connectWithRetry(context.Background(), u, "Bearer test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection")
	}
	conn.Close() //nolint:errcheck,gosec
}

func TestConnectWithRetry_AuthHeader(t *testing.T) {
	const token = "Bearer super-secret"
	gotHeader := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get("Authorization")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, err := connectWithRetry(context.Background(), u, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	conn.Close() //nolint:errcheck,gosec

	select {
	case h := <-gotHeader:
		if h != token {
			t.Errorf("expected Authorization %q, got %q", token, h)
		}
	case <-time.After(time.Second):
		t.Error("server never received a request")
	}
}

func TestConnectWithRetry_RetriesBeforeSuccess(t *testing.T) {
	const wantAttempts = 3
	var attempt atomic.Int32

	// The handler is an HTTP handler (not a WebSocket handler) so that we can
	// reject the upgrade request with a 503 on early attempts. This causes
	// websocket.DefaultDialer.Dial to return an error, which is the condition
	// connectWithRetry retries on.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempt.Add(1)
		if n < wantAttempts {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		defer conn.Close() //nolint:errcheck
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, err := connectWithRetry(context.Background(), u, "Bearer test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn == nil {
		t.Fatal("expected non-nil connection after retries")
	}
	conn.Close() //nolint:errcheck,gosec

	if got := attempt.Load(); got < wantAttempts {
		t.Errorf("expected at least %d attempts, got %d", wantAttempts, got)
	}
}

func TestConnectWithRetry_ContextCancelled(t *testing.T) {
	// Server that is never ready — every request gets a 503 so the dialer keeps retrying.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		u := "ws" + strings.TrimPrefix(srv.URL, "http")
		conn, err := connectWithRetry(ctx, u, "Bearer test-token")
		if err == nil {
			conn.Close() //nolint:errcheck
			t.Errorf("expected error on context cancellation, got nil")
		}
	}()

	// Give the goroutine time to start its first retry sleep, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connectWithRetry did not return after context cancellation")
	}
}
