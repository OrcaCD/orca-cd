package websocket

import (
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/rs/zerolog"
)

type Worker struct {
	hub *Hub
	log *zerolog.Logger
}

func NewWorker(h *Hub, log *zerolog.Logger) *Worker {
	return &Worker{hub: h, log: log}
}

// Start begins a background ticker that broadcasts a message every 60 seconds.
func (w *Worker) Start() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for t := range ticker.C {
			w.log.Debug().Msgf("Worker broadcasting ping: %s", t)
			w.hub.Broadcast(&messages.ServerMessage{
				Payload: &messages.ServerMessage_Ping{
					Ping: &messages.PingRequest{
						Timestamp: time.Now().UnixMilli(),
					},
				},
			})
		}
	}()
}
