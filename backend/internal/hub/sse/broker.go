package sse

// SSE are used to push real-time updates to the frontend

import (
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type EventType string

const (
	EventTypeUpdate EventType = "update"
)

type Event struct {
	Type EventType `json:"type"`
	URL  string    `json:"url,omitempty"`
}

type Broker struct {
	mu      sync.RWMutex
	clients map[string]chan Event
	log     *zerolog.Logger
}

var DefaultBroker *Broker

func NewBroker(log *zerolog.Logger) *Broker {
	return &Broker{
		clients: make(map[string]chan Event),
		log:     log,
	}
}

// Subscribe registers a new SSE client.
// Returns the connection ID and a receive-only event channel.
func (b *Broker) Subscribe() (string, <-chan Event) {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.Must(uuid.NewRandom())
	}
	connID := id.String()
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.clients[connID] = ch
	b.mu.Unlock()
	b.log.Debug().Str("connID", connID).Msg("SSE client subscribed")
	return connID, ch
}

// Unsubscribe removes a client and closes its channel.
func (b *Broker) Unsubscribe(connID string) {
	b.mu.Lock()
	ch, ok := b.clients[connID]
	if ok {
		delete(b.clients, connID)
		close(ch)
	}
	b.mu.Unlock()
	if ok {
		b.log.Debug().Str("connID", connID).Msg("SSE client unsubscribed")
	}
}

func PublishUpdate(topics ...Topic) {
	if DefaultBroker == nil {
		return
	}
	DefaultBroker.PublishUpdate(topics...)
}

func (b *Broker) PublishUpdate(topics ...Topic) {
	for i, topic := range topics {
		if topic.url == "" || slices.Contains(topics[:i], topic) {
			continue
		}
		b.Publish(Event{Type: EventTypeUpdate, URL: topic.url})
	}
}

// Shutdown closes all client channels, causing their SSE handlers to return.
// Call this before shutting down the HTTP server so active SSE connections
// don't block graceful shutdown.
func (b *Broker) Shutdown() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for connID, ch := range b.clients {
		close(ch)
		delete(b.clients, connID)
	}
	b.log.Debug().Msg("SSE broker shut down")
}

// Publish sends an event to all connected SSE clients.
func (b *Broker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for connID, ch := range b.clients {
		select {
		case ch <- event:
		default:
			b.log.Warn().Str("connID", connID).Msg("SSE client buffer full, dropping event")
		}
	}
}
