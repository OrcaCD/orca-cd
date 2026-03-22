package hub

import (
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug    bool
	Port     string
	LogLevel zerolog.Level
}

func DefaultConfig() Config {
	debug := os.Getenv("DEBUG")
	port := os.Getenv("PORT")
	logLevelStr := os.Getenv("LOG_LEVEL")

	if port == "" {
		port = "8080"
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	return Config{
		Debug:    debug == "true",
		Port:     port,
		LogLevel: logLevel,
	}
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "hub").Logger()

func Run(cfg Config) error {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	Log = Log.Level(cfg.LogLevel)

	router := gin.Default()
	router.Use(middleware.SecurityHeaders())

	RegisterRoutes(router, cfg)

	Log.Info().Str("port", cfg.Port).Str("version", version.Version).Msg("hub started")
	return router.Run(":" + cfg.Port)
}
