package hub

import (
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug bool
	Port  string
}

func DefaultConfig() Config {
	debug := os.Getenv("ORCA_DEBUG")
	port := os.Getenv("ORCA_PORT")

	if port == "" {
		port = "8080"
	}

	return Config{
		Debug: debug == "true",
		Port:  port,
	}
}

var log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "hub").Logger()

func Run(cfg Config) error {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	RegisterRoutes(router, cfg)

	log.Info().Str("port", cfg.Port).Str("version", version.Version).Msg("hub started")
	return router.Run(":" + cfg.Port)
}
