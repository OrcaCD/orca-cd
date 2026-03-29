package agent

import (
	"errors"
	"os"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	Debug    bool
	LogLevel zerolog.Level
	HubURL   string
}

func DefaultConfig() (Config, error) {
	debug := os.Getenv("DEBUG")
	logLevelStr := os.Getenv("LOG_LEVEL")
	hubURL := os.Getenv("HUB_URL")

	if hubURL == "" {
		return Config{}, errors.New("HUB_URL is required")
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	return Config{
		Debug:    debug == "true",
		LogLevel: logLevel,
		HubURL:   hubURL,
	}, nil
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "agent").Logger()

func Run(cfg Config) error {
	Log = Log.Level(cfg.LogLevel)

	Log.Info().Str("version", version.Version).Msg("agent started")

	conn := connectWithRetry(cfg.HubURL)

	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			Log.Error().Err(closeErr).Msg("failed to close WebSocket connection")
		}
	}()

	// Read incoming messages from server
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				Log.Error().Err(err).Msg("Read error")
				return
			}
			msg := &messages.ServerMessage{}
			if err := proto.Unmarshal(data, msg); err != nil {
				Log.Error().Err(err).Msg("Unmarshal error")
				continue
			}
			handleServerMessage(msg)
		}
	}()

	// Send a ping every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for t := range ticker.C {
		ping := &messages.ClientMessage{
			Payload: &messages.ClientMessage_Ping{
				Ping: &messages.PingRequest{Timestamp: t.UnixMilli()},
			},
		}
		data, err := proto.Marshal(ping)
		if err != nil {
			Log.Error().Err(err).Msg("Marshal error")
			continue
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			Log.Error().Err(err).Msg("Write error")
			return err
		}
	}

	return nil
}
