package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/agenttoken"
	"github.com/OrcaCD/orca-cd/internal/shared/logger"
	"github.com/OrcaCD/orca-cd/internal/shared/utils"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// FatalConfigError signals a misconfiguration that cannot be resolved by restarting.
type FatalConfigError struct {
	Msg string
}

func (e *FatalConfigError) Error() string { return e.Msg }

type Config struct {
	LogLevel                      zerolog.Level
	LogJSON                       bool
	HubUrl                        string
	AuthToken                     string
	AgentID                       string
	HubPublicKey                  ed25519.PublicKey
	SigningPrivateKey             ed25519.PrivateKey
	HealthPort                    string
	DeploymentsDir                string
	AllowedPrivilegedApps         map[string]struct{}
	RestrictBindMountsToDeployDir bool
	// ImagePollIntervalOverride replaces the image polling interval of every
	// application. Only meant for tests, it is zero unless set.
	ImagePollIntervalOverride time.Duration
	// RootCAs is nil unless CA_CERT_FILE is set, in which case it holds the
	// system roots plus the configured certificates.
	RootCAs *x509.CertPool
}

func DefaultConfig() (Config, error) {
	logLevelStr := os.Getenv("LOG_LEVEL")
	hubUrl := os.Getenv("HUB_URL")
	authToken := os.Getenv("AUTH_TOKEN")

	if hubUrl == "" {
		return Config{}, &FatalConfigError{"HUB_URL is not set — set the HUB_URL environment variable and recreate the container"}
	}
	hubUrl, err := parseHubURL(hubUrl)
	if err != nil {
		return Config{}, fmt.Errorf("HUB_URL: %w", err)
	}

	if authToken == "" {
		return Config{}, &FatalConfigError{"AUTH_TOKEN is not set — set the AUTH_TOKEN environment variable and recreate the container"}
	}

	jwtToken, signingPrivateKey, err := agenttoken.Decode(authToken)
	if err != nil {
		return Config{}, &FatalConfigError{fmt.Sprintf("AUTH_TOKEN: %s", err)}
	}

	agentID, hubPublicKey, err := parseTokenClaims(jwtToken)
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_TOKEN: %w", err)
	}

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" {
		logLevel = zerolog.InfoLevel
	}

	logJSON := utils.EnvBool("LOG_JSON")

	healthPort := os.Getenv("HEALTHCHECK_PORT")
	if healthPort == "" {
		healthPort = "8090"
	}

	deploymentsDir := os.Getenv("DEPLOYMENTS_DIR")
	if deploymentsDir == "" {
		deploymentsDir = "/deployments"
	}
	// The deploy policy compares it with the absolute paths compose resolves.
	deploymentsDir, err = filepath.Abs(deploymentsDir)
	if err != nil {
		return Config{}, &FatalConfigError{fmt.Sprintf("DEPLOYMENTS_DIR: %s", err)}
	}

	allowedPrivilegedApps := make(map[string]struct{})
	for entry := range strings.SplitSeq(os.Getenv("ALLOWED_PRIVILEGED_APPS"), ",") {
		if s := strings.TrimSpace(entry); s != "" {
			allowedPrivilegedApps[s] = struct{}{}
		}
	}

	restrictBindMountsToDeployDir := utils.EnvBool("RESTRICT_VOLUMES_TO_DEPLOYMENTS_DIR")

	var imagePollIntervalOverride time.Duration
	if value := os.Getenv("IMAGE_POLL_INTERVAL_OVERRIDE"); value != "" {
		imagePollIntervalOverride, err = time.ParseDuration(value)
		if err != nil || imagePollIntervalOverride < time.Second {
			return Config{}, &FatalConfigError{fmt.Sprintf("IMAGE_POLL_INTERVAL_OVERRIDE: %q is not a duration of at least 1s", value)}
		}
	}

	var rootCAs *x509.CertPool
	if caCertFile := os.Getenv("CA_CERT_FILE"); caCertFile != "" {
		rootCAs, err = loadCACertPool(caCertFile)
		if err != nil {
			return Config{}, &FatalConfigError{fmt.Sprintf("CA_CERT_FILE: %s", err)}
		}
	}

	return Config{
		LogLevel:                      logLevel,
		LogJSON:                       logJSON,
		HubUrl:                        hubUrl,
		AuthToken:                     jwtToken,
		AgentID:                       agentID,
		HubPublicKey:                  hubPublicKey,
		SigningPrivateKey:             signingPrivateKey,
		HealthPort:                    healthPort,
		DeploymentsDir:                deploymentsDir,
		AllowedPrivilegedApps:         allowedPrivilegedApps,
		RestrictBindMountsToDeployDir: restrictBindMountsToDeployDir,
		ImagePollIntervalOverride:     imagePollIntervalOverride,
		RootCAs:                       rootCAs,
	}, nil
}

// loadCACertPool returns the system cert pool extended with the PEM encoded
// certificates from path, so a hub with a publicly trusted certificate keeps
// working after a custom CA has been configured.
func loadCACertPool(path string) (*x509.CertPool, error) {
	pemData, err := os.ReadFile(path) //nolint:gosec // path is operator-provided configuration
	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		Log.Warn().Err(err).Msg("could not load system certificate pool, only certificates from CA_CERT_FILE will be trusted")
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("no valid PEM encoded certificates found in %s", path)
	}
	return pool, nil
}

type agentTokenClaims struct {
	jwt.RegisteredClaims
	HubPublicKey string `json:"hub_pubkey"`
}

