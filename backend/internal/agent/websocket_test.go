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
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
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
	handleServerMessage(msg, clientConn, nil)

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

	handleServerMessage(&messages.ServerMessage{}, clientConn, nil)

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() {
		defer close(done)
		u := "ws" + strings.TrimPrefix(srv.URL, "http")
		conn, err := connectWithRetry(ctx, u, "Bearer test-token")
		if err == nil {
			conn.Close() //nolint:errcheck,gosec
			t.Errorf("expected error on context cancellation, got nil")
		}
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connectWithRetry did not return after context cancellation")
	}
}

func TestConnectWithRetry_PreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cancel()

	conn, err := connectWithRetry(ctx, "ws://localhost:1", "Bearer test-token")
	if err == nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatal("expected error for pre-cancelled context, got nil")
	}
	if conn != nil {
		t.Errorf("expected nil conn, got non-nil")
	}
}

func TestPerformHandshake_Success(t *testing.T) {
	hubKeys, err := wscrypto.GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		init := &messages.ServerMessage{
			Payload: &messages.ServerMessage_KeyExchangeInit{
				KeyExchangeInit: &messages.KeyExchangeInit{
					MlkemEncapsulationKey: hubKeys.MLKEMEncapKey,
					X25519PublicKey:       hubKeys.X25519PublicKey,
				},
			},
		}
		data, _ := proto.Marshal(init)
		if err := serverConn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			t.Errorf("write init: %v", err)
			return
		}
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck,gosec
		_, respData, err := serverConn.ReadMessage()
		if err != nil {
			t.Errorf("read response: %v", err)
			return
		}
		clientMsg := &messages.ClientMessage{}
		if err := proto.Unmarshal(respData, clientMsg); err != nil {
			t.Errorf("unmarshal response: %v", err)
			return
		}
		resp := clientMsg.GetKeyExchangeResponse()
		if resp == nil {
			t.Error("expected KeyExchangeResponse")
		}
	})

	conn := dialServer(t, srv)
	session, err := performHandshake(conn, "agent-1")
	if err != nil {
		t.Fatalf("performHandshake: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
}

func TestPerformHandshake_WrongMessageType(t *testing.T) {
	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		// Send a Ping instead of KeyExchangeInit.
		msg := &messages.ServerMessage{
			Payload: &messages.ServerMessage_Ping{Ping: &messages.PingRequest{Timestamp: 1}},
		}
		data, _ := proto.Marshal(msg)
		if err := serverConn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			t.Errorf("write: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	})

	conn := dialServer(t, srv)
	_, err := performHandshake(conn, "agent-1")
	if err == nil {
		t.Fatal("expected error when server sends wrong message type")
	}
}

func TestPerformHandshake_InvalidProtoData(t *testing.T) {
	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		// Send bytes that are not valid protobuf.
		if err := serverConn.WriteMessage(websocket.BinaryMessage, []byte{0xFF, 0xFF, 0xFF}); err != nil {
			t.Errorf("write: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	})

	conn := dialServer(t, srv)
	_, err := performHandshake(conn, "agent-1")
	if err == nil {
		t.Fatal("expected error for invalid protobuf data")
	}
}

func TestSendMessage_AllowedUnencrypted(t *testing.T) {
	var received *messages.ClientMessage
	done := make(chan struct{})

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		defer close(done)
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck,gosec
		_, data, err := serverConn.ReadMessage()
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}
		msg := &messages.ClientMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		received = msg
	})

	conn := dialServer(t, srv)
	pong := &messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{Pong: &messages.PongResponse{Timestamp: 42}},
	}
	if err := sendMessage(conn, nil, pong); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive message")
	}

	if received == nil || received.GetPong() == nil {
		t.Fatal("expected pong message to arrive unencrypted")
	}
	if received.GetPong().Timestamp != 42 {
		t.Errorf("expected timestamp 42, got %d", received.GetPong().Timestamp)
	}
}

func TestSendMessage_Encrypted(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var received *messages.ClientMessage
	done := make(chan struct{})

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		defer close(done)
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck,gosec
		_, data, err := serverConn.ReadMessage()
		if err != nil {
			t.Errorf("read: %v", err)
			return
		}
		msg := &messages.ClientMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Errorf("unmarshal: %v", err)
			return
		}
		received = msg
	})

	conn := dialServer(t, srv)
	// KeyExchangeResponse is not allowed unencrypted — sendMessage must encrypt it.
	innerMsg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_KeyExchangeResponse{
			KeyExchangeResponse: &messages.KeyExchangeResponse{},
		},
	}
	if err := sendMessage(conn, session, innerMsg); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive message")
	}

	if received == nil || received.GetEncryptedPayload() == nil {
		t.Fatal("expected encrypted payload on the wire")
	}
}

func TestHandleServerMessage_EncryptedPing(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var receivedPong *messages.ClientMessage
	done := make(chan struct{})

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		defer close(done)
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck,gosec
		_, data, err := serverConn.ReadMessage()
		if err != nil {
			t.Errorf("read pong: %v", err)
			return
		}
		msg := &messages.ClientMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Errorf("unmarshal pong: %v", err)
			return
		}
		receivedPong = msg
	})

	conn := dialServer(t, srv)

	pingMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{Ping: &messages.PingRequest{Timestamp: time.Now().UnixMilli()}},
	}
	env, err := session.Encrypt(pingMsg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	encryptedMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
	}

	handleServerMessage(encryptedMsg, conn, session)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive pong response")
	}

	if receivedPong == nil || receivedPong.GetPong() == nil {
		t.Fatal("expected a Pong response after handling encrypted Ping")
	}
}

func TestHandleServerMessage_EncryptedDecryptError(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Invalid ciphertext — AEGIS authentication will fail.
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{
			EncryptedPayload: &messages.EncryptedPayload{
				Nonce:      make([]byte, 32),
				Ciphertext: []byte{0x01, 0x02},
			},
		},
	}
	handleServerMessage(msg, nil, session) // must return without panic
}

func TestHandleServerMessage_DoublyEncrypted(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Inner payload is itself an EncryptedPayload — should be dropped after one unwrap.
	innerMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{
			EncryptedPayload: &messages.EncryptedPayload{Nonce: make([]byte, 32), Ciphertext: []byte{0x01}},
		},
	}
	env, err := session.Encrypt(innerMsg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
	}
	handleServerMessage(msg, nil, session) // must drop without panic
}

func TestHandleServerMessage_DropsUnencryptedNonPing(t *testing.T) {
	// KeyExchangeInit is not a Ping and not wrapped in EncryptedPayload — must be dropped.
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_KeyExchangeInit{KeyExchangeInit: &messages.KeyExchangeInit{}},
	}
	handleServerMessage(msg, nil, nil) // must return without panic
}

func TestConnTracker_CloseNilConn(t *testing.T) {
	var tracker connTracker
	tracker.close()
}

func TestConnTracker_SetAndClose(t *testing.T) {
	serverDone := make(chan struct{})
	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		defer close(serverDone)
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck,gosec
		_, _, _ = serverConn.ReadMessage()
	})

	clientConn := dialServer(t, srv)

	var tracker connTracker
	tracker.setAndCancelled(context.Background(), clientConn)
	tracker.close()

	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not detect connection close")
	}
}
