package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDefaultClient(t *testing.T) {
	if Default == nil {
		t.Fatal("Default client is nil")
	}
	if Default.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, Default.Timeout)
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout != 15*time.Second {
		t.Errorf("expected 15s, got %v", DefaultTimeout)
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent()
	if !strings.HasPrefix(ua, "OrcaCD/") {
		t.Errorf("expected UserAgent to start with 'OrcaCD/', got %q", ua)
	}
}

func TestNewRequest_SetsUserAgent(t *testing.T) {
	req, err := NewRequest(context.Background(), http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("User-Agent") != UserAgent() {
		t.Errorf("expected User-Agent %q, got %q", UserAgent(), req.Header.Get("User-Agent"))
	}
}

func TestNewRequest_SetsMethod(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req, err := NewRequest(context.Background(), method, "http://example.com", nil)
		if err != nil {
			t.Fatalf("unexpected error for method %s: %v", method, err)
		}
		if req.Method != method {
			t.Errorf("expected method %s, got %s", method, req.Method)
		}
	}
}

func TestNewRequest_UsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := NewRequest(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Context().Err() == nil {
		t.Error("expected cancelled context, got nil error")
	}
}

func TestNewRequest_InvalidURL(t *testing.T) {
	_, err := NewRequest(context.Background(), http.MethodGet, "://invalid", nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestNewRequest_WithBody(t *testing.T) {
	body := strings.NewReader(`{"key":"value"}`)
	req, err := NewRequest(context.Background(), http.MethodPost, "http://example.com", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Body == nil {
		t.Error("expected non-nil body")
	}
}

func TestGet_SendsRequest(t *testing.T) {
	var receivedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if receivedUA != UserAgent() {
		t.Errorf("expected User-Agent %q, got %q", UserAgent(), receivedUA)
	}
}

func TestGet_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := Get(ctx, server.URL)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}

	if resp != nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Error("expected nil response for cancelled context")
	}
}

func TestGet_InvalidURL(t *testing.T) {
	resp, err := Get(context.Background(), "://invalid")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
	if resp != nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Error("expected nil response for invalid URL")
	}
}
