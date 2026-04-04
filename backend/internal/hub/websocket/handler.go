package websocket

import (
	"context"
	"net/http"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{}

func WsHandler(h *Hub, log *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken := c.GetHeader("Authorization")
		if authToken == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		claims, err := auth.ValidateAgentToken(authToken)
		if err != nil {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		agent, err := gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).First(c.Request.Context())

		if err != nil {
			if err == gorm.ErrRecordNotFound {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Database error")
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		if agent.KeyId.String() != claims.KeyId {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Error().Err(err).Msg("Upgrade error")
			return
		}

		client := h.Register(claims.Subject, conn)
		gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(c.Request.Context(), "status", models.AgentStatusOnline)

		// WritePump runs in background, reads from client.Send channel
		go h.WritePump(client, log)

		// ReadPump runs here (blocking), reads incoming messages from WS
		defer func() {
			h.Unregister(claims.Subject)
			client.Close()
			gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(context.Background(), "status", models.AgentStatusOffline)
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

			handleClientMessage(claims.Subject, msg, log)
		}
	}
}

func handleClientMessage(id string, msg *messages.ClientMessage, log *zerolog.Logger) {
	switch p := msg.Payload.(type) {
	case *messages.ClientMessage_Pong:
		log.Info().Str("client", id).Msgf("Pong received, timestamp: %d", p.Pong.Timestamp)
		ctx := context.Background() // TODO: Check if gin context can be passed here for timeout
		gorm.G[models.Agent](db.DB).Where("id = ?", id).Update(ctx, "last_seen", p.Pong.Timestamp)
	default:
		log.Warn().Str("client", id).Msg("Unknown message type received")
	}
}
