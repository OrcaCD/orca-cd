package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/OrcaCD/orca-cd/internal/version"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const handshakeTimeout = 15 * time.Second
const deploymentTimeout = 5 * time.Minute
const writeWait = 10 * time.Second
const hubReadTimeout = 150 * time.Second

type outboundSender interface {
	SendMessage(msg *messages.ClientMessage) error
}

type deployExecutor interface {
	Deploy(ctx context.Context, req docker.DeployRequest) error
	Remove(ctx context.Context, req docker.DeleteRequest) error
}

type envManager interface {
	ListEnv(ctx context.Context, appName string) ([]*messages.EnvFile, error)
	UpdateEnv(ctx context.Context, appName string, files []*messages.EnvFileChanges) error
	Redeploy(ctx context.Context, appID, appName string) error
}

type pollerHandler interface {
	ApplySettings(apps []docker.AppPollConfig)
	TriggerNow(appID, appName, requestID string, deleteOld bool)
}

type serviceImageReporter interface {
	ServiceImages(ctx context.Context, appID string) ([]*messages.ServiceImage, error)
}

type imageRetentionSetter interface {
	SetImageRetention(appID string, retention *messages.ImageRetention)
}

type statusReporter interface {
	ReportApplicationStatus(ctx context.Context, sender docker.MessageSender, appIDs []string)
}

type dockerStateProvider interface {
	Ready() bool
	ServerVersion() string
}

type messageConn interface {
	SetWriteDeadline(deadline time.Time) error
	WriteMessage(messageType int, data []byte) error
	Close() error
}

type messageSender struct {
	conn    messageConn
	mu      sync.Mutex
	session *wscrypto.Session
}

func newMessageSender(conn *websocket.Conn, session *wscrypto.Session) *messageSender {
	return &messageSender{
		conn:    conn,
		session: session,
	}
}

func performHandshake(conn *websocket.Conn, agentID string, hubPubKey ed25519.PublicKey, signingPrivKey ed25519.PrivateKey) (*wscrypto.Session, error) {
	if err := conn.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("read KeyExchangeInit: %w", err)
	}

	serverMsg := &messages.ServerMessage{}
	if err := proto.Unmarshal(data, serverMsg); err != nil {
		return nil, fmt.Errorf("unmarshal KeyExchangeInit: %w", err)
	}

	init, ok := serverMsg.Payload.(*messages.ServerMessage_KeyExchangeInit)
	if !ok {
		return nil, fmt.Errorf("expected KeyExchangeInit, got %T", serverMsg.Payload)
	}

	if !ed25519.Verify(hubPubKey, wscrypto.HandshakeSignaturePayload(init.KeyExchangeInit.MlkemEncapsulationKey, init.KeyExchangeInit.X25519PublicKey, agentID), init.KeyExchangeInit.HubSignature) {
		return nil, errors.New("hub signature verification failed: hub identity not confirmed")
	}

	mlkemCiphertext, agentX25519Pub, sessionKey, err := wscrypto.AgentHandshake(
		init.KeyExchangeInit.MlkemEncapsulationKey,
		init.KeyExchangeInit.X25519PublicKey,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("agent handshake: %w", err)
	}

	agentSignature := ed25519.Sign(signingPrivKey, wscrypto.AgentResponseSignaturePayload(mlkemCiphertext, agentX25519Pub, agentID))

	resp := &messages.ClientMessage{
		Payload: &messages.ClientMessage_KeyExchangeResponse{
			KeyExchangeResponse: &messages.KeyExchangeResponse{
				MlkemCiphertext:      mlkemCiphertext,
				AgentX25519PublicKey: agentX25519Pub,
				AgentSignature:       agentSignature,
			},
		},
	}
	respData, err := proto.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal KeyExchangeResponse: %w", err)
	}
	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return nil, fmt.Errorf("set KeyExchangeResponse write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, respData); err != nil {
		return nil, fmt.Errorf("send KeyExchangeResponse: %w", err)
	}

	// Clear the handshake deadline — the hub's read loop will set its own deadline.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear read deadline: %w", err)
	}

	Log.Debug().Str("agent_id", agentID).Msg("WS handshake complete")
	return wscrypto.NewSession(sessionKey, wscrypto.RoleAgent)
}

