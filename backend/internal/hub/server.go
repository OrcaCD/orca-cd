package hub

import (
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug         bool
	Port          string
	LogLevel      zerolog.Level
	EncryptionKey string
}

func DefaultConfig() Config {
	debug := os.Getenv("DEBUG")
	port := os.Getenv("PORT")
	logLevelStr := os.Getenv("LOG_LEVEL")
	encryptionKey := os.Getenv("ENCRYPTION_KEY")

	if port == "" {
		port = "8080"
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	if encryptionKey == "" {
		Log.Warn().Msg("ENCRYPTION_KEY not set, using default key (not secure for production)")
		encryptionKey = "default-secret-key-please-change"
	}

	return Config{
		Debug:         debug == "true",
		Port:          port,
		LogLevel:      logLevel,
		EncryptionKey: encryptionKey,
	}
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "hub").Logger()

func Run(cfg Config) error {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	Log = Log.Level(cfg.LogLevel)

	if err := crypto.Init(cfg.EncryptionKey); err != nil {
		Log.Fatal().Err(err).Msg("failed to init crypto")
	}

	_, err := db.Connect()
	if err != nil {
		Log.Fatal().Err(err).Msg("failed to connect to database")
	}

	router := gin.Default()

	RegisterRoutes(router, cfg)

	Log.Info().Str("port", cfg.Port).Str("version", version.Version).Msg("hub started")
	return router.Run(":" + cfg.Port)
}
