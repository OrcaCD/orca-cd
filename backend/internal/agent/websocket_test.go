package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentdocker "github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type stubSender struct {
	err  error
	sent chan *messages.ClientMessage
}

type recordingMessageConn struct {
	deadline    time.Time
	deadlineErr error
	writeCalled bool
	closed      bool
}

func (c *recordingMessageConn) SetWriteDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.deadlineErr
}

func (c *recordingMessageConn) WriteMessage(int, []byte) error {
	c.writeCalled = true
	return nil
}

func (c *recordingMessageConn) Close() error {
	c.closed = true
	return nil
}

func (s *stubSender) SendMessage(msg *messages.ClientMessage) error {
	if s.sent != nil {
		s.sent <- msg
	}

	return s.err
}

type stubDeployer struct {
	block    chan struct{}
	err      error
	reqCh    chan agentdocker.DeployRequest
	deleteCh chan agentdocker.DeleteRequest
}

func (d *stubDeployer) Remove(_ context.Context, req agentdocker.DeleteRequest) error {
	if d.deleteCh != nil {
		d.deleteCh <- req
	}
	return d.err
}

func (d *stubDeployer) Deploy(ctx context.Context, req agentdocker.DeployRequest) error {
	if d.reqCh != nil {
		d.reqCh <- req
	}
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return d.err
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
	sender := newMessageSender(clientConn, nil)
	handleServerMessage(context.Background(), msg, nil, sender, nil, nil)

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

	sender := newMessageSender(clientConn, nil)
	handleServerMessage(context.Background(), &messages.ServerMessage{}, nil, sender, nil, nil)

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

// signHandshakeInit signs the hub's KeyExchangeInit payload for testing.
func signHandshakeInit(t *testing.T, priv ed25519.PrivateKey, mlkemKey, x25519Key []byte, agentID string) []byte {
	t.Helper()
	return ed25519.Sign(priv, wscrypto.HandshakeSignaturePayload(mlkemKey, x25519Key, agentID))
}

func TestPerformHandshake_Success(t *testing.T) {
	hubKeys, err := wscrypto.GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	hubPub, hubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	const agentID = "agent-1"
	sig := signHandshakeInit(t, hubPriv, hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, agentID)

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		init := &messages.ServerMessage{
			Payload: &messages.ServerMessage_KeyExchangeInit{
				KeyExchangeInit: &messages.KeyExchangeInit{
					MlkemEncapsulationKey: hubKeys.MLKEMEncapKey,
					X25519PublicKey:       hubKeys.X25519PublicKey,
					HubSignature:          sig,
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
	session, err := performHandshake(conn, agentID, hubPub)
	if err != nil {
		t.Fatalf("performHandshake: %v", err)
	}
	if session == nil {
		t.Fatal("expected non-nil session")
	}
}

func TestPerformHandshake_InvalidSignature(t *testing.T) {
	hubKeys, err := wscrypto.GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	hubPub, hubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Sign with the correct key but for a different agentID — signature will not verify.
	badSig := signHandshakeInit(t, hubPriv, hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "different-agent")

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		init := &messages.ServerMessage{
			Payload: &messages.ServerMessage_KeyExchangeInit{
				KeyExchangeInit: &messages.KeyExchangeInit{
					MlkemEncapsulationKey: hubKeys.MLKEMEncapKey,
					X25519PublicKey:       hubKeys.X25519PublicKey,
					HubSignature:          badSig,
				},
			},
		}
		data, _ := proto.Marshal(init)
		serverConn.WriteMessage(websocket.BinaryMessage, data) //nolint:errcheck,gosec
		time.Sleep(500 * time.Millisecond)
	})

	conn := dialServer(t, srv)
	_, err = performHandshake(conn, "agent-1", hubPub)
	if err == nil {
		t.Fatal("expected error for invalid hub signature")
	}
}

func TestPerformHandshake_WrongMessageType(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

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
	_, err = performHandshake(conn, "agent-1", pub)
	if err == nil {
		t.Fatal("expected error when server sends wrong message type")
	}
}

func TestPerformHandshake_InvalidProtoData(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	srv := newTestServer(t, func(serverConn *websocket.Conn) {
		// Send bytes that are not valid protobuf.
		if err := serverConn.WriteMessage(websocket.BinaryMessage, []byte{0xFF, 0xFF, 0xFF}); err != nil {
			t.Errorf("write: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	})

	conn := dialServer(t, srv)
	_, err = performHandshake(conn, "agent-1", pub)
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
	sender := newMessageSender(conn, nil)
	if err := sender.SendMessage(pong); err != nil {
		t.Fatalf("Send: %v", err)
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
	// KeyExchangeResponse is not allowed unencrypted — Send must encrypt it.
	innerMsg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_KeyExchangeResponse{
			KeyExchangeResponse: &messages.KeyExchangeResponse{},
		},
	}
	sender := newMessageSender(conn, session)
	if err := sender.SendMessage(innerMsg); err != nil {
		t.Fatalf("Send: %v", err)
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

func TestSendMessage_SetsWriteDeadline(t *testing.T) {
	conn := &recordingMessageConn{}
	sender := &messageSender{conn: conn}
	before := time.Now()

	err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{Pong: &messages.PongResponse{}},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if !conn.writeCalled {
		t.Fatal("expected WriteMessage to be called")
	}
	if conn.deadline.Before(before.Add(writeWait-time.Second)) || conn.deadline.After(time.Now().Add(writeWait+time.Second)) {
		t.Errorf("write deadline %s is not approximately %s from now", conn.deadline, writeWait)
	}
}

func TestSendMessage_DeadlineErrorClosesConnection(t *testing.T) {
	deadlineErr := errors.New("deadline failed")
	conn := &recordingMessageConn{deadlineErr: deadlineErr}
	sender := &messageSender{conn: conn}

	err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{Pong: &messages.PongResponse{}},
	})
	if !errors.Is(err, deadlineErr) {
		t.Fatalf("SendMessage error = %v, want %v", err, deadlineErr)
	}
	if conn.writeCalled {
		t.Fatal("WriteMessage was called after SetWriteDeadline failed")
	}
	if !conn.closed {
		t.Fatal("connection was not closed after SetWriteDeadline failed")
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

	handleServerMessage(context.Background(), encryptedMsg, session, newMessageSender(conn, session), nil, nil)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to receive pong response")
	}

	if receivedPong == nil || receivedPong.GetPong() == nil {
		t.Fatal("expected a Pong response after handling encrypted Ping")
	}
}

func TestHandleServerMessage_DeployRequest(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubDeployer{reqCh: make(chan agentdocker.DeployRequest, 1)}

	deployMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeployRequest{
			DeployRequest: &messages.DeployRequest{
				RequestId:       "req-1",
				ApplicationId:   "app-1",
				ApplicationName: "billing",
				ComposeFile:     "services:\n  app:\n    image: ghcr.io/orcacd/billing:1.0.0\n",
			},
		},
	}
	env, err := session.Encrypt(deployMsg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	handleServerMessage(context.Background(), &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{
			EncryptedPayload: env,
		},
	}, session, sender, deployer, nil)

	select {
	case req := <-deployer.reqCh:
		if req.ApplicationID != "app-1" {
			t.Fatalf("expected application id %q, got %q", "app-1", req.ApplicationID)
		}
		if req.ApplicationName != "billing" {
			t.Fatalf("expected application name %q, got %q", "billing", req.ApplicationName)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for deploy request")
	}

	select {
	case msg := <-sender.sent:
		result := msg.GetDeployResult()
		if result == nil {
			t.Fatal("expected deploy result payload")
		}
		if !result.Success {
			t.Fatal("expected successful deploy result")
		}
		if result.RequestId != "req-1" {
			t.Fatalf("expected request id %q, got %q", "req-1", result.RequestId)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for deploy result")
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
	handleServerMessage(context.Background(), msg, session, &stubSender{}, nil, nil) // must return without panic
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
	handleServerMessage(context.Background(), msg, session, &stubSender{}, nil, nil) // must drop without panic
}

func TestHandleServerMessage_DropsUnencryptedNonPing(t *testing.T) {
	// KeyExchangeInit is not a Ping and not wrapped in EncryptedPayload — must be dropped.
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_KeyExchangeInit{KeyExchangeInit: &messages.KeyExchangeInit{}},
	}
	handleServerMessage(context.Background(), msg, nil, &stubSender{}, nil, nil) // must return without panic
}

type stubPoller struct {
	mu             sync.Mutex
	snapshots      [][]agentdocker.AppPollConfig
	triggeredAppID string
	triggeredReqID string
	applyCh        chan struct{}
	triggerCh      chan struct{}
}

func (p *stubPoller) ApplySettings(apps []agentdocker.AppPollConfig) {
	p.mu.Lock()
	cp := append([]agentdocker.AppPollConfig(nil), apps...)
	p.snapshots = append(p.snapshots, cp)
	p.mu.Unlock()
	if p.applyCh != nil {
		p.applyCh <- struct{}{}
	}
}

func (p *stubPoller) TriggerNow(appID, appName, requestID string) {
	p.triggeredAppID = appID
	p.triggeredReqID = requestID
	if p.triggerCh != nil {
		p.triggerCh <- struct{}{}
	}
}

func (p *stubPoller) lastSnapshot() []agentdocker.AppPollConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.snapshots) == 0 {
		return nil
	}
	return p.snapshots[len(p.snapshots)-1]
}

func TestHandleServerMessage_AgentSettings(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	poller := &stubPoller{applyCh: make(chan struct{}, 1)}

	settingsMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_AgentSettings{
			AgentSettings: &messages.AgentSettings{
				ImagePollSettings: []*messages.ImagePollSettings{
					{
						ApplicationId:   "app-1",
						ApplicationName: "myapp",
						Enabled:         true,
						IntervalSeconds: 120,
						DeleteOldImages: true,
					},
					{
						ApplicationId:   "app-2",
						ApplicationName: "billing",
						Enabled:         false,
					},
				},
			},
		},
	}
	env, err := session.Encrypt(settingsMsg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	handleServerMessage(context.Background(), &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
	}, session, &stubSender{}, nil, poller)

	select {
	case <-poller.applyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ApplySettings call")
	}

	snap := poller.lastSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected snapshot with 2 entries, got %d", len(snap))
	}

	// Find entries by appID rather than assuming order.
	byID := make(map[string]agentdocker.AppPollConfig, 2)
	for _, entry := range snap {
		byID[entry.AppID] = entry
	}

	u1, ok := byID["app-1"]
	if !ok {
		t.Fatal("missing entry for app-1")
	}
	if u1.AppName != "myapp" {
		t.Errorf("app-1: expected AppName %q, got %q", "myapp", u1.AppName)
	}
	if !u1.Settings.Enabled {
		t.Error("app-1: expected Enabled=true")
	}
	if u1.Settings.IntervalSeconds != 120 {
		t.Errorf("app-1: expected interval 120, got %d", u1.Settings.IntervalSeconds)
	}
	if !u1.Settings.DeleteOldImages {
		t.Error("app-1: expected DeleteOldImages=true")
	}

	u2, ok := byID["app-2"]
	if !ok {
		t.Fatal("missing entry for app-2")
	}
	if u2.AppName != "billing" {
		t.Errorf("app-2: expected AppName %q, got %q", "billing", u2.AppName)
	}
	if u2.Settings.Enabled {
		t.Error("app-2: expected Enabled=false")
	}
}

func TestHandleServerMessage_PullImagesRequest(t *testing.T) {
	sessionKey := make([]byte, 32)
	session, err := wscrypto.NewSession(sessionKey)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	poller := &stubPoller{triggerCh: make(chan struct{}, 1)}

	pullMsg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_PullImagesRequest{
			PullImagesRequest: &messages.PullImagesRequest{
				RequestId:       "req-42",
				ApplicationId:   "app-2",
				ApplicationName: "billing",
			},
		},
	}
	env, err := session.Encrypt(pullMsg)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	handleServerMessage(context.Background(), &messages.ServerMessage{
		Payload: &messages.ServerMessage_EncryptedPayload{EncryptedPayload: env},
	}, session, &stubSender{}, nil, poller)

	select {
	case <-poller.triggerCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TriggerNow call")
	}

	if poller.triggeredAppID != "app-2" {
		t.Errorf("expected appID %q, got %q", "app-2", poller.triggeredAppID)
	}
	if poller.triggeredReqID != "req-42" {
		t.Errorf("expected requestID %q, got %q", "req-42", poller.triggeredReqID)
	}
}

func TestApplyAgentSettings_NilPoller(t *testing.T) {
	// Must not panic when poller is nil.
	applyAgentSettings(nil, &messages.AgentSettings{
		ImagePollSettings: []*messages.ImagePollSettings{
			{ApplicationId: "app-1", ApplicationName: "myapp", Enabled: true},
		},
	}) // must not panic
}

func TestExecutePullImages_NilPoller(t *testing.T) {
	// Must not panic when poller is nil.
	executePullImages(nil, &messages.PullImagesRequest{
		RequestId:       "req-1",
		ApplicationId:   "app-1",
		ApplicationName: "myapp",
	})
}

func TestExecuteDeployment_NilDeployer(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeDeployment(context.Background(), sender, nil, &messages.DeployRequest{
		RequestId:       "req-1",
		ApplicationId:   "app-1",
		ApplicationName: "billing",
		ComposeFile:     "services: {}\n",
	})

	select {
	case msg := <-sender.sent:
		result := msg.GetDeployResult()
		if result == nil {
			t.Fatal("expected DeployResult payload")
		}
		if result.Success {
			t.Error("expected success=false when deployer is nil")
		}
		if result.ErrorMessage == "" {
			t.Error("expected non-empty error message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for deploy result")
	}
}

func TestExecuteDeployment_SerializesSameApplication(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 2)}
	deployer := &stubDeployer{
		reqCh: make(chan agentdocker.DeployRequest, 2),
		block: make(chan struct{}),
	}

	req := &messages.DeployRequest{
		ApplicationId:   "app-1",
		ApplicationName: "billing",
		ComposeFile:     "services: {}\n",
	}

	go executeDeployment(context.Background(), sender, deployer, &messages.DeployRequest{
		RequestId: "req-1", ApplicationId: req.ApplicationId, ApplicationName: req.ApplicationName, ComposeFile: req.ComposeFile,
	})

	select {
	case <-deployer.reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first deploy to start")
	}

	go executeDeployment(context.Background(), sender, deployer, &messages.DeployRequest{
		RequestId: "req-2", ApplicationId: req.ApplicationId, ApplicationName: req.ApplicationName, ComposeFile: req.ComposeFile,
	})

	select {
	case <-deployer.reqCh:
		t.Fatal("second deploy started while the first was still running for the same application")
	case <-time.After(100 * time.Millisecond):
	}

	close(deployer.block)

	select {
	case <-deployer.reqCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second deploy to run after the first completed")
	}

	for range 2 {
		select {
		case <-sender.sent:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for deploy result")
		}
	}
}

type stubReporter struct {
	health map[string]agentdocker.HealthState
}

func (r *stubReporter) ReportApplicationStatus(_ context.Context, sender agentdocker.MessageSender, appIDs []string) {
	statuses := make([]*messages.ApplicationStatus, 0, len(appIDs))
	for _, appID := range appIDs {
		statuses = append(statuses, &messages.ApplicationStatus{
			ApplicationId: appID,
			Health:        r.health[appID].Proto(),
		})
	}
	_ = sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ApplicationStatusReport{
			ApplicationStatusReport: &messages.ApplicationStatusReport{Statuses: statuses},
		},
	})
}

