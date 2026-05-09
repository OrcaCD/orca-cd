package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

func New(service string, logJSON bool) zerolog.Logger {
	if logJSON {
		return zerolog.New(os.Stderr).With().Timestamp().Str("service", service).Logger()
	}
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().Timestamp().Str("service", service).Logger()
}
