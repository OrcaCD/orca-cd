package agent

import (
	"net/http"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

func handleServerMessage(msg *messages.ServerMessage, conn *websocket.Conn) {
	switch p := msg.Payload.(type) {
	case *messages.ServerMessage_Ping:
		latency := time.Now().UnixMilli() - p.Ping.Timestamp
		Log.Debug().Msgf("Ping received, latency: %dms", latency)

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
			Log.Error().Err(err).Msg("Failed to marshal Pong response")
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			Log.Error().Err(err).Msg("Failed to send Pong response")
			return
		}
	default:
		Log.Warn().Msg("Unknown message type received")
	}
}

func connectWithRetry(url string, authToken string) *websocket.Conn {
	const (
		initialDelay = 1 * time.Second
		maxDelay     = 60 * time.Second
	)
	delay := initialDelay
	for {
		header := make(http.Header)
		header.Set("Authorization", authToken)
		conn, resp, err := websocket.DefaultDialer.Dial(url, header)

		if err == nil {
			Log.Info().Str("url", url).Msg("Connected to hub")
			return conn
		}
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			Log.Fatal().Msg("Unauthorized: auth token is invalid or expired, aborting")
		}
		Log.Error().Err(err).Dur("retry_in", delay/1000).Msg("Connection failed, retrying")
		time.Sleep(delay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}

		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				Log.Error().Err(closeErr).Msg("Failed to close response body")
			}
		}
	}
}
