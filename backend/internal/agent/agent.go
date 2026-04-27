package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// FatalConfigError signals a misconfiguration that cannot be resolved by restarting.
type FatalConfigError struct {
	Msg string
}

func (e *FatalConfigError) Error() string { return e.Msg }

type Config struct {
	LogLevel     zerolog.Level
	LogJSON      bool
	HubUrl       string
	AuthToken    string
	AgentID      string
	HubPublicKey ed25519.PublicKey
	HealthPort   string
}

func DefaultConfig() (Config, error) {
	logLevelStr := os.Getenv("LOG_LEVEL")
	logJSONStr := os.Getenv("LOG_JSON")
	hubUrl := os.Getenv("HUB_URL")
	authToken := os.Getenv("AUTH_TOKEN")

	hubUrl, err := parseHubURL(hubUrl)
	if err != nil {
		return Config{}, fmt.Errorf("HUB_URL: %w", err)
	}

	if authToken == "" {
		return Config{}, &FatalConfigError{"AUTH_TOKEN is not set — set the AUTH_TOKEN environment variable and recreate the container"}
	}

	agentID, hubPublicKey, err := parseTokenClaims(authToken)
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_TOKEN: %w", err)
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	logJSON := strings.EqualFold(logJSONStr, "true")

	healthPort := os.Getenv("HEALTHCHECK_PORT")
	if healthPort == "" {
		healthPort = "8090"
	}

	return Config{
		LogLevel:     logLevel,
		LogJSON:      logJSON,
		HubUrl:       hubUrl,
		AuthToken:    authToken,
		AgentID:      agentID,
		HubPublicKey: hubPublicKey,
		HealthPort:   healthPort,
	}, nil
}

type agentTokenClaims struct {
	jwt.RegisteredClaims
	HubPublicKey string `json:"hub_pubkey"`
}

// parseTokenClaims extracts the agent ID and hub Ed25519 public key from the AUTH_TOKEN
func parseTokenClaims(authToken string) (agentID string, hubPublicKey ed25519.PublicKey, err error) {
	p := jwt.NewParser()
	unverified, _, err := p.ParseUnverified(authToken, &agentTokenClaims{})
	if err != nil {
		return "", nil, fmt.Errorf("could not parse token: %w", err)
	}
	claims, ok := unverified.Claims.(*agentTokenClaims)
	if !ok || claims.HubPublicKey == "" {
		return "", nil, errors.New("token is missing hub_pubkey claim; re-issue the agent token from the hub")
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(claims.HubPublicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return "", nil, errors.New("token has invalid hub_pubkey claim")
	}
	hubPubKey := ed25519.PublicKey(pubKeyBytes)

	if !ok || claims.Subject == "" {
		return "", nil, errors.New("token is missing subject claim")
	}

	return claims.Subject, hubPubKey, nil
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

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var wsConnected atomic.Bool

	go startHealthServer(ctx, cfg.HealthPort, dockerClient.Ready, wsConnected.Load)

	var tracker connTracker
	go func() {
		<-ctx.Done()
		Log.Info().Msg("shutting down agent...")
		tracker.close()
	}()

	conn, session, err := connectAndHandshake(ctx, cfg, &tracker)
	if conn == nil {
		Log.Info().Msg("agent stopped")
		return err
	}
	wsConnected.Store(true)

	for {
		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			wsConnected.Store(false)
			if ctx.Err() != nil {
				Log.Info().Msg("agent stopped")
				return nil
			}
			Log.Error().Err(readErr).Msg("disconnected from hub, reconnecting...")
			if closeErr := conn.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("error closing connection")
			}
			conn, session, err = connectAndHandshake(ctx, cfg, &tracker)
			if conn == nil {
				Log.Info().Msg("agent stopped")
				return err
			}
			wsConnected.Store(true)
			continue
		}
		msg := &messages.ServerMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			Log.Error().Err(err).Msg("unmarshal error")
			continue
		}
		handleServerMessage(msg, conn, session)
	}
}
