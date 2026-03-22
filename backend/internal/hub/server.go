package hub

import (
	"os"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Debug          bool
	Host           string
	Port           string
	LogLevel       zerolog.Level
	TrustedProxies []string
	AppURL         string
}

func DefaultConfig() (Config, error) {
	debug := os.Getenv("DEBUG")
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")
	logLevelStr := os.Getenv("LOG_LEVEL")

	if port == "" {
		port = "8080"
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	var trustedProxies []string
	if tp := os.Getenv("TRUSTED_PROXIES"); tp != "" {
		trustedProxies = strings.Split(tp, ",")
	}

	appURL, err := parseAppURL(os.Getenv("APP_URL"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Debug:          debug == "true",
		Host:           host,
		Port:           port,
		LogLevel:       logLevel,
		TrustedProxies: trustedProxies,
		AppURL:         appURL,
	}, nil
}

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
	With().Timestamp().Str("service", "hub").Logger()

func Run(cfg Config) error {
	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	Log = Log.Level(cfg.LogLevel)

	router := gin.Default()
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return err
	}
	if len(cfg.TrustedProxies) == 0 {
		Log.Warn().Msg("no trusted proxies configured; in production the server should always run behind a reverse proxy")
	}
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.ValidateOrigin(cfg.AppURL))

	RegisterRoutes(router, cfg)

	addr := cfg.Host + ":" + cfg.Port
	Log.Info().Str("addr", addr).Str("version", version.Version).Msg("hub started")
	return router.Run(addr)
}
