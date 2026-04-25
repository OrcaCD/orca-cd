package agent

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const handshakeTimeout = 15 * time.Second

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

func sendMessage(conn *websocket.Conn, session *wscrypto.Session, msg *messages.ClientMessage) error {
	outMsg := msg
	if !wscrypto.AllowedUnencrypted(msg) {
		env, err := session.Encrypt(msg)
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
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func handleServerMessage(msg *messages.ServerMessage, conn *websocket.Conn, session *wscrypto.Session) {
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
		if err := sendMessage(conn, session, pong); err != nil {
			Log.Error().Err(err).Msg("failed to send Pong response")
		}
	default:
		Log.Warn().Msg("unknown message type received")
	}
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