func TestReportApplicationStatus_SendsOneReportForAllApps(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 4)}
	reporter := &stubReporter{health: map[string]agentdocker.HealthState{
		"app-1": agentdocker.HealthHealthy,
		"app-2": agentdocker.HealthUnhealthy,
	}}
	settings := &messages.AgentSettings{ImagePollSettings: []*messages.ImagePollSettings{
		{ApplicationId: "app-1"},
		{ApplicationId: "app-2"},
	}}

	reportApplicationStatus(context.Background(), sender, reporter, settings)

	var msg *messages.ClientMessage
	select {
	case msg = <-sender.sent:
	case <-time.After(time.Second):
		t.Fatal("expected an application status report")
	}

	report := msg.GetApplicationStatusReport()
	if report == nil {
		t.Fatalf("expected ApplicationStatusReport, got %T", msg.Payload)
	}
	if len(report.Statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(report.Statuses))
	}

	// Exactly one message must have been sent.
	select {
	case <-sender.sent:
		t.Fatal("expected exactly one report message")
	default:
	}

	byID := make(map[string]messages.HealthStatus, 2)
	for _, s := range report.Statuses {
		byID[s.ApplicationId] = s.Health
	}
	if byID["app-1"] != messages.HealthStatus_HEALTH_STATUS_HEALTHY {
		t.Errorf("app-1: expected healthy, got %v", byID["app-1"])
	}
	if byID["app-2"] != messages.HealthStatus_HEALTH_STATUS_UNHEALTHY {
		t.Errorf("app-2: expected unhealthy, got %v", byID["app-2"])
	}
}