// parseTokenClaims extracts the agent ID and hub Ed25519 public key from the AUTH_TOKEN
func parseTokenClaims(authToken string) (agentID string, hubPublicKey ed25519.PublicKey, err error) {
	p := jwt.NewParser()
	unverified, _, err := p.ParseUnverified(authToken, &agentTokenClaims{})
	if err != nil {
		return "", nil, fmt.Errorf("could not parse token: %w", err)
	}
	claims, ok := unverified.Claims.(*agentTokenClaims)
	if !ok || claims.HubPublicKey == "" {
		return "", nil, errors.New("token is missing hub_pubkey claim; re-issue the agent token from the hub")
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(claims.HubPublicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return "", nil, errors.New("token has invalid hub_pubkey claim")
	}
	hubPubKey := ed25519.PublicKey(pubKeyBytes)

	if !ok || claims.Subject == "" {
		return "", nil, errors.New("token is missing subject claim")
	}

	return claims.Subject, hubPubKey, nil
}

var Log = logger.New("agent", false)

// senderRef holds an atomic pointer to the current outbound sender so the
// image poller always uses the latest connection without needing a restart.
type senderRef struct {
	p atomic.Pointer[outboundSender]
}

func (r *senderRef) store(s outboundSender) {
	r.p.Store(&s)
}

func (r *senderRef) load() outboundSender {
	if p := r.p.Load(); p != nil {
		return *p
	}
	return nil
}

func (r *senderRef) SendMessage(msg *messages.ClientMessage) error {
	if s := r.load(); s != nil {
		return s.SendMessage(msg)
	}
	return nil
}

// connTracker holds the active WebSocket connection under a mutex so the
// shutdown goroutine can safely close it from a different goroutine.
type connTracker struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (t *connTracker) close() {
	t.mu.Lock()
	if t.conn != nil {
		_ = t.conn.Close()
	}
	t.mu.Unlock()
}

func (t *connTracker) setAndCancelled(ctx context.Context, conn *websocket.Conn) bool {
	t.mu.Lock()
	t.conn = conn
	cancelled := ctx.Err() != nil
	if cancelled {
		_ = conn.Close()
	}
	t.mu.Unlock()
	return cancelled
}

// Secrets are removed from the process environment once the config is loaded.
// Compose builds the project environment from the agent's environment, so a
// compose file could otherwise copy them into a container through
// interpolation or a value-less environment entry.
var agentSecretEnvVars = []string{"AUTH_TOKEN"}

// hubSecretEnvVars are the hub's secrets, which reach the agent when both share
// an env file. APP_SECRET derives the hub's JWT, handshake and database keys.
var hubSecretEnvVars = []string{"APP_SECRET", "OLD_APP_SECRET"}

// clearSecretEnv removes the secrets and returns the hub secrets it found.
func clearSecretEnv() ([]string, error) {
	var foundHubSecrets []string
	for _, name := range hubSecretEnvVars {
		if _, ok := os.LookupEnv(name); ok {
			foundHubSecrets = append(foundHubSecrets, name)
		}
	}
	for _, name := range slices.Concat(agentSecretEnvVars, hubSecretEnvVars) {
		if err := os.Unsetenv(name); err != nil {
			return nil, fmt.Errorf("unset %s: %w", name, err)
		}
	}
	return foundHubSecrets, nil
}

func Run(cfg Config) error {
	Log = logger.New("agent", cfg.LogJSON).Level(cfg.LogLevel)

	foundHubSecrets, err := clearSecretEnv()
	if err != nil {
		return err
	}
	if len(foundHubSecrets) > 0 {
		Log.Warn().Strs("variables", foundHubSecrets).Msg("found the hub's secrets in the agent environment and removed them. Give the agent its own env file instead of sharing the hub's")
	}

	Log.Info().Str("version", version.Version).Msg("agent started")

	dockerClient, err := docker.New(Log, cfg.DeploymentsDir, cfg.AllowedPrivilegedApps, cfg.RestrictBindMountsToDeployDir)
	if err != nil {
		return fmt.Errorf("docker init: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	sRef := &senderRef{}
	dockerClient.SetHealthReporter(sRef)
	// Re-report the Docker state when the daemon goes away or comes (back) up
	// after the agent already connected; without a connection this is a no-op.
	dockerClient.SetDaemonStateHandler(func() { sendAgentInfo(sRef, dockerClient) })
	poller := docker.NewImagePoller(dockerClient, sRef, Log, cfg.ImagePollIntervalOverride)
	defer poller.StopAll()

	var wsConnected atomic.Bool

	go startHealthServer(ctx, cfg.HealthPort, dockerClient.Ready, wsConnected.Load)

	var tracker connTracker
	go func() {
		<-ctx.Done()
		Log.Info().Msg("shutting down agent...")
		tracker.close()
	}()

	conn, session, err := connectAndHandshake(ctx, cfg, &tracker)
	if conn == nil {
		Log.Info().Msg("agent stopped")
		return err
	}
	wsConnected.Store(true)
	sender := newMessageSender(conn, session)
	sRef.store(sender)
	sendAgentInfo(sender, dockerClient)

	for {
		data, readErr := readHubMessage(conn, hubReadTimeout)
		if readErr != nil {
			wsConnected.Store(false)
			if ctx.Err() != nil {
				Log.Info().Msg("agent stopped")
				return nil
			}
			Log.Error().Err(readErr).Msg("disconnected from hub, reconnecting...")
			if closeErr := conn.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("error closing connection")
			}
			conn, session, err = connectAndHandshake(ctx, cfg, &tracker)
			if conn == nil {
				Log.Info().Msg("agent stopped")
				return err
			}
			wsConnected.Store(true)
			sender = newMessageSender(conn, session)
			sRef.store(sender)
			sendAgentInfo(sender, dockerClient)
			continue
		}
		msg := &messages.ServerMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			Log.Error().Err(err).Msg("unmarshal error")
			continue
		}
		handleServerMessage(ctx, msg, session, sender, dockerClient, poller)
	}
}
