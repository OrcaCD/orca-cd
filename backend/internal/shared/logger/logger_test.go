package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNew_JSONMode_ProducesValidJSON(t *testing.T) {
	l := New("hub", true)

	var buf bytes.Buffer
	ll := l.Output(&buf)
	ll.Info().Msg("test message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON output, got: %s (parse error: %v)", buf.String(), err)
	}
}

func TestNew_JSONMode_IncludesServiceField(t *testing.T) {
	l := New("hub", true)

	var buf bytes.Buffer
	ll := l.Output(&buf)
	ll.Info().Msg("test message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry["service"] != "hub" {
		t.Errorf("expected service=hub, got %v", entry["service"])
	}
}

func TestNew_JSONMode_IncludesTimestamp(t *testing.T) {
	l := New("hub", true)

	var buf bytes.Buffer
	ll := l.Output(&buf)
	ll.Info().Msg("test message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := entry["time"]; !ok {
		t.Error("expected 'time' field in JSON output")
	}
}

func TestNew_TextMode_IsNotRawJSON(t *testing.T) {
	// Output() replaces the ConsoleWriter, so we must capture stderr directly.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStderr := os.Stderr
	os.Stderr = w

	l := New("agent", false)
	l.Info().Msg("test message")

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = origStderr

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected human-readable output, got JSON-like: %s", out)
	}
}

func TestNew_TextMode_IncludesServiceValue(t *testing.T) {
	l := New("agent", false)

	var buf bytes.Buffer
	ll := l.Output(&buf)
	ll.Info().Msg("test message")

	if !strings.Contains(buf.String(), "agent") {
		t.Errorf("expected service name in output, got: %s", buf.String())
	}
}

func TestNew_DifferentServiceNames(t *testing.T) {
	for _, svc := range []string{"hub", "agent", "my-service"} {
		l := New(svc, true)

		var buf bytes.Buffer
		ll := l.Output(&buf)
		ll.Info().Msg("x")

		var entry map[string]any
		if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
			t.Fatalf("invalid JSON for service %q: %v", svc, err)
		}
		if entry["service"] != svc {
			t.Errorf("service %q: expected service=%q, got %v", svc, svc, entry["service"])
		}
	}
}
