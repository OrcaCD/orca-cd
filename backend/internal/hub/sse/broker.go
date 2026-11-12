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

// Event is published to every client, which only gets the topics it may see.
type Event struct {
	Type   EventType
	Topics []Topic
}

// Message is what a client receives: the URLs to refetch.
type Message struct {
	Type EventType `json:"type"`
	URLs []string  `json:"urls,omitempty"`
}

// ReloadsProfileOf reports whether the event tells the user to reload their profile.
func (e Event) ReloadsProfileOf(userID string) bool {
	return slices.ContainsFunc(e.Topics, func(topic Topic) bool {
		return topic.url == topicProfile.url && topic.SentTo(userID)
	})
}

// URLs returns the URLs of all topics, as sent to a client that may see them.
func (e Event) URLs() []string {
	message, _ := e.Message(func(Topic) bool { return true })
	return message.URLs
}

// Message returns what to send to a client that may see the topics visible
// accepts. Returns false when nothing is left to send.
func (e Event) Message(visible func(Topic) bool) (Message, bool) {
	message := Message{Type: e.Type}
	for _, topic := range e.Topics {
		if visible(topic) && !slices.Contains(message.URLs, topic.url) {
			message.URLs = append(message.URLs, topic.url)
		}
	}
	return message, len(message.URLs) > 0
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
	event := Event{Type: EventTypeUpdate}
	seen := make(map[Topic]struct{}, len(topics))
	for _, topic := range topics {
		if _, ok := seen[topic]; ok || topic.url == "" {
			continue
		}
		seen[topic] = struct{}{}
		event.Topics = append(event.Topics, topic)
	}
	if len(event.Topics) == 0 {
		return
	}
	b.Publish(event)
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
