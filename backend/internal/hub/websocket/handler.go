package websocket

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/OrcaCD/orca-cd/internal/shared/wscrypto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

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
	default:
		log.Warn().Str("client", client.Id).Msg("Unknown message type received")
	}
}

func performHandshake(conn *websocket.Conn, agentID string, log *zerolog.Logger) (*wscrypto.Session, error) {
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
