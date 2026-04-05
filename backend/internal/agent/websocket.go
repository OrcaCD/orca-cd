package agent

import (
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

func connectWithRetry(url string, authToken string) *websocket.Conn {
	const (
		initialDelay = 1 * time.Second
		maxDelay     = 60 * time.Second
	)

	if strings.HasPrefix(url, "ws://") {
		Log.Warn().Msg("connecting over unencrypted WebSocket. Do not use this in production or over untrusted networks")
	}

	delay := initialDelay
	for {
		header := make(http.Header)
		header.Set("Authorization", authToken)
		conn, resp, err := websocket.DefaultDialer.Dial(url, header)

		if err == nil {
			Log.Info().Str("url", url).Msg("connected to hub")
			return conn
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			Log.Fatal().Msg("unauthorized: auth token is invalid or expired, aborting")
		}
		Log.Error().Err(err).Dur("retry_in", delay/1000).Msg("connection failed, retrying")
		time.Sleep(delay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}

		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("failed to close response body")
			}
		}
	}
}
