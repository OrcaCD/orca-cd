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

// Start begins a background ticker that broadcasts a message every 10 seconds.
func (w *Worker) Start() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for t := range ticker.C {
			w.log.Info().Msgf("Worker broadcasting tick: %s", t)
			w.hub.Broadcast(&messages.ServerMessage{
				Payload: &messages.ServerMessage_Data{
					Data: &messages.DataResponse{
						Key:   "tick",
						Value: t.Format(time.RFC3339),
					},
				},
			})
		}
	}()
}
