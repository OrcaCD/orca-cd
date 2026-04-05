package agent

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

type Config struct {
	Debug     bool
	LogLevel  zerolog.Level
	HubUrl    string
	AuthToken string
}

func DefaultConfig() (Config, error) {
	debug := os.Getenv("DEBUG")
	logLevelStr := os.Getenv("LOG_LEVEL")
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

	return Config{
		Debug:     debug == "true",
		LogLevel:  logLevel,
		HubUrl:    hubUrl,
		AuthToken: authToken,
	}, nil
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "agent").Logger()

func Run(cfg Config) error {
	Log = Log.Level(cfg.LogLevel)

	Log.Info().Str("version", version.Version).Msg("agent started")

	dockerClient, err := docker.New(Log)
	if err != nil {
		return fmt.Errorf("docker init: %w", err)
	}
	_ = dockerClient // will be passed to handlers once deployment logic is added

	conn := connectWithRetry(cfg.HubUrl, cfg.AuthToken)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			Log.Error().Err(err).Msg("disconnected from hub, reconnecting...")
			err = conn.Close()
			if err != nil {
				Log.Error().Err(err).Msg("error closing connection")
			}
			conn = connectWithRetry(cfg.HubUrl, cfg.AuthToken)
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
