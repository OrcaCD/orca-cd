package hub

import (
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDefaultConfig_Valid(t *testing.T) {
	t.Setenv("APP_URL", "https://example.com")
	t.Setenv("APP_SECRET", "a-test-secret-that-is-at-least-32-chars!!")
	t.Setenv("PORT", "9090")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("DEBUG", "true")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_JSON", "true")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1, 10.0.0.2")

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
	if cfg.LogLevel != zerolog.DebugLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, zerolog.DebugLevel)
	}
	if !cfg.LogJSON {
		t.Error("LogJSON = false, want true")
	}
	if cfg.AppURL != "https://example.com" {
		t.Errorf("AppURL = %q, want %q", cfg.AppURL, "https://example.com")
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "10.0.0.1" || cfg.TrustedProxies[1] != "10.0.0.2" {
		t.Errorf("TrustedProxies = %v, want [10.0.0.1 10.0.0.2]", cfg.TrustedProxies)
	}
}

func TestDefaultConfig_Defaults(t *testing.T) {
	t.Setenv("APP_URL", "https://example.com")
	t.Setenv("APP_SECRET", "a-test-secret-that-is-at-least-32-chars!!")
	t.Setenv("PORT", "")
	t.Setenv("DEBUG", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("LOG_JSON", "")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want default %q", cfg.Port, "8080")
	}
	if cfg.Debug {
		t.Error("Debug = true, want false by default")
	}
	if cfg.LogLevel != zerolog.InfoLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, zerolog.InfoLevel)
	}
	if cfg.LogJSON {
		t.Error("LogJSON = true, want false by default")
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want empty", cfg.TrustedProxies)
	}
}

func TestDefaultConfig_LogJSON(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"TRUE", true},
		{"false", false},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("APP_URL", "https://example.com")
			t.Setenv("APP_SECRET", "a-test-secret-that-is-at-least-32-chars!!")
			t.Setenv("LOG_JSON", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogJSON != tt.want {
				t.Errorf("LogJSON = %v, want %v", cfg.LogJSON, tt.want)
			}
		})
	}
}

func TestDefaultConfig_LogLevels(t *testing.T) {
	tests := []struct {
		input string
		want  zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"invalid", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv("APP_URL", "https://example.com")
			t.Setenv("APP_SECRET", "a-test-secret-that-is-at-least-32-chars!!")
			t.Setenv("LOG_LEVEL", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestDefaultConfig_TrustedProxies(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single", "10.0.0.1", []string{"10.0.0.1"}},
		{"multiple", "10.0.0.1,10.0.0.2,10.0.0.3", []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}},
		{"spaces trimmed", " 10.0.0.1 , 10.0.0.2 ", []string{"10.0.0.1", "10.0.0.2"}},
		{"empty", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_URL", "https://example.com")
			t.Setenv("APP_SECRET", "a-test-secret-that-is-at-least-32-chars!!")
			t.Setenv("TRUSTED_PROXIES", tt.input)

			cfg, err := DefaultConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.TrustedProxies) != len(tt.want) {
				t.Fatalf("TrustedProxies = %v, want %v", cfg.TrustedProxies, tt.want)
			}
			for i, proxy := range cfg.TrustedProxies {
				if proxy != tt.want[i] {
					t.Errorf("TrustedProxies[%d] = %q, want %q", i, proxy, tt.want[i])
				}
			}
		})
	}
}

func TestDefaultConfig_Errors(t *testing.T) {
	tests := []struct {
		name      string
		appURL    string
		appSecret string
	}{
		{"empty secret", "https://example.com", ""},
		{"short secret", "https://example.com", "tooshort"},
		{"secret exactly 31 chars", "https://example.com", "this-secret-is-31-chars-exactly"},
		{"invalid url", "not-a-url", "a-test-secret-that-is-at-least-32-chars!!"},
		{"url with path", "https://example.com/path", "a-test-secret-that-is-at-least-32-chars!!"},
		{"empty url", "", "a-test-secret-that-is-at-least-32-chars!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_URL", tt.appURL)
			t.Setenv("APP_SECRET", tt.appSecret)

			_, err := DefaultConfig()
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDefaultConfig_SecretExactly32Chars(t *testing.T) {
	t.Setenv("APP_URL", "https://example.com")

	secret32 := "12345678901234567890123456789012"
	if len(secret32) != 32 {
		t.Fatalf("test setup error: secret length is %d", len(secret32))
	}
	t.Setenv("APP_SECRET", secret32)

	_, err := DefaultConfig()
	if err != nil {
		t.Errorf("unexpected error for 32-char secret: %v", err)
	}
}

// freePort returns a localhost address with an available TCP port.
func freePort(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	err = ln.Close()
	if err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}
	return "127.0.0.1", strconv.Itoa(addr.Port)
}

// waitForServer polls the health endpoint until it responds or the deadline passes.
func waitForServer(t *testing.T, addr string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/api/v1/health")
		if err == nil {
			err = resp.Body.Close()
			if err != nil {
				t.Fatalf("failed to close response body: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not become ready within 5 seconds")
}

func TestRun_GracefulShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process self-signaling not supported on Windows")
	}
	t.Chdir(t.TempDir())

	host, port := freePort(t)
	cfg := Config{
		Debug:     true,
		Host:      host,
		Port:      port,
		LogLevel:  zerolog.Disabled,
		AppURL:    "http://" + host + ":" + port,
		AppSecret: "a-test-secret-that-is-at-least-32-chars!!",
	}

	done := make(chan error, 1)
	go func() {
		done <- Run(cfg)
	}()

	waitForServer(t, host+":"+port)

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("failed to find own process: %v", err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatalf("failed to send interrupt: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Run did not shut down within 5 seconds after SIGINT")
	}
}

func TestRun_PortInUse(t *testing.T) {
	t.Chdir(t.TempDir())

	// Hold a port so ListenAndServe fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() {
		if err := ln.Close(); err != nil {
			t.Fatalf("failed to close listener: %v", err)
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	cfg := Config{
		Debug:     true,
		Host:      "127.0.0.1",
		Port:      port,
		LogLevel:  zerolog.Disabled,
		AppURL:    "http://localhost:" + port,
		AppSecret: "a-test-secret-that-is-at-least-32-chars!!",
	}

	err = Run(cfg)
	if err == nil {
		t.Error("expected error when port is already in use, got nil")
	}
}
