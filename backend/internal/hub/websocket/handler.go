package websocket

import (
	"net/http"

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
		id := c.Param("id")

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Error().Err(err).Msg("Upgrade error")
			return
		}

		client := h.Register(id, conn)

		// WritePump runs in background, reads from client.Send channel
		go h.WritePump(client, log)

		// ReadPump runs here (blocking), reads incoming messages from WS
		defer func() {
			h.Unregister(id)
			client.Close()
		}()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Str("client", id).Msg("Client disconnected")
				return
			}

			msg := &messages.ClientMessage{}
			if err := proto.Unmarshal(data, msg); err != nil {
				log.Error().Err(err).Str("client", id).Msg("Unmarshal error")
				continue
			}

			handleClientMessage(id, msg, h, log)
		}
	}
}

func handleClientMessage(id string, msg *messages.ClientMessage, h *Hub, log *zerolog.Logger) {
	switch p := msg.Payload.(type) {
	case *messages.ClientMessage_Ping:
		h.Send(id, &messages.ServerMessage{
			Payload: &messages.ServerMessage_Pong{
				Pong: &messages.PongResponse{Timestamp: p.Ping.Timestamp},
			},
		})
	case *messages.ClientMessage_Data:
		log.Error().Str("client", id).Str("key", p.Data.Key).Msg("Data request")
		h.Send(id, &messages.ServerMessage{
			Payload: &messages.ServerMessage_Data{
				Data: &messages.DataResponse{
					Key:   p.Data.Key,
					Value: "value-for-" + p.Data.Key,
				},
			},
		})
	}
}
