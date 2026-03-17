package agent

import (
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug    bool
	LogLevel zerolog.Level
}

func DefaultConfig() Config {
	debug := os.Getenv("DEBUG")
	logLevelStr := os.Getenv("LOG_LEVEL")

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	return Config{
		Debug:    debug == "true",
		LogLevel: logLevel,
	}
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "agent").Logger()

func Run(cfg Config) error {
	Log = Log.Level(cfg.LogLevel)

	Log.Info().Str("version", version.Version).Msg("agent started")

	return nil
}
