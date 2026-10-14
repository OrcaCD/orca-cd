package sse

import (
	"os"
	"slices"
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

	b.Publish(Event{Type: EventTypeUpdate, URLs: []string{"http://example.com"}})

	select {
	case received := <-ch:
		if received.Type != EventTypeUpdate {
			t.Errorf("expected type %q, got %q", EventTypeUpdate, received.Type)
		}
		if !slices.Equal(received.URLs, []string{"http://example.com"}) {
			t.Errorf("expected URLs %v, got %v", []string{"http://example.com"}, received.URLs)
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
	PublishUpdate(TopicRepositories) // must not panic
}

func TestPublishUpdate_SendsUpdateEvent(t *testing.T) {
	b := NewBroker(newTestLogger())
	DefaultBroker = b
	t.Cleanup(func() { DefaultBroker = nil })

	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	PublishUpdate(TopicRepositories)

	select {
	case received := <-ch:
		if received.Type != EventTypeUpdate {
			t.Errorf("expected type %q, got %q", EventTypeUpdate, received.Type)
		}
		if !slices.Equal(received.URLs, []string{"/api/v1/repositories"}) {
			t.Errorf("expected URLs %v, got %v", []string{"/api/v1/repositories"}, received.URLs)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestPublishUpdate_NilBrokerMultipleTopics(t *testing.T) {
	DefaultBroker = nil
	PublishUpdate(TopicAgents, TopicApplications) // must not panic
}

func TestTopicApplicationEvents_URL(t *testing.T) {
	if got, want := TopicApplicationEvents("application-1").URL(), "/api/v1/applications/application-1/events"; got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestPublishUpdate_SendsSingleEventWithDistinctTopics(t *testing.T) {
	b := NewBroker(newTestLogger())
	DefaultBroker = b
	t.Cleanup(func() { DefaultBroker = nil })

	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	PublishUpdate(
		TopicApplications,
		TopicApplicationEvents("app-1"),
		TopicApplications,
		Topic{},
	)

	want := []string{"/api/v1/applications", "/api/v1/applications/app-1/events"}
	select {
	case received := <-ch:
		if received.Type != EventTypeUpdate {
			t.Errorf("expected type %q, got %q", EventTypeUpdate, received.Type)
		}
		if !slices.Equal(received.URLs, want) {
			t.Errorf("expected URLs %v, got %v", want, received.URLs)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	select {
	case received := <-ch:
		t.Fatalf("unexpected extra event: %+v", received)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishUpdate_NoTopicsSendsNothing(t *testing.T) {
	b := NewBroker(newTestLogger())
	connID, ch := b.Subscribe()
	defer b.Unsubscribe(connID)

	b.PublishUpdate()
	b.PublishUpdate(Topic{})

	select {
	case received := <-ch:
		t.Fatalf("unexpected event: %+v", received)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBrokerShutdown_ClosesAllChannels(t *testing.T) {
	b := NewBroker(newTestLogger())

	ids := make([]string, 3)
	chs := make([]<-chan Event, 3)
	for i := range 3 {
		ids[i], chs[i] = b.Subscribe()
	}

	b.Shutdown()

	for i, ch := range chs {
		select {
		case _, open := <-ch:
			if open {
				t.Errorf("subscriber %d: expected channel to be closed after shutdown", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for channel to close", i)
		}
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
