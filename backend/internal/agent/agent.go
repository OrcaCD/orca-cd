package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	Debug     bool
	LogLevel  zerolog.Level
	LogJSON   bool
	HubUrl    string
	AuthToken string
}

func DefaultConfig() (Config, error) {
	debug := os.Getenv("DEBUG")
	logLevelStr := os.Getenv("LOG_LEVEL")
	logJSONStr := os.Getenv("LOG_JSON")
	hubUrl := os.Getenv("HUB_URL")
	authToken := os.Getenv("AUTH_TOKEN")

	hubUrl, err := parseHubURL(hubUrl)
	if err != nil {
		return Config{}, fmt.Errorf("HUB_URL: %w", err)
	}

	if authToken == "" {
		return Config{}, errors.New("AUTH_TOKEN is required")
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	logJSON := strings.EqualFold(logJSONStr, "true")

	return Config{
		Debug:     debug == "true",
		LogLevel:  logLevel,
		LogJSON:   logJSON,
		HubUrl:    hubUrl,
		AuthToken: authToken,
	}, nil
}

func newLogger(logJSON bool) zerolog.Logger {
	if logJSON {
		return zerolog.New(os.Stderr).With().Timestamp().Str("service", "agent").Logger()
	}

	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("service", "agent").Logger()
}

var Log = newLogger(false)

// connTracker holds the active WebSocket connection under a mutex so the
// shutdown goroutine can safely close it from a different goroutine.
type connTracker struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (t *connTracker) close() {
	t.mu.Lock()
	if t.conn != nil {
		_ = t.conn.Close()
	}
	t.mu.Unlock()
}

func (t *connTracker) setAndCancelled(ctx context.Context, conn *websocket.Conn) bool {
	t.mu.Lock()
	t.conn = conn
	cancelled := ctx.Err() != nil
	if cancelled {
		_ = conn.Close()
	}
	t.mu.Unlock()
	return cancelled
}

func Run(cfg Config) error {
	Log = newLogger(cfg.LogJSON).Level(cfg.LogLevel)

	Log.Info().Str("version", version.Version).Msg("agent started")

	dockerClient, err := docker.New(Log)
	if err != nil {
		return fmt.Errorf("docker init: %w", err)
	}
	_ = dockerClient // will be passed to handlers once deployment logic is added

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var tracker connTracker
	go func() {
		<-ctx.Done()
		Log.Info().Msg("shutting down agent...")
		tracker.close()
	}()

	conn, err := connectWithRetry(ctx, cfg.HubUrl, cfg.AuthToken)
	if err != nil || tracker.setAndCancelled(ctx, conn) {
		Log.Info().Msg("agent stopped")
		return nil
	}

	for {
		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			if ctx.Err() != nil {
				Log.Info().Msg("agent stopped")
				return nil
			}
			Log.Error().Err(readErr).Msg("disconnected from hub, reconnecting...")
			if closeErr := conn.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("error closing connection")
			}
			conn, err = connectWithRetry(ctx, cfg.HubUrl, cfg.AuthToken)
			if err != nil || tracker.setAndCancelled(ctx, conn) {
				Log.Info().Msg("agent stopped")
				return nil
			}
			continue
		}
		msg := &messages.ServerMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			Log.Error().Err(err).Msg("unmarshal error")
			continue
		}
		handleServerMessage(msg, conn)
	}
}
