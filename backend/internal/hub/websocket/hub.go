package websocket

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	Id      string
	conn    *websocket.Conn
	Send    chan *messages.ServerMessage
	session *wscrypto.Session
}

type DeployHandle struct {
	hub       *Hub
	requestID string
	outcome   chan deployOutcome
}

type deployOutcome struct {
	err    error
	result *messages.DeployResult
}

type pendingDeploy struct {
	agentID string
	outcome chan deployOutcome
}

var ErrAgentDisconnected = errors.New("agent disconnected before deployment completed")
var ErrDeployUnavailable = errors.New("agent is not connected or unable to receive deploy requests")

func (h *DeployHandle) Await(ctx context.Context) (*messages.DeployResult, error) {
	select {
	case outcome, ok := <-h.outcome:
		if !ok {
			return nil, ErrAgentDisconnected
		}
		return outcome.result, outcome.err
	case <-ctx.Done():
		h.Cancel()
		return nil, ctx.Err()
	}
}

func (h *DeployHandle) Cancel() {
	h.hub.cancelDeploy(h.requestID)
}

// Close signals the WritePump to stop.
func (c *Client) Close() {
	close(c.Send)
}

type Hub struct {
	mu             sync.RWMutex
	clients        map[string]*Client
	deploysMu      sync.Mutex
	pendingDeploys map[string]pendingDeploy
	log            *zerolog.Logger
}

func NewHub(log *zerolog.Logger) *Hub {
	return &Hub{
		clients:        make(map[string]*Client),
		pendingDeploys: make(map[string]pendingDeploy),
		log:            log,
	}
}

func (h *Hub) Register(id string, conn *websocket.Conn) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.clients[id]; exists {
		return nil, fmt.Errorf("client Id %s already registered", id)
	}
	c := &Client{
		Id:   id,
		conn: conn,
		Send: make(chan *messages.ServerMessage, 64),
	}
	h.clients[id] = c
	h.log.Debug().Str("client", id).Msg("Client registered")
	return c, nil
}

func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	delete(h.clients, id)
	h.mu.Unlock()
	h.failPendingDeploys(id, ErrAgentDisconnected)
	h.log.Debug().Str("client", id).Msg("Client unregistered")
}

// Send sends a message to a specific client by Id.
// Returns false if the client doesn't exist or the buffer is full.
func (h *Hub) Send(id string, msg *messages.ServerMessage) bool {
	h.mu.RLock()
	c, ok := h.clients[id]
	defer h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case c.Send <- msg:
		return true
	default:
		h.log.Warn().Str("client", id).Msg("Client send buffer full, dropping message")
		return false
	}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg *messages.ServerMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.Send <- msg:
		default:
			h.log.Warn().Str("client", c.Id).Msg("Client send buffer full, dropping message")
		}
	}
}

func (h *Hub) StartDeploy(agentID string, req *messages.DeployRequest) (*DeployHandle, error) {
	handle := &DeployHandle{
		hub:       h,
		requestID: req.RequestId,
		outcome:   make(chan deployOutcome, 1),
	}

	h.deploysMu.Lock()
	h.pendingDeploys[req.RequestId] = pendingDeploy{
		agentID: agentID,
		outcome: handle.outcome,
	}
	h.deploysMu.Unlock()

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeployRequest{
			DeployRequest: req,
		},
	}

	if !h.Send(agentID, msg) {
		h.cancelDeploy(req.RequestId)
		return nil, ErrDeployUnavailable
	}

	return handle, nil
}

func (h *Hub) ResolveDeploy(result *messages.DeployResult) bool {
	h.deploysMu.Lock()
	pending, ok := h.pendingDeploys[result.RequestId]
	if ok {
		delete(h.pendingDeploys, result.RequestId)
	}
	h.deploysMu.Unlock()

	if !ok {
		return false
	}

	pending.outcome <- deployOutcome{result: result}
	close(pending.outcome)

	return true
}

func (h *Hub) cancelDeploy(requestID string) {
	h.deploysMu.Lock()
	pending, ok := h.pendingDeploys[requestID]
	if ok {
		delete(h.pendingDeploys, requestID)
	}
	h.deploysMu.Unlock()

	if ok {
		close(pending.outcome)
	}
}

func (h *Hub) failPendingDeploys(agentID string, err error) {
	h.deploysMu.Lock()
	requestIDs := make([]string, 0)
	pending := make([]pendingDeploy, 0)
	for requestID, waiter := range h.pendingDeploys {
		if waiter.agentID == agentID {
			requestIDs = append(requestIDs, requestID)
			pending = append(pending, waiter)
		}
	}
	for _, requestID := range requestIDs {
		delete(h.pendingDeploys, requestID)
	}
	h.deploysMu.Unlock()

	for _, waiter := range pending {
		waiter.outcome <- deployOutcome{err: err}
		close(waiter.outcome)
	}
}

const writeWait = 10 * time.Second

// WritePump writes outgoing messages to the WebSocket connection.
// Must be run in a goroutine. Exits when the client's Send channel is closed.
func (h *Hub) WritePump(c *Client, log *zerolog.Logger) {
	defer func() {
		if closeErr := c.conn.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close WebSocket connection")
		}
	}()

	for msg := range c.Send {
		allowedUnencrypted := wscrypto.AllowedUnencrypted(msg)

		// Refuse to send sensitive messages before the handshake is complete.
		if c.session == nil && !allowedUnencrypted {
			log.Error().Str("client", c.Id).Msg("dropping message: session not established")
			continue
		}

		outMsg := msg

		if !allowedUnencrypted {
			env, err := c.session.Encrypt(msg)
			if err != nil {
				log.Error().Err(err).Msg("encrypt error")
				continue
			}
			outMsg = &messages.ServerMessage{
				Payload: &messages.ServerMessage_EncryptedPayload{
					EncryptedPayload: env,
				},
			}
		}

		data, err := proto.Marshal(outMsg)
		if err != nil {
			log.Error().Err(err).Msg("Marshal error")
			continue
		}
		if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			log.Error().Err(err).Msg("failed to set write deadline")
			return
		}
		if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			log.Error().Err(err).Msg("Write error")
			return
		}
	}
}
