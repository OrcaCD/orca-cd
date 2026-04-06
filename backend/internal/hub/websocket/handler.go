package websocket

import (
	"context"
	"net/http"
	"time"

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

const pongWait = 90 * time.Second // must be greater than the worker ping interval (60s)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

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

		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Error().Err(err).Msg("Failed to set read deadline")
			err = conn.Close()
			if err != nil {
				log.Error().Err(err).Msg("Failed to close connection after read deadline error")
			}
			return
		}

		client, err := h.Register(claims.Subject, conn)
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to register client")
			err = conn.Close()
			if err != nil {
				log.Error().Err(err).Msg("Failed to close connection after registration error")
			}
			return
		}

		_, err = gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(c.Request.Context(), "status", models.AgentStatusOnline)
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to update status to online")
		}

		go h.WritePump(client, log)

		defer func() {
			h.Unregister(claims.Subject)
			client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(ctx, "status", models.AgentStatusOffline)
			if err != nil {
				log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to update status to offline")
			}
		}()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Debug().Err(err).Str("client", claims.Subject).Msg("Client disconnected")
				return
			}
			if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
				log.Error().Err(err).Str("client", claims.Subject).Msg("Failed to reset read deadline")
				return
			}

			msg := &messages.ClientMessage{}
			if err := proto.Unmarshal(data, msg); err != nil {
				log.Error().Err(err).Str("client", claims.Subject).Msg("Unmarshal error")
				continue
			}

			go handleClientMessage(claims.Subject, msg, log)
		}
	}
}

func handleClientMessage(id string, msg *messages.ClientMessage, log *zerolog.Logger) {
	switch p := msg.Payload.(type) {
	case *messages.ClientMessage_Pong:
		log.Debug().Str("client", id).Msgf("Pong received, timestamp: %d", p.Pong.Timestamp)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lastSeen := time.Unix(p.Pong.Timestamp, 0)
		_, err := gorm.G[models.Agent](db.DB).Where("id = ?", id).Update(ctx, "last_seen", lastSeen)
		if err != nil {
			log.Error().Err(err).Str("client", id).Msg("Failed to update last_seen")
		}
	default:
		log.Warn().Str("client", id).Msg("Unknown message type received")
	}
}
