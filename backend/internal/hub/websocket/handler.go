package websocket

import (
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func WsHandler(h *Hub, log *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken := c.GetHeader("Authorization")
		if authToken == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		claims, err := auth.ValidateAgentToken(authToken)
		// TODO: Check agent status
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			c.Abort()
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Error().Err(err).Msg("Upgrade error")
			return
		}

		client := h.Register(claims.Subject, conn)

		// WritePump runs in background, reads from client.Send channel
		go h.WritePump(client, log)

		// ReadPump runs here (blocking), reads incoming messages from WS
		defer func() {
			h.Unregister(claims.Subject)
			client.Close()
		}()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Str("client", claims.Subject).Msg("Client disconnected")
				return
			}

			msg := &messages.ClientMessage{}
			if err := proto.Unmarshal(data, msg); err != nil {
				log.Error().Err(err).Str("client", claims.Subject).Msg("Unmarshal error")
				continue
			}

			handleClientMessage(claims.Subject, msg, h, log)
		}
	}
}

func handleClientMessage(id string, msg *messages.ClientMessage, h *Hub, log *zerolog.Logger) {
	switch p := msg.Payload.(type) {
	case *messages.ClientMessage_Pong:
		log.Info().Str("client", id).Msgf("Pong received, timestamp: %d", p.Pong.Timestamp)
		// TODO: Update status
	default:
		log.Warn().Str("client", id).Msg("Unknown message type received")
	}
}
