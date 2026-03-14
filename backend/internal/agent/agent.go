package agent

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

type Config struct {
	Debug bool
}

func DefaultConfig() Config {
	debug := os.Getenv("ORCA_DEBUG")
	return Config{
		Debug: debug == "true",
	}
}

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Logger()

func Run(cfg Config) error {
	log.Info().Msg("agent started")

	return nil
}
