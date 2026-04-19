package sse

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestLogger() *zerolog.Logger {
	log := zerolog.New(os.Stderr).Level(zerolog.Disabled)
	return &log
}

func TestBrokerSubscribe(t *testing.T) {
	b := NewBroker(newTestLogger())
	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	if connID == "" {
		t.Fatal("expected non-empty connID")
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestBrokerUnsubscribe_ClosesChannel(t *testing.T) {
	b := NewBroker(newTestLogger())
	connID, ch := b.Subscribe()
	b.Unsubscribe(connID)

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}
}

func TestBrokerUnsubscribe_NonExistent(t *testing.T) {
	b := NewBroker(newTestLogger())
	b.Unsubscribe("nonexistent-id") // must not panic
}

func TestBrokerPublish_DeliveredToSubscriber(t *testing.T) {
	b := NewBroker(newTestLogger())
	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	b.Publish(Event{Type: EventTypeUpdate, URL: "http://example.com"})

	select {
	case received := <-ch:
		if received.Type != EventTypeUpdate {
			t.Errorf("expected type %q, got %q", EventTypeUpdate, received.Type)
		}
		if received.URL != "http://example.com" {
			t.Errorf("expected URL %q, got %q", "http://example.com", received.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBrokerPublish_DeliveredToAllSubscribers(t *testing.T) {
	b := NewBroker(newTestLogger())

	ids := make([]string, 3)
	chs := make([]<-chan Event, 3)
	for i := range 3 {
		ids[i], chs[i] = b.Subscribe()
	}
	defer func() {
		for _, id := range ids {
			b.Unsubscribe(id)
		}
	}()

	b.Publish(Event{Type: EventTypeUpdate})

	for i, ch := range chs {
		select {
		case received := <-ch:
			if received.Type != EventTypeUpdate {
				t.Errorf("subscriber %d: expected type %q, got %q", i, EventTypeUpdate, received.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestPublishUpdate_NilBroker(t *testing.T) {
	DefaultBroker = nil
	PublishUpdate("/api/v1/repositories") // must not panic
}

func TestPublishUpdate_SendsUpdateEvent(t *testing.T) {
	b := NewBroker(newTestLogger())
	DefaultBroker = b
	t.Cleanup(func() { DefaultBroker = nil })

	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	PublishUpdate("/api/v1/repositories")

	select {
	case received := <-ch:
		if received.Type != EventTypeUpdate {
			t.Errorf("expected type %q, got %q", EventTypeUpdate, received.Type)
		}
		if received.URL != "/api/v1/repositories" {
			t.Errorf("expected URL %q, got %q", "/api/v1/repositories", received.URL)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBrokerPublish_DropsWhenBufferFull(t *testing.T) {
	b := NewBroker(newTestLogger())
	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	// Publish more than the channel buffer capacity (16)
	for range 20 {
		b.Publish(Event{Type: EventTypeUpdate})
	}

	count := 0
	deadline := time.After(100 * time.Millisecond)
drain:
	for {
		select {
		case <-ch:
			count++
		case <-deadline:
			break drain
		}
	}

	if count > 16 {
		t.Errorf("expected at most 16 events (buffer capacity), received %d", count)
	}
	if count == 0 {
		t.Error("expected at least some events to be received")
	}
}
