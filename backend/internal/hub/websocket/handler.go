package websocket

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

// WSConn is an interface for WebSocket connections, allowing for testing.
type WSConn interface {
	ReadMessage() (messageType int, data []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

const pongWait = 90 * time.Second // must be greater than the worker ping interval (60s)
const handshakeTimeout = 15 * time.Second

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

		conn.SetReadLimit(20 << 20) // 20MB max message size to prevent DoS with large messages

		session, err := performHandshake(conn, claims.Subject, log)
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Handshake failed")
			err = conn.Close()
			if err != nil {
				log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to close connection after handshake failure")
			}
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			log.Error().Err(err).Msg("Failed to set read deadline")
			err = conn.Close()
			if err != nil {
				log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to close connection after read deadline error")
			}
			return
		}

		client, err := h.Register(claims.Subject, conn)
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to register client")
			err = conn.Close()
			if err != nil {
				log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to close connection after registration failure")
			}
			return
		}
		client.session = session

		_, err = gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(c.Request.Context(), "status", models.AgentStatusOnline)
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to update status to online")
		}
		// Application health is reported by the agent (see handleApplicationStatusReport)
		// once it has inspected its containers — the hub no longer assumes Healthy on connect.

		go h.WritePump(client, log)

		// Send current poll settings so the agent can start polling immediately,
		// without waiting for a future settings update or a manual trigger.
		apps, err := gorm.G[models.Application](db.DB).
			Where("agent_id = ?", claims.Subject).
			Find(c.Request.Context())
		if err != nil {
			log.Error().Err(err).Str("agent_id", claims.Subject).Msg("failed to fetch applications for AgentSettings")
		} else {
			h.SendAgentSettings(claims.Subject, apps)
		}

		defer func() {
			h.Unregister(claims.Subject)
			client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := gorm.G[models.Agent](db.DB).Where("id = ?", claims.Subject).Update(ctx, "status", models.AgentStatusOffline)
			if err != nil {
				log.Error().Err(err).Str("agent_id", claims.Subject).Msg("Failed to update status to offline")
			}
			_, applicationErr := gorm.G[models.Application](db.DB).Where("agent_id = ?", claims.Subject).Update(ctx, "health_status", models.UnknownHealth)
			if applicationErr != nil {
				log.Error().Err(applicationErr).Str("agent_id", claims.Subject).Msg("Failed to update application status to offline")
			}
			// Fail any deploy still in progress: the agent that owned it is gone, so
			// no result will arrive and the app would otherwise spin in syncing.
			_, syncErr := gorm.G[models.Application](db.DB).
				Where("agent_id = ? AND sync_status = ?", claims.Subject, models.Syncing).
				Update(ctx, "sync_status", models.OutOfSync)
			if syncErr != nil {
				log.Error().Err(syncErr).Str("agent_id", claims.Subject).Msg("Failed to reset applications stuck in syncing status")
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

			go handleClientMessage(client, msg, log)
		}
	}
}

