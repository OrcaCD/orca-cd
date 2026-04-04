package websocket

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const (
	handlerTestSecret = "test-secret-that-is-long-enough-32chars"
	handlerTestURL    = "http://localhost:8080"
)

func setupHandlerTestEnv(t *testing.T) {
	t.Helper()

	if err := crypto.Init(handlerTestSecret); err != nil {
		t.Fatalf("failed to init crypto: %v", err)
	}
	if err := auth.Init(handlerTestSecret, handlerTestURL); err != nil {
		t.Fatalf("failed to init auth: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormlogger.New(
			log.New(os.Stderr, "\n", log.LstdFlags),
			gormlogger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  gormlogger.Warn,
				IgnoreRecordNotFoundError: true,
			},
		),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.Agent{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	sqlDB, err := testDB.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		db.DB = nil
	})

	db.DB = testDB
}

func createTestAgent(t *testing.T, keyId string) *models.Agent {
	t.Helper()
	agent := &models.Agent{
		Name:  crypto.EncryptedString("test-agent"),
		KeyId: crypto.EncryptedString(keyId),
	}
	if err := db.DB.Create(agent).Error; err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	// Restore plain-text values so callers can use them directly.
	agent.Name = crypto.EncryptedString("test-agent")
	agent.KeyId = crypto.EncryptedString(keyId)
	return agent
}

func newHandlerTestServer(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	r := gin.New()
	r.GET("/ws", WsHandler(h, &log))
	server := httptest.NewServer(r)
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})
	return server
}

func dialWS(server *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", token)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	return websocket.DefaultDialer.Dial(wsURL, headers)
}

// waitForOffline polls the DB until the agent status is Offline or the timeout expires.
func waitForOffline(t *testing.T, agentID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var dbAgent models.Agent
		if err := db.DB.First(&dbAgent, "id = ?", agentID).Error; err != nil {
			return // DB may already be closed during cleanup; that's fine.
		}
		if dbAgent.Status == models.AgentStatusOffline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("timed out waiting for agent %s to go offline", agentID)
}

func TestWsHandler_MissingAuthHeader(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	_, resp, err := dialWS(server, "")
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %v", resp)
	}
}

func TestWsHandler_InvalidToken(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	_, resp, err := dialWS(server, "not-a-valid-jwt")
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %v", resp)
	}
}

func TestWsHandler_AgentNotFound(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	// Valid token but no matching record in DB.
	ghost := &models.Agent{
		Base:  models.Base{Id: "nonexistent-agent-id"},
		KeyId: crypto.EncryptedString("some-key"),
	}
	token, err := auth.GenerateAgentToken(ghost)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, resp, dialErr := dialWS(server, token)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if dialErr == nil {
		t.Fatal("expected dial error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %v", resp)
	}
}

func TestWsHandler_KeyIdMismatch(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	agent := createTestAgent(t, "actual-key-id")

	// Token claims a different KeyId than what is stored.
	tokenAgent := &models.Agent{
		Base:  models.Base{Id: agent.Id},
		KeyId: crypto.EncryptedString("wrong-key-id"),
	}
	token, err := auth.GenerateAgentToken(tokenAgent)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, resp, dialErr := dialWS(server, token)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if dialErr == nil {
		t.Fatal("expected dial error, got nil")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %v", resp)
	}
}

func TestWsHandler_Success_ReceivesPing(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	agent := createTestAgent(t, "key-id-1")
	token, err := auth.GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	conn, resp, err := dialWS(server, token)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("expected successful WS connection, got: %v", err)
	}

	// Wait for the server to register the client (with timeout).
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 12345},
		},
	}
	var sent bool
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sent = h.Send(agent.Id, msg); sent {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sent {
		conn.Close() //nolint:errcheck,gosec
		t.Fatal("timed out waiting for agent to register in hub")
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatalf("failed to set read deadline: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatalf("failed to read message: %v", err)
	}

	received := &messages.ServerMessage{}
	if err := proto.Unmarshal(data, received); err != nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	if received.GetPing().Timestamp != 12345 {
		t.Errorf("expected timestamp 12345, got %d", received.GetPing().Timestamp)
	}

	// Close and wait for the handler goroutine to finish its deferred DB update.
	conn.Close() //nolint:errcheck,gosec
	waitForOffline(t, agent.Id)
}

func TestWsHandler_HandlesPong(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	agent := createTestAgent(t, "key-id-2")
	token, err := auth.GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	conn, resp, err := dialWS(server, token)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("expected successful WS connection, got: %v", err)
	}

	// Wait for registration.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		_, ok := h.clients[agent.Id]
		h.mu.RUnlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	pongMsg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{
			Pong: &messages.PongResponse{Timestamp: time.Now().Unix()},
		},
	}
	data, err := proto.Marshal(pongMsg)
	if err != nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatalf("failed to marshal pong: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		conn.Close() //nolint:errcheck,gosec
		t.Fatalf("failed to send pong: %v", err)
	}

	// Poll until last_seen is updated in DB.
	var lastSeen *time.Time
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var dbAgent models.Agent
		if err := db.DB.First(&dbAgent, "id = ?", agent.Id).Error; err != nil {
			conn.Close() //nolint:errcheck,gosec
			t.Fatalf("failed to query agent: %v", err)
		}
		if dbAgent.LastSeen != nil {
			lastSeen = dbAgent.LastSeen
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastSeen == nil {
		t.Error("expected last_seen to be updated after pong")
	}

	// Close and wait for the handler goroutine to finish its deferred DB update.
	conn.Close() //nolint:errcheck,gosec
	waitForOffline(t, agent.Id)
}

func TestWsHandler_AgentMarkedOfflineOnDisconnect(t *testing.T) {
	setupHandlerTestEnv(t)
	log := testLogger()
	h := NewHub(&log)
	server := newHandlerTestServer(t, h)

	agent := createTestAgent(t, "key-id-3")
	token, err := auth.GenerateAgentToken(agent)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	conn, resp, err := dialWS(server, token)
	if resp != nil {
		defer resp.Body.Close() //nolint:errcheck
	}
	if err != nil {
		t.Fatalf("expected successful WS connection, got: %v", err)
	}

	// Wait for registration then close the connection.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		_, ok := h.clients[agent.Id]
		h.mu.RUnlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	conn.Close() //nolint:errcheck,gosec

	// Poll until agent status is Offline.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var dbAgent models.Agent
		if err := db.DB.First(&dbAgent, "id = ?", agent.Id).Error; err != nil {
			t.Fatalf("failed to query agent: %v", err)
		}
		if dbAgent.Status == models.AgentStatusOffline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected agent status to be Offline after disconnect")
}