func readHubMessage(conn *websocket.Conn, timeout time.Duration) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set read deadline: %w", err)
	}
	_, data, err := conn.ReadMessage()
	return data, err
}

// TODO: We should make this async by adding it to a goroutine
func (s *messageSender) SendMessage(msg *messages.ClientMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	outMsg := msg
	if !wscrypto.AllowedUnencrypted(msg) {
		env, err := s.session.Encrypt(msg)
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
		outMsg = &messages.ClientMessage{
			Payload: &messages.ClientMessage_EncryptedPayload{
				EncryptedPayload: env,
			},
		}
	}
	data, err := proto.Marshal(outMsg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := s.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		_ = s.conn.Close()
		return fmt.Errorf("set write deadline: %w", err)
	}
	if err := s.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		_ = s.conn.Close()
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

func handleServerMessage(ctx context.Context, msg *messages.ServerMessage, session *wscrypto.Session, sender outboundSender, deployer deployExecutor, poller pollerHandler) {
	_, isEncrypted := msg.Payload.(*messages.ServerMessage_EncryptedPayload)
	if !isEncrypted && !wscrypto.AllowedUnencrypted(msg) {
		Log.Warn().Msgf("dropping unencrypted message of type %T", msg.Payload)
		return
	}

	// Unwrap at most one layer of encryption to prevent a malicious server from
	// crafting a chain of nested EncryptedPayloads that causes unbounded recursion.
	if p, ok := msg.Payload.(*messages.ServerMessage_EncryptedPayload); ok {
		inner := &messages.ServerMessage{}
		if err := session.Decrypt(p.EncryptedPayload, inner); err != nil {
			Log.Error().Err(err).Msg("failed to decrypt message")
			return
		}
		if _, stillEncrypted := inner.Payload.(*messages.ServerMessage_EncryptedPayload); stillEncrypted {
			Log.Warn().Msg("dropping doubly-encrypted message")
			return
		}
		msg = inner
	}

	Log.Debug().Msgf("received message of type %T", msg.Payload)

	switch p := msg.Payload.(type) {
	case *messages.ServerMessage_Ping:
		latency := time.Now().UnixMilli() - p.Ping.Timestamp
		Log.Debug().Msgf("ping received, latency: %dms", latency)

		pong := &messages.ClientMessage{
			Payload: &messages.ClientMessage_Pong{
				Pong: &messages.PongResponse{
					Timestamp: time.Now().UnixMilli(),
				},
			},
		}
		if err := sender.SendMessage(pong); err != nil {
			Log.Error().Err(err).Msg("failed to send Pong response")
		}
	case *messages.ServerMessage_DeployRequest:
		setImageRetention(deployer, p.DeployRequest.ApplicationId, p.DeployRequest.ImageRetention)
		go executeDeployment(ctx, sender, deployer, p.DeployRequest)
	case *messages.ServerMessage_DeleteRequest:
		go executeDelete(ctx, sender, deployer, p.DeleteRequest)
	case *messages.ServerMessage_AgentSettings:
		for _, settings := range p.AgentSettings.ImagePollSettings {
			setImageRetention(deployer, settings.ApplicationId, settings.ImageRetention)
		}
		go applyAgentSettings(poller, p.AgentSettings)
		// The hub sends AgentSettings right after connect, carrying the agent's
		// full app list. Use it to report the real health of every application in
		// a single message so the hub doesn't have to guess on reconnect.
		if reporter, ok := deployer.(statusReporter); ok {
			go reportApplicationStatus(ctx, sender, reporter, p.AgentSettings)
		}
	case *messages.ServerMessage_PullImagesRequest:
		setImageRetention(deployer, p.PullImagesRequest.ApplicationId, p.PullImagesRequest.ImageRetention)
		go executePullImages(poller, p.PullImagesRequest)
	case *messages.ServerMessage_EnvListRequest:
		go executeEnvList(ctx, sender, deployer, p.EnvListRequest)
	case *messages.ServerMessage_EnvUpdateRequest:
		go executeEnvUpdate(ctx, sender, deployer, p.EnvUpdateRequest)
	default:
		Log.Warn().Str("type", fmt.Sprintf("%T", msg.Payload)).Msg("unknown message type received")
	}
}

// sendAgentInfo reports the agent and Docker versions and whether the Docker
// daemon is reachable to the hub so it can display them and warn about
// outdated or broken agents.
func sendAgentInfo(sender outboundSender, dockerState dockerStateProvider) {
	info := &messages.AgentInfo{Version: version.Version}
	if dockerState != nil {
		info.DockerVersion = dockerState.ServerVersion()
		info.DockerUnreachable = !dockerState.Ready()
	}

	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_AgentInfo{AgentInfo: info},
	}); err != nil {
		Log.Error().Err(err).Msg("failed to send agent info")
	}
}

