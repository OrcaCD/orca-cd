package hub

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/applicationevents"
	"github.com/OrcaCD/orca-cd/internal/hub/applications"
	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/middleware"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type Config struct {
	Host             string
	Port             string
	LogLevel         zerolog.Level
	LogJSON          bool
	TrustedProxies   []string
	AllowedIPs       []string
	AppURL           string
	AppSecret        string
	DisableLocalAuth bool
	Demo             bool
	DisableUI        bool
}

func DefaultConfig() (Config, error) {
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")
	logLevelStr := os.Getenv("LOG_LEVEL")
	logJSONStr := os.Getenv("LOG_JSON")
	appSecret := os.Getenv("APP_SECRET")

	disableLocalAuth, _ := strconv.ParseBool(os.Getenv("DISABLE_LOCAL_AUTH"))
	demo, _ := strconv.ParseBool(os.Getenv("DEMO"))
	disableUI, _ := strconv.ParseBool(os.Getenv("DISABLE_UI"))
	if port == "" {
		port = "8080"
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	logJSON := strings.EqualFold(logJSONStr, "true")

	var trustedProxies []string
	for entry := range strings.SplitSeq(os.Getenv("TRUSTED_PROXIES"), ",") {
		if s := strings.TrimSpace(entry); s != "" {
			trustedProxies = append(trustedProxies, s)
		}
	}

	var allowedIPs []string
	for entry := range strings.SplitSeq(os.Getenv("ALLOWED_IPS"), ",") {
		if s := strings.TrimSpace(entry); s != "" {
			allowedIPs = append(allowedIPs, s)
		}
	}

	appURL, err := parseAppURL(os.Getenv("APP_URL"))
	if err != nil {
		return Config{}, err
	}

	if appSecret == "" || len(appSecret) < 32 {
		return Config{}, errors.New("invalid app secret: must be minimum 32 characters")
	}

	return Config{
		Host:             host,
		Port:             port,
		LogLevel:         logLevel,
		LogJSON:          logJSON,
		TrustedProxies:   trustedProxies,
		AllowedIPs:       allowedIPs,
		AppURL:           appURL,
		AppSecret:        appSecret,
		DisableLocalAuth: disableLocalAuth,
		Demo:             demo,
		DisableUI:        disableUI,
	}, nil
}

var Log = logger.New("hub", false)

func Run(cfg Config) error {
	gin.SetMode(gin.ReleaseMode)
	Log = logger.New("hub", cfg.LogJSON).Level(cfg.LogLevel)
	applications.Log = Log

	if err := crypto.Init(cfg.AppSecret); err != nil {
		Log.Error().Err(err).Msg("failed to init crypto")
		return err
	}

	if err := auth.Init(cfg.AppSecret, cfg.AppURL); err != nil {
		Log.Error().Err(err).Msg("failed to init auth")
		return err
	}

	if err := initDataDir(); err != nil {
		Log.Error().Err(err).Msg("failed to initialize data directory")
		return err
	}

	dbLogger := Log.With().Str("component", "gorm").Logger()
	err := db.Connect(dbLogger, cfg.LogLevel, cfg.Demo)
	if err != nil {
		Log.Error().Err(err).Msg("failed to connect to database")
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	recoverInterruptedState(context.Background(), &Log)

	applications.DefaultQueue = applications.NewQueue(&Log)
	applications.DefaultQueue.Start()

	applications.DefaultPoller = applications.NewPoller(&Log)
	applications.DefaultPoller.Start()
	defer applications.DefaultPoller.Stop()

	if !cfg.Demo {
		defer db.StartVacuumScheduler()()
	}

	router := gin.New()

	router.Use(middleware.RequestLogger(Log))

	if cfg.Demo {
		router.Use(middleware.DemoBlocking())
	}

	router.Use(middleware.Recovery(Log))
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return err
	}
	if len(cfg.TrustedProxies) == 0 {
		Log.Warn().Msg("no trusted proxies configured; in production the server should always run behind a reverse proxy")
	}
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.IPLock(cfg.AllowedIPs))
	router.Use(middleware.ValidateOrigin(cfg.AppURL))
	router.Use(middleware.TimeoutMiddleware(30 * time.Second))

	err = RegisterRoutes(router, cfg)
	if err != nil {
		return err
	}

	addr := cfg.Host + ":" + cfg.Port

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    22 << 10, // 22 KB
		IdleTimeout:       120 * time.Second,
	}

	Log.Info().Str("addr", addr).Str("version", version.Version).Msg("hub started")

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case err := <-serverErr:
		Log.Error().Err(err).Msg("server error")
		return err
	case sig := <-quit:
		Log.Info().Str("signal", sig.String()).Msg("shutting down hub")
	}

	sse.DefaultBroker.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		Log.Error().Err(err).Msg("forced shutdown")
		return err
	}

	Log.Info().Msg("hub stopped")
	return nil
}

// recoverInterruptedState resets repositories, applications, and history events
// left mid-operation by a previous crash or restart. Failures are logged and do
// not block startup. No SSE update is published: clients are not connected yet.
func recoverInterruptedState(ctx context.Context, log *zerolog.Logger) {
	// Reset repositories stuck in syncing status from a previous crash.
	resetSyncStatus := db.DB.WithContext(ctx).
		Model(&models.Repository{}).
		Where("sync_status = ?", models.SyncStatusSyncing).
		Update("sync_status", models.SyncStatusUnknown)
	if resetSyncStatus.Error != nil {
		log.Warn().Err(resetSyncStatus.Error).Msg("failed to reset repositories stuck in syncing status")
	}

	// Reset applications stuck in syncing status from a previous crash, so a
	// deploy interrupted by a hub or agent restart does not spin forever.
	resetAppSyncStatus := db.DB.WithContext(ctx).
		Model(&models.Application{}).
		Where("sync_status = ?", models.Syncing).
		Update("sync_status", models.OutOfSync)
	if resetAppSyncStatus.Error != nil {
		log.Warn().Err(resetAppSyncStatus.Error).Msg("failed to reset applications stuck in syncing status")
	}

	recovered, err := applicationevents.RecoverRunning(ctx, "hub restarted before the operation result was received")
	if err != nil {
		log.Warn().Err(err).Msg("failed to recover interrupted application events")
	} else if recovered > 0 {
		log.Info().Int64("count", recovered).Msg("marked interrupted application events as failed")
	}
}
