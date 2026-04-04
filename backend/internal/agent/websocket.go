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

// TODO: Handle unauthorized gracefully
// TODO: Handle service disconnect gracefully and retry
func connectWithRetry(url string, authToken string) *websocket.Conn {
	for {
		header := make(http.Header)
		header.Set("Authorization", authToken)
		conn, _, err := websocket.DefaultDialer.Dial(url, header)
		if err == nil {
			Log.Println("Connected to", url)
			return conn
		}
		Log.Printf("Connection failed, retrying in 5s: %v", err)
		time.Sleep(5 * time.Second)
	}
}
