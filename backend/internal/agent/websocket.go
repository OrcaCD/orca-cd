package agent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/agent/docker"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const handshakeTimeout = 15 * time.Second
const deploymentTimeout = 5 * time.Minute

type outboundSender interface {
	SendMessage(msg *messages.ClientMessage) error
}

type deployExecutor interface {
	Deploy(ctx context.Context, req docker.DeployRequest) error
	Remove(ctx context.Context, req docker.DeleteRequest) error
}

type pollerHandler interface {
	ApplySettings(apps []docker.AppPollConfig)
	TriggerNow(appID, appName, requestID string)
}

type statusReporter interface {
	ApplicationHealth(ctx context.Context, appID string) docker.HealthState
}

type messageSender struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	session *wscrypto.Session
}

func newMessageSender(conn *websocket.Conn, session *wscrypto.Session) *messageSender {
	return &messageSender{
		conn:    conn,
		session: session,
	}
}

func performHandshake(conn *websocket.Conn, agentID string, hubPubKey ed25519.PublicKey) (*wscrypto.Session, error) {
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

	resp := &messages.ClientMessage{
		Payload: &messages.ClientMessage_KeyExchangeResponse{
			KeyExchangeResponse: &messages.KeyExchangeResponse{
				MlkemCiphertext:      mlkemCiphertext,
				AgentX25519PublicKey: agentX25519Pub,
			},
		},
	}
	respData, err := proto.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("marshal KeyExchangeResponse: %w", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, respData); err != nil {
		return nil, fmt.Errorf("send KeyExchangeResponse: %w", err)
	}

	// Clear the handshake deadline — the hub's read loop will set its own deadline.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear read deadline: %w", err)
	}

	Log.Debug().Str("agent_id", agentID).Msg("WS handshake complete")
	return wscrypto.NewSession(sessionKey)
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
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
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
		go executeDeployment(ctx, sender, deployer, p.DeployRequest)
	case *messages.ServerMessage_DeleteRequest:
		go executeDelete(ctx, sender, deployer, p.DeleteRequest)
	case *messages.ServerMessage_AgentSettings:
		go applyAgentSettings(poller, p.AgentSettings)
		// The hub sends AgentSettings right after connect, carrying the agent's
		// full app list. Use it to report the real health of every application in
		// a single message so the hub doesn't have to guess on reconnect.
		if reporter, ok := deployer.(statusReporter); ok {
			go reportApplicationStatus(ctx, sender, reporter, p.AgentSettings)
		}
	case *messages.ServerMessage_PullImagesRequest:
		go executePullImages(poller, p.PullImagesRequest)
	default:
		Log.Warn().Str("type", fmt.Sprintf("%T", msg.Payload)).Msg("unknown message type received")
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

	statuses := make([]*messages.ApplicationStatus, 0, len(settings.ImagePollSettings))
	for _, s := range settings.ImagePollSettings {
		statuses = append(statuses, &messages.ApplicationStatus{
			ApplicationId: s.ApplicationId,
			Health:        reporter.ApplicationHealth(ctx, s.ApplicationId).Proto(),
		})
	}
	if len(statuses) == 0 {
		return
	}

	if err := sender.SendMessage(&messages.ClientMessage{
		Payload: &messages.ClientMessage_ApplicationStatusReport{
			ApplicationStatusReport: &messages.ApplicationStatusReport{Statuses: statuses},
		},
	}); err != nil {
		Log.Error().Err(err).Msg("failed to send application status report")
	}
}

func executePullImages(poller pollerHandler, req *messages.PullImagesRequest) {
	if poller == nil {
		return
	}
	poller.TriggerNow(req.ApplicationId, req.ApplicationName, req.RequestId)
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

	deployCtx, cancel := context.WithTimeout(ctx, deploymentTimeout)
	defer cancel()

	if err := deployer.Deploy(deployCtx, docker.DeployRequest{
		ApplicationID:   req.ApplicationId,
		ApplicationName: req.ApplicationName,
		ComposeFile:     req.ComposeFile,
	}); err != nil {
		Log.Error().Err(err).Str("application_id", req.ApplicationId).Str("request_id", req.RequestId).Msg("deployment failed")
		result.ErrorMessage = err.Error()
		sendDeployResult(sender, result)
		return
	}

	result.Success = true
	sendDeployResult(sender, result)
	// The deploy does not block on healthchecks; the docker client observes the
	// application's health via daemon events and reports it once it settles.
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
	conn, err := connectWithRetry(ctx, cfg.HubUrl, cfg.AuthToken)
	if err != nil || tracker.setAndCancelled(ctx, conn) {
		return nil, nil, nil
	}
	session, err := performHandshake(conn, cfg.AgentID, cfg.HubPublicKey)
	if err != nil {
		Log.Error().Err(err).Msg("handshake failed")
		_ = conn.Close()
		return nil, nil, fmt.Errorf("handshake: %w", err)
	}
	return conn, session, nil
}

func connectWithRetry(ctx context.Context, url, authToken string) (*websocket.Conn, error) {
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
		conn, resp, err := websocket.DefaultDialer.DialContext(ctx, url, header)

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
