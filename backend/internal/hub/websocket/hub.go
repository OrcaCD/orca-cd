package websocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/deployments"
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

	closeOnce sync.Once
	closing   atomic.Bool

	// healthSynced holds the applications whose health history was synced on
	// this connection. Only the connection's message handler accesses it.
	healthSynced map[string]struct{}
	// reportedHealth holds the applications this connection reported the
	// health of. Unlike healthSynced it is read outside the message handler.
	reportedHealth sync.Map
}

func (c *Client) healthHistorySynced(applicationID string) bool {
	_, synced := c.healthSynced[applicationID]
	return synced
}

func (c *Client) markHealthHistorySynced(applicationID string) {
	if c.healthSynced == nil {
		c.healthSynced = make(map[string]struct{})
	}
	c.healthSynced[applicationID] = struct{}{}
}

// Close signals the WritePump to stop.
func (c *Client) Close() {
	c.closing.Store(true)
	c.closeOnce.Do(func() {
		close(c.Send)
	})
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
	log     *zerolog.Logger

	deleteMu       sync.Mutex
	pendingDeletes map[string]pendingDelete

	presence *agentPresenceNotifier
}

func NewHub(log *zerolog.Logger) *Hub {
	return &Hub{
		clients:        make(map[string]*Client),
		log:            log,
		pendingDeletes: make(map[string]pendingDelete),
		presence: newAgentPresenceNotifier(agentOfflineGracePeriod, func(agentId string, event models.NotificationEvent) {
			notifyAgentPresence(agentId, event, log)
		}),
	}
}

// Shutdown prepares the hub for process exit. Connections dropping during
// shutdown are not agent outages, so no offline notifications are sent for them.
func (h *Hub) Shutdown() {
	h.presence.Stop()
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
	h.log.Debug().Str("client", id).Msg("Client unregistered")
}

// BeginDisconnect makes a client unavailable while retaining its registration.
// Keeping the registration reserved prevents a reconnect from publishing fresh
// state before the old connection's database cleanup has finished.
func (h *Hub) BeginDisconnect(c *Client) {
	h.mu.Lock()
	// Close while holding the hub lock so Send/Broadcast cannot race with the
	// channel close. This is unconditional: even a stale or already unregistered
	// client must have its socket torn down immediately.
	c.Close()
	h.mu.Unlock()

	// Closing Send stops the writer; closing the socket also unblocks the reader
	// immediately and prevents queued commands from being drained to a session
	// whose results can no longer be processed.
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// FinishDisconnect releases the registration reserved by BeginDisconnect.
func (h *Hub) FinishDisconnect(c *Client) {
	h.mu.Lock()
	if registered, ok := h.clients[c.Id]; ok && registered == c {
		delete(h.clients, c.Id)
	}
	h.mu.Unlock()
	h.log.Debug().Str("client", c.Id).Msg("Client unregistered")
}

func (h *Hub) GetClient(id string) (*Client, bool) {
	h.mu.RLock()
	c, ok := h.clients[id]
	if ok && c.closing.Load() {
		ok = false
		c = nil
	}
	h.mu.RUnlock()
	return c, ok
}

// HasReportedHealth reports whether the agent's current connection reported
// the health of the application. Until then its stored health may be stale.
func (h *Hub) HasReportedHealth(agentID, applicationID string) bool {
	h.mu.RLock()
	c, ok := h.clients[agentID]
	h.mu.RUnlock()
	if !ok || c.closing.Load() {
		return false
	}
	_, reported := c.reportedHealth.Load(applicationID)
	return reported
}

// IsRegistered reports whether an ID is reserved by an active or closing
// client. It allows duplicate reconnects to be rejected before the expensive
// WebSocket and key-exchange handshakes.
func (h *Hub) IsRegistered(id string) bool {
	h.mu.RLock()
	_, ok := h.clients[id]
	h.mu.RUnlock()
	return ok
}

// Send sends a message to a specific client by Id.
// Returns false if the client doesn't exist or the buffer is full.
func (h *Hub) Send(id string, msg *messages.ServerMessage) bool {
	h.mu.RLock()
	c, ok := h.clients[id]
	defer h.mu.RUnlock()
	if !ok || c.closing.Load() {
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
		if c.closing.Load() {
			continue
		}
		select {
		case c.Send <- msg:
		default:
			h.log.Warn().Str("client", c.Id).Msg("Client send buffer full, dropping message")
		}
	}
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
			// A rolled back application runs pinned images until it is deployed again.
			Enabled:         apps[i].ImagePollEnabled && apps[i].RolledBackAt == nil,
			IntervalSeconds: apps[i].ImagePollIntervalSeconds,
			DeleteOldImages: apps[i].DeleteOldImages,
			ImageRetention:  h.imageRetention(&apps[i]),
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

// imageRetention keeps the images a rollback needs when periodic image
// updates delete old images.
func (h *Hub) imageRetention(app *models.Application) *messages.ImageRetention {
	if !app.RollbackEnabled || !app.DeleteOldImages {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	retention, err := deployments.ImageRetention(ctx, app)
	if err != nil {
		h.log.Error().Err(err).Str("applicationId", app.Id).Msg("failed to determine images to keep for rollbacks")
	}
	return retention
}

const writeWait = 10 * time.Second

// WritePump writes outgoing messages to the WebSocket connection.
// Must be run in a goroutine. Exits when the client's Send channel is closed.
func (h *Hub) WritePump(c *Client, log *zerolog.Logger) {
	defer func() {
		// BeginDisconnect usually closed the socket already.
		if closeErr := c.conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			log.Error().Err(closeErr).Msg("failed to close WebSocket connection")
		}
	}()

	for msg := range c.Send {
		if c.closing.Load() {
			return
		}
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
		if c.closing.Load() {
			return
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