func handleClientMessage(client *Client, msg *messages.ClientMessage, log *zerolog.Logger) {
	_, isEncrypted := msg.Payload.(*messages.ClientMessage_EncryptedPayload)
	if !isEncrypted && !wscrypto.AllowedUnencrypted(msg) {
		log.Warn().Str("client", client.Id).Msgf("dropping unencrypted message of type %T", msg.Payload)
		return
	}

	// Unwrap at most one layer of encryption to prevent a malicious client from
	// crafting a chain of nested EncryptedPayloads that causes unbounded recursion.
	if p, ok := msg.Payload.(*messages.ClientMessage_EncryptedPayload); ok {
		inner := &messages.ClientMessage{}
		if err := client.session.Decrypt(p.EncryptedPayload, inner); err != nil {
			log.Error().Err(err).Str("client", client.Id).Msg("Failed to decrypt message")
			return
		}
		if _, stillEncrypted := inner.Payload.(*messages.ClientMessage_EncryptedPayload); stillEncrypted {
			log.Warn().Str("client", client.Id).Msg("Dropping doubly-encrypted message")
			return
		}
		msg = inner
	}

	switch p := msg.Payload.(type) {
	case *messages.ClientMessage_Pong:
		log.Debug().Str("client", client.Id).Msgf("Pong received, timestamp: %d", p.Pong.Timestamp)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lastSeen := time.UnixMilli(p.Pong.Timestamp)
		_, err := gorm.G[models.Agent](db.DB).Where("id = ?", client.Id).Update(ctx, "last_seen", lastSeen)
		if err != nil {
			log.Error().Err(err).Str("client", client.Id).Msg("Failed to update last_seen")
		}
	case *messages.ClientMessage_DeployResult:
		handleDeployResult(p.DeployResult, log)
	case *messages.ClientMessage_DeleteResult:
		if DefaultHub == nil || !DefaultHub.ResolveDeleteResult(p.DeleteResult) {
			log.Warn().Str("client", client.Id).Str("request_id", p.DeleteResult.RequestId).Msg("received delete result for unknown request")
		}
	case *messages.ClientMessage_PullImagesResult:
		handlePullImagesResult(client, p.PullImagesResult, log)
	case *messages.ClientMessage_ApplicationStatusReport:
		handleApplicationStatusReport(client, p.ApplicationStatusReport, log)
	default:
		log.Warn().Str("client", client.Id).Msg("Unknown message type received")
	}
}

// handleApplicationStatusReport applies the health an agent reports for its
// applications. Updates are scoped to the reporting agent so an agent can only
// affect its own applications.
func handleApplicationStatusReport(client *Client, report *messages.ApplicationStatusReport, log *zerolog.Logger) {
	if report == nil || len(report.Statuses) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, status := range report.Statuses {
		if _, err := gorm.G[models.Application](db.DB).
			Where("id = ? AND agent_id = ?", status.ApplicationId, client.Id).
			Update(ctx, "health_status", protoHealthToModel(status.Health)); err != nil {
			log.Error().Err(err).
				Str("agent_id", client.Id).
				Str("applicationId", status.ApplicationId).
				Msg("failed to update application health from status report")
		}
	}

	sse.PublishUpdate("/api/v1/applications")
}

func protoHealthToModel(h messages.HealthStatus) models.HealthStatus {
	switch h {
	case messages.HealthStatus_HEALTH_STATUS_HEALTHY:
		return models.Healthy
	case messages.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return models.Unhealthy
	default:
		return models.UnknownHealth
	}
}

func performHandshake(conn WSConn, agentID string, log *zerolog.Logger) (*wscrypto.Session, error) {
	hubKeys, err := wscrypto.GenerateHubKeys()
	if err != nil {
		return nil, err
	}

	sig := auth.SignHandshake(wscrypto.HandshakeSignaturePayload(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, agentID))

	init := &messages.ServerMessage{
		Payload: &messages.ServerMessage_KeyExchangeInit{
			KeyExchangeInit: &messages.KeyExchangeInit{
				MlkemEncapsulationKey: hubKeys.MLKEMEncapKey,
				X25519PublicKey:       hubKeys.X25519PublicKey,
				HubSignature:          sig,
			},
		},
	}
	initData, err := proto.Marshal(init)
	if err != nil {
		return nil, err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
		return nil, err
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, initData); err != nil {
		return nil, err
	}

	if err := conn.SetReadDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return nil, err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	clientMsg := &messages.ClientMessage{}
	if err := proto.Unmarshal(data, clientMsg); err != nil {
		return nil, err
	}
	resp, ok := clientMsg.Payload.(*messages.ClientMessage_KeyExchangeResponse)
	if !ok {
		return nil, fmt.Errorf("expected KeyExchangeResponse, got %T", clientMsg.Payload)
	}

	sessionKey, err := wscrypto.HubDeriveSessionKey(hubKeys, resp.KeyExchangeResponse.MlkemCiphertext, resp.KeyExchangeResponse.AgentX25519PublicKey, agentID)
	if err != nil {
		return nil, err
	}

	log.Debug().Str("agent_id", agentID).Msg("WS handshake complete")
	return wscrypto.NewSession(sessionKey)
}
