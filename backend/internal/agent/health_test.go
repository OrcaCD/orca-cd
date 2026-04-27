package agent

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"
)

var healthTestClient = &http.Client{Timeout: 2 * time.Second}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	err = ln.Close()
	if err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}
	return port
}

// startTestHealthServer starts a health server on a free port and returns the
// base URL and a cancel func. It blocks until the server is accepting requests.
func startTestHealthServer(t *testing.T, dockerReady, wsConnected func() bool) (baseURL string, cancel context.CancelFunc) {
	t.Helper()
	port := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	go startHealthServer(ctx, port, dockerReady, wsConnected)

	baseURL = "http://127.0.0.1:" + port
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := healthTestClient.Get(baseURL + "/api/v1/health")
		if err == nil {
			err := resp.Body.Close()
			if err != nil {
				t.Errorf("failed to close response body: %v", err)
			}
			return
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("health server did not start in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHealthHandler_Status(t *testing.T) {
	tests := []struct {
		name        string
		dockerReady bool
		wsConnected bool
		wantStatus  int
		wantBody    string
	}{
		{"healthy", true, true, http.StatusOK, `{"status":"ok"}`},
		{"docker not ready", false, true, http.StatusServiceUnavailable, `{"status":"unhealthy"}`},
		{"ws not connected", true, false, http.StatusServiceUnavailable, `{"status":"unhealthy"}`},
		{"both not ready", false, false, http.StatusServiceUnavailable, `{"status":"unhealthy"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dr, wsc := tt.dockerReady, tt.wsConnected
			baseURL, cancel := startTestHealthServer(t, func() bool { return dr }, func() bool { return wsc })
			defer cancel()

			resp, err := healthTestClient.Get(baseURL + "/api/v1/health")
			if err != nil {
				t.Fatalf("GET /api/v1/health: %v", err)
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("failed to close response body: %v", closeErr)
				}
			}()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			body, _ := io.ReadAll(resp.Body)
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", string(body), tt.wantBody)
			}
		})
	}
}

func TestHealthHandler_ResponseHeaders(t *testing.T) {
	baseURL, cancel := startTestHealthServer(t, func() bool { return true }, func() bool { return true })
	defer cancel()

	resp, err := healthTestClient.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /api/v1/health: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("failed to close response body: %v", closeErr)
		}
	}()

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

func TestHealthHandler_WrongMethod(t *testing.T) {
	baseURL, cancel := startTestHealthServer(t, func() bool { return true }, func() bool { return true })
	defer cancel()

	resp, err := healthTestClient.Post(baseURL+"/api/v1/health", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/health: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHealthHandler_UnknownPath(t *testing.T) {
	baseURL, cancel := startTestHealthServer(t, func() bool { return true }, func() bool { return true })
	defer cancel()

	resp, err := healthTestClient.Get(baseURL + "/api/v1/unknown")
	if err != nil {
		t.Fatalf("GET /api/v1/unknown: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHealthServer_GracefulShutdown(t *testing.T) {
	baseURL, cancel := startTestHealthServer(t, func() bool { return true }, func() bool { return true })

	resp, err := healthTestClient.Get(baseURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("pre-shutdown health check: %v", err)
	}
	err = resp.Body.Close()
	if err != nil {
		t.Errorf("failed to close response body: %v", err)
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := healthTestClient.Get(baseURL + "/api/v1/health")

		defer func() {
			if resp != nil {
				err := resp.Body.Close()
				if err != nil {
					t.Errorf("failed to close response body: %v", err)
				}
			}
		}()

		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not shut down after context cancellation")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