func TestReportApplicationStatus_NoAppsSendsNothing(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	reportApplicationStatus(context.Background(), sender, &stubReporter{}, &messages.AgentSettings{})

	select {
	case <-sender.sent:
		t.Fatal("expected no message when there are no applications")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestExecuteDelete_Success(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubDeployer{deleteCh: make(chan agentdocker.DeleteRequest, 1)}

	executeDelete(context.Background(), sender, deployer, &messages.DeleteRequest{
		RequestId:       "req-1",
		ApplicationId:   "app-1",
		ApplicationName: "billing",
	})

	select {
	case req := <-deployer.deleteCh:
		if req.ApplicationID != "app-1" || req.ApplicationName != "billing" {
			t.Fatalf("unexpected delete request: %+v", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Remove call")
	}

	select {
	case msg := <-sender.sent:
		result := msg.GetDeleteResult()
		if result == nil {
			t.Fatal("expected DeleteResult payload")
		}
		if !result.Success {
			t.Error("expected success=true")
		}
		if result.RequestId != "req-1" {
			t.Errorf("expected request id %q, got %q", "req-1", result.RequestId)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete result")
	}
}

func TestExecuteDelete_NilDeployer(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}

	executeDelete(context.Background(), sender, nil, &messages.DeleteRequest{
		RequestId:     "req-1",
		ApplicationId: "app-1",
	})

	select {
	case msg := <-sender.sent:
		result := msg.GetDeleteResult()
		if result == nil {
			t.Fatal("expected DeleteResult payload")
		}
		if result.Success {
			t.Error("expected success=false when deployer is nil")
		}
		if result.ErrorMessage == "" {
			t.Error("expected non-empty error message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete result")
	}
}

func TestExecuteDelete_RemoveError(t *testing.T) {
	sender := &stubSender{sent: make(chan *messages.ClientMessage, 1)}
	deployer := &stubDeployer{err: errors.New("remove failed")}

	executeDelete(context.Background(), sender, deployer, &messages.DeleteRequest{
		RequestId:     "req-1",
		ApplicationId: "app-1",
	})

	select {
	case msg := <-sender.sent:
		result := msg.GetDeleteResult()
		if result == nil || result.Success {
			t.Fatalf("expected failed delete result, got %+v", result)
		}
		if result.ErrorMessage == "" {
			t.Error("expected non-empty error message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete result")
	}
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