// setImageRetention stores the hub's latest image retention for an
// application before the request that may delete old images runs.
func setImageRetention(deployer deployExecutor, appID string, retention *messages.ImageRetention) {
	if setter, ok := deployer.(imageRetentionSetter); ok {
		setter.SetImageRetention(appID, retention)
	}
}

func applyAgentSettings(poller pollerHandler, settings *messages.AgentSettings) {
	if poller == nil {
		return
	}
	apps := make([]docker.AppPollConfig, 0, len(settings.ImagePollSettings))
	for _, s := range settings.ImagePollSettings {
		apps = append(apps, docker.AppPollConfig{
			AppID:   s.ApplicationId,
			AppName: s.ApplicationName,
			Settings: docker.PollSettings{
				Enabled:         s.Enabled,
				IntervalSeconds: s.IntervalSeconds,
				DeleteOldImages: s.DeleteOldImages,
			},
		})
	}
	poller.ApplySettings(apps)
}

// reportApplicationStatus inspects every application the hub assigned to this
// agent and sends their health back in a single ApplicationStatusReport.
func reportApplicationStatus(ctx context.Context, sender outboundSender, reporter statusReporter, settings *messages.AgentSettings) {
	if reporter == nil || settings == nil {
		return
	}

	appIDs := make([]string, 0, len(settings.ImagePollSettings))
	for _, s := range settings.ImagePollSettings {
		appIDs = append(appIDs, s.ApplicationId)
	}
	if len(appIDs) == 0 {
		return
	}
	reporter.ReportApplicationStatus(ctx, sender, appIDs)
}

func executePullImages(poller pollerHandler, req *messages.PullImagesRequest) {
	if poller == nil {
		return
	}
	poller.TriggerNow(req.ApplicationId, req.ApplicationName, req.RequestId, req.DeleteOldImages)
}

// appExecLocks serializes Deploy/Remove calls per application ID. The hub has
// no cross-request guarantee that only one deploy is ever in flight for a
// given application (e.g. a duplicated webhook delivery can dispatch two
// DeployRequests, or a delete can race a deploy), and executeDeployment /
// executeDelete each run in their own goroutine. Without this lock, two
// docker compose invocations for the same project could run concurrently.
var (
	appExecMu    sync.Mutex
	appExecLocks = make(map[string]*sync.Mutex)
)

func lockForApplication(appID string) *sync.Mutex {
	appExecMu.Lock()
	defer appExecMu.Unlock()
	lock, ok := appExecLocks[appID]
	if !ok {
		lock = &sync.Mutex{}
		appExecLocks[appID] = lock
	}
	return lock
}

func executeDeployment(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.DeployRequest) {
	Log.Info().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("starting deployment")

	result := &messages.DeployResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	if deployer == nil {
		result.ErrorMessage = "deployment executor not initialized"
		sendDeployResult(sender, result)
		return
	}

	lock := lockForApplication(req.ApplicationId)
	lock.Lock()
	defer lock.Unlock()

	deployCtx, cancel := context.WithTimeout(ctx, deploymentTimeout)
	defer cancel()

	if err := deployer.Deploy(deployCtx, docker.DeployRequest{
		ApplicationID:   req.ApplicationId,
		ApplicationName: req.ApplicationName,
		ComposeFile:     req.ComposeFile,
		DeleteOldImages: req.DeleteOldImages,
		PinnedImages:    pinnedImages(req.PinnedImages),
	}); err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("deployment failed")
		result.ErrorMessage = err.Error()
		sendDeployResult(sender, result)
		return
	}

	result.Success = true
	result.Images = reportServiceImages(deployCtx, deployer, req.ApplicationId)
	sendDeployResult(sender, result)
	// The deploy does not block on healthchecks; the docker client observes the
	// application's health via daemon events and reports it once it settles.
}

