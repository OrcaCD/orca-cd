package websocket

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

func testLogger() zerolog.Logger {
	return zerolog.New(os.Stderr).Level(zerolog.Disabled)
}

func TestNewHub(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	if h == nil {
		t.Fatal("expected non-nil hub")
	}
	if h.clients == nil {
		t.Fatal("expected non-nil clients map")
	}
	if len(h.clients) != 0 {
		t.Errorf("expected empty clients map, got %d entries", len(h.clients))
	}
}

func TestHub_Register(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	wsConn := newTestWSConn(t)
	defer wsConn.Close() //nolint:errcheck

	client := h.Register("agent-1", wsConn)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Id != "agent-1" {
		t.Errorf("expected client id %q, got %q", "agent-1", client.Id)
	}
	if client.Send == nil {
		t.Fatal("expected non-nil Send channel")
	}

	h.mu.RLock()
	registered, ok := h.clients["agent-1"]
	h.mu.RUnlock()
	if !ok {
		t.Fatal("expected client to be in hub clients map")
	}
	if registered != client {
		t.Error("expected registered client to be the same pointer")
	}
}

func TestHub_Register_ReplacesExisting(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn1 := newTestWSConn(t)
	defer conn1.Close() //nolint:errcheck
	conn2 := newTestWSConn(t)
	defer conn2.Close() //nolint:errcheck

	first := h.Register("agent-1", conn1)
	second := h.Register("agent-1", conn2)

	h.mu.RLock()
	registered := h.clients["agent-1"]
	h.mu.RUnlock()

	if registered == first {
		t.Error("expected hub to have replaced the first client")
	}
	if registered != second {
		t.Error("expected hub to have the second client")
	}
}

func TestHub_Unregister(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck

	h.Register("agent-1", conn)
	h.Unregister("agent-1")

	h.mu.RLock()
	_, ok := h.clients["agent-1"]
	h.mu.RUnlock()
	if ok {
		t.Error("expected client to be removed from hub")
	}
}

func TestHub_Unregister_Nonexistent(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	// Should not panic
	h.Unregister("does-not-exist")
}

func TestHub_Send_ExistingClient(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck

	client := h.Register("agent-1", conn)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 1234},
		},
	}

	ok := h.Send("agent-1", msg)
	if !ok {
		t.Fatal("expected Send to return true for existing client")
	}

	select {
	case received := <-client.Send:
		ping := received.GetPing()
		if ping == nil {
			t.Fatal("expected ping payload")
		}
		if ping.Timestamp != 1234 {
			t.Errorf("expected timestamp 1234, got %d", ping.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message on client channel")
	}
}

func TestHub_Send_NonexistentClient(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 1},
		},
	}

	ok := h.Send("ghost", msg)
	if ok {
		t.Error("expected Send to return false for nonexistent client")
	}
}

func TestHub_Send_FullBuffer(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck

	client := h.Register("agent-1", conn)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 1},
		},
	}

	// Fill the buffer (capacity is 64)
	for range cap(client.Send) {
		client.Send <- msg
	}

	ok := h.Send("agent-1", msg)
	if ok {
		t.Error("expected Send to return false when buffer is full")
	}
}

func TestHub_Broadcast(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	const numClients = 5
	clients := make([]*Client, numClients)
	for i := range numClients {
		conn := newTestWSConn(t)
		defer conn.Close() //nolint:errcheck
		clients[i] = h.Register(strings.Repeat("a", i+1), conn)
	}

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 9999},
		},
	}

	h.Broadcast(msg)

	for i, c := range clients {
		select {
		case received := <-c.Send:
			if received.GetPing().Timestamp != 9999 {
				t.Errorf("client %d: expected timestamp 9999, got %d", i, received.GetPing().Timestamp)
			}
		case <-time.After(time.Second):
			t.Fatalf("client %d: timed out waiting for broadcast", i)
		}
	}
}

func TestHub_Broadcast_SkipsFullBuffer(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	conn := newTestWSConn(t)
	defer conn.Close() //nolint:errcheck

	client := h.Register("agent-full", conn)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 1},
		},
	}

	// Fill the buffer
	for range cap(client.Send) {
		client.Send <- msg
	}

	// Should not block or panic
	h.Broadcast(msg)
}

func TestHub_ConcurrentAccess(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	var wg sync.WaitGroup

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 1},
		},
	}

	// Concurrent registrations
	for i := range 20 {
		wg.Go(func() {
			conn := newTestWSConn(t)
			defer conn.Close() //nolint:errcheck
			id := strings.Repeat("x", i+1)
			h.Register(id, conn)
			h.Send(id, msg)
			h.Unregister(id)
		})
	}

	// Concurrent broadcasts
	for range 10 {
		wg.Go(func() {
			h.Broadcast(msg)
		})
	}

	wg.Wait()
}

func TestHub_WritePump(t *testing.T) {
	log := testLogger()
	h := NewHub(&log)

	// Set up a real WebSocket server/client pair
	serverConn, clientConn := newWSPair(t)
	defer clientConn.Close() //nolint:errcheck

	client := h.Register("agent-wp", serverConn)
	go h.WritePump(client, &log)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 42},
		},
	}

	client.Send <- msg

	// Read on the client side
	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, data, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	received := &messages.ServerMessage{}
	if err := proto.Unmarshal(data, received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.GetPing().Timestamp != 42 {
		t.Errorf("expected timestamp 42, got %d", received.GetPing().Timestamp)
	}

	// Close the send channel to stop WritePump
	client.Close()
}

func TestClient_Close(t *testing.T) {
	c := &Client{
		Id:   "test",
		Send: make(chan *messages.ServerMessage, 1),
	}

	c.Close()

	// Channel should be closed – receiving should return zero value + not ok
	_, ok := <-c.Send
	if ok {
		t.Error("expected Send channel to be closed")
	}
}

func newTestWSConn(t *testing.T) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		// Keep the server connection alive until the test is done
		select {}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("failed to dial test WS server: %v", err)
	}
	return conn
}

func newWSPair(t *testing.T) (serverConn, clientConn *websocket.Conn) {
	t.Helper()

	serverReady := make(chan *websocket.Conn, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("server upgrade failed: %v", err)
			return
		}
		serverReady <- conn
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cConn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("client dial failed: %v", err)
	}

	select {
	case sConn := <-serverReady:
		return sConn, cConn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server connection")
		return nil, nil
	}
}
