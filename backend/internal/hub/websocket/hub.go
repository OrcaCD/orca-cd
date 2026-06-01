package websocket

import (
	"fmt"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// DefaultHub is the package-level Hub instance, set during server initialisation.
var DefaultHub *Hub

type Client struct {
	Id      string
	conn    *websocket.Conn
	Send    chan *messages.ServerMessage
	session *wscrypto.Session
}

// Close signals the WritePump to stop.
func (c *Client) Close() {
	close(c.Send)
}

type Hub struct {
	mu            sync.RWMutex
	clients       map[string]*Client
	deployManager *DeployManager
	log           *zerolog.Logger
}

func NewHub(log *zerolog.Logger) *Hub {
	return &Hub{
		clients:       make(map[string]*Client),
		deployManager: NewDeployManager(),
		log:           log,
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
	h.deployManager.FailPendingDeploys(id, ErrAgentDisconnected)
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
	pending := h.deployManager.StartDeploy(agentID, req)

	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_DeployRequest{
			DeployRequest: req,
		},
	}

	if !h.Send(agentID, msg) {
		h.deployManager.CancelDeploy(req.RequestId)
		return nil, ErrDeployUnavailable
	}

	handle := &DeployHandle{
		deployManager: h.deployManager,
		requestID:     req.RequestId,
		outcome:       pending.outcome,
	}

	return handle, nil
}

func (h *Hub) ResolveDeploy(result *messages.DeployResult) bool {
	return h.deployManager.ResolveDeploy(result)
}

// SendAgentSettings builds an AgentSettings message from the given applications
// and sends it to the specified agent. Returns false if the agent is not connected
// or the send buffer is full.
func (h *Hub) SendAgentSettings(agentID string, apps []models.Application) bool {
	pollSettings := make([]*messages.ImagePollSettings, 0, len(apps))
	for i := range apps {
		pollSettings = append(pollSettings, &messages.ImagePollSettings{
			ApplicationId:   apps[i].Id,
			ApplicationName: apps[i].Name.String(),
			Enabled:         apps[i].ImagePollEnabled,
			IntervalSeconds: apps[i].ImagePollIntervalSeconds,
			DeleteOldImages: apps[i].ImagePollDeleteOldImages,
		})
	}
	return h.Send(agentID, &messages.ServerMessage{
		Payload: &messages.ServerMessage_AgentSettings{
			AgentSettings: &messages.AgentSettings{
				ImagePollSettings: pollSettings,
			},
		},
	})
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