// pinnedImages maps services to the image they are pinned to, preferring the
// registry digest so a removed image can be pulled again.
func pinnedImages(images []*messages.ServiceImage) map[string]string {
	if len(images) == 0 {
		return nil
	}
	pins := make(map[string]string, len(images))
	for _, image := range images {
		ref := image.GetPinnedRef()
		if ref == "" {
			ref = image.GetImageId()
		}
		if image.GetService() != "" && ref != "" {
			pins[image.GetService()] = ref
		}
	}
	return pins
}

// reportServiceImages returns the images the application runs after a
// deployment. Failing to list them doesn't fail the deployment; the hub then
// can't pin the images when rolling back to it.
func reportServiceImages(ctx context.Context, deployer deployExecutor, appID string) []*messages.ServiceImage {
	reporter, ok := deployer.(serviceImageReporter)
	if !ok {
		return nil
	}
	images, err := reporter.ServiceImages(ctx, appID)
	if err != nil {
		Log.Warn().Err(err).Str("application_id", appID).Msg("could not list service images")
		return nil
	}
	return images
}

func executeDelete(ctx context.Context, sender outboundSender, deployer deployExecutor, req *messages.DeleteRequest) {
	Log.Info().Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("starting removal")

	result := &messages.DeleteResult{
		RequestId:     req.RequestId,
		ApplicationId: req.ApplicationId,
	}

	if deployer == nil {
		result.ErrorMessage = "deployment executor not initialized"
		sendDeleteResult(sender, result)
		return
	}

	lock := lockForApplication(req.ApplicationId)
	lock.Lock()
	defer lock.Unlock()

	delCtx, cancel := context.WithTimeout(ctx, deploymentTimeout)
	defer cancel()

	if err := deployer.Remove(delCtx, docker.DeleteRequest{
		ApplicationID:   req.ApplicationId,
		ApplicationName: req.ApplicationName,
	}); err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("removal failed")
		result.ErrorMessage = err.Error()
		sendDeleteResult(sender, result)
		return
	}

	result.Success = true
	sendDeleteResult(sender, result)
}

func sendDeleteResult(sender outboundSender, result *messages.DeleteResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_DeleteResult{
			DeleteResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send delete result")
	}
}

func sendDeployResult(sender outboundSender, result *messages.DeployResult) {
	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_DeployResult{
			DeployResult: result,
		},
	}); err != nil {
		Log.Error().
			Err(err).
			Str("application_id", result.ApplicationId).
			Str("request_id", result.RequestId).
			Msg("failed to send deploy result")
	}
}

func connectAndHandshake(ctx context.Context, cfg Config, tracker *connTracker) (*websocket.Conn, *wscrypto.Session, error) {
	conn, err := connectWithRetry(ctx, newDialer(cfg.RootCAs), cfg.HubUrl, cfg.AuthToken)
	if err != nil || tracker.setAndCancelled(ctx, conn) {
		return nil, nil, nil
	}
	session, err := performHandshake(conn, cfg.AgentID, cfg.HubPublicKey, cfg.SigningPrivateKey)
	if err != nil {
		Log.Error().Err(err).Msg("handshake failed")
		_ = conn.Close()
		return nil, nil, fmt.Errorf("handshake: %w", err)
	}
	return conn, session, nil
}

// newDialer returns a dialer that verifies the hub certificate against rootCAs,
// or against the system roots when rootCAs is nil.
func newDialer(rootCAs *x509.CertPool) *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	if rootCAs != nil {
		dialer.TLSClientConfig = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		}
	}
	return &dialer
}

func connectWithRetry(ctx context.Context, dialer *websocket.Dialer, url, authToken string) (*websocket.Conn, error) {
	const (
		initialDelay = 1 * time.Second
		maxDelay     = 30 * time.Second
	)

	if strings.HasPrefix(url, "ws://") {
		Log.Warn().Msg("connecting over unencrypted WebSocket. Do not use this in production or over untrusted networks")
	}

	delay := initialDelay
	for {
		header := make(http.Header)
		header.Set("Authorization", authToken)
		conn, resp, err := dialer.DialContext(ctx, url, header)

		if err == nil {
			Log.Info().Str("url", url).Msg("connected to hub")
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			Log.Fatal().Msg("unauthorized: auth token is invalid or expired, aborting")
		}
		Log.Error().Err(err).Float64("retry_in", delay.Seconds()).Msg("connection failed, retrying")

		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("failed to close response body")
			}
		}

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}
