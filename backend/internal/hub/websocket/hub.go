package websocket

import (
	"fmt"
	"sync"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	Id   string
	conn *websocket.Conn
	Send chan *messages.ServerMessage
}

// Close signals the WritePump to stop.
func (c *Client) Close() {
	close(c.Send)
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
	log     *zerolog.Logger
}

func NewHub(log *zerolog.Logger) *Hub {
	return &Hub{clients: make(map[string]*Client), log: log}
}

func (h *Hub) Register(id string, conn *websocket.Conn) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.clients[id]; exists {
		return nil, fmt.Errorf("client ID %s already registered", id)
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

// Send sends a message to a specific client by ID.
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

// WritePump writes outgoing messages to the WebSocket connection.
// Must be run in a goroutine. Exits when the client's Send channel is closed.
func (h *Hub) WritePump(c *Client, log *zerolog.Logger) {
	defer func() {
		if closeErr := c.conn.Close(); closeErr != nil {
			log.Error().Err(closeErr).Msg("failed to close WebSocket connection")
		}
	}()

	for msg := range c.Send {
		data, err := proto.Marshal(msg)
		if err != nil {
			log.Error().Err(err).Msg("Marshal error")
			continue
		}
		if err := c.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			log.Error().Err(err).Msg("Write error")
			return
		}
	}
}
