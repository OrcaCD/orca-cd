package hub

import (
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug bool
}

func DefaultConfig() Config {
	debug := os.Getenv("ORCA_HUB_DEBUG")
	return Config{
		Debug: debug == "true",
	}
}

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Logger()

func Run(cfg Config) error {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	RegisterRoutes(router, cfg)

	log.Info().Msg("hub started")
	return router.Run()
}
