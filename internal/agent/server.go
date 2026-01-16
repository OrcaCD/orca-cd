package agent

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	agentgrpc "github.com/OrcaCD/orca-cd/internal/agent/grpc"
	"github.com/OrcaCD/orca-cd/internal/agent/executor"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

func StartAgent(hubAddr, agentID string) {
	if agentID == "" {
		agentID = uuid.New().String()
	}

	log.Info().
		Str("agent_id", agentID).
		Str("hub", hubAddr).
		Msg("Starting OrcaCD Agent")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create task executor
	exec := executor.NewExecutor()

	// Create and start the agent client
	client := agentgrpc.NewClient(agentID, hubAddr, exec)
	
	go func() {
		if err := client.Connect(ctx); err != nil {
			log.Error().Err(err).Msg("Agent connection error")
			cancel()
		}
	}()

	// Wait for shutdown signal
	select {
	case sig := <-sigChan:
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
	case <-ctx.Done():
		log.Info().Msg("Context cancelled")
	}

	client.Shutdown()
	log.Info().Msg("Agent shutdown complete")
}
