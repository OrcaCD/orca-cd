package agent

import (
	"context"
	"net/http"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

func handleServerMessage(msg *messages.ServerMessage, conn *websocket.Conn) {
	switch p := msg.Payload.(type) {
	case *messages.ServerMessage_Ping:
		latency := time.Now().UnixMilli() - p.Ping.Timestamp
		Log.Debug().Msgf("ping received, latency: %dms", latency)

		// Respond with Pong
		pong := &messages.ClientMessage{
			Payload: &messages.ClientMessage_Pong{
				Pong: &messages.PongResponse{
					Timestamp: time.Now().UnixMilli(),
				},
			},
		}
		data, err := proto.Marshal(pong)

		if err != nil {
			Log.Error().Err(err).Msg("failed to marshal Pong response")
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			Log.Error().Err(err).Msg("failed to send Pong response")
			return
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

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}
