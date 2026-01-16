package grpc

import (
	"context"
	"sync"
	"time"

	pb "github.com/OrcaCD/orca-cd/api/proto"
	"github.com/OrcaCD/orca-cd/internal/agent/executor"
	"github.com/OrcaCD/orca-cd/internal/config"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Client represents the agent's gRPC client for communicating with the hub
type Client struct {
	agentID  string
	hubAddr  string
	executor *executor.Executor
	conn     *grpc.ClientConn
	stream   pb.HubService_AgentStreamClient
	mu       sync.RWMutex
	done     chan struct{}
}

// NewClient creates a new agent gRPC client
func NewClient(agentID, hubAddr string, exec *executor.Executor) *Client {
	return &Client{
		agentID:  agentID,
		hubAddr:  hubAddr,
		executor: exec,
		done:     make(chan struct{}),
	}
}

// Connect establishes a connection to the hub and starts streaming
func (c *Client) Connect(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		default:
		}

		if err := c.connectOnce(ctx); err != nil {
			log.Error().Err(err).Msg("Connection failed, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	// Establish gRPC connection
	conn, err := grpc.NewClient(c.hubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		conn.Close()
	}()

	client := pb.NewHubServiceClient(conn)

	// Start the bidirectional stream
	stream, err := client.AgentStream(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.stream = stream
	c.mu.Unlock()

	// Send registration
	if err := c.register(); err != nil {
		return err
	}

	// Start heartbeat goroutine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go c.heartbeatLoop(heartbeatCtx)

	// Process incoming messages
	return c.receiveLoop()
}

func (c *Client) register() error {
	msg := &pb.AgentMessage{
		AgentId: c.agentID,
		Payload: &pb.AgentMessage_Registration{
			Registration: &pb.AgentRegistration{
				AgentId:      c.agentID,
				Version:      config.Version,
				Capabilities: []string{"shell", "deploy", "build"},
				Labels: map[string]string{
					"os": "linux",
				},
			},
		},
	}

	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return ErrNotConnected
	}

	return stream.Send(msg)
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.sendHeartbeat(); err != nil {
				log.Error().Err(err).Msg("Failed to send heartbeat")
			}
		}
	}
}

func (c *Client) sendHeartbeat() error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return ErrNotConnected
	}

	runningTasks := c.executor.RunningTaskCount()

	msg := &pb.AgentMessage{
		AgentId: c.agentID,
		Payload: &pb.AgentMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Timestamp: time.Now().Unix(),
				Status: &pb.AgentStatus{
					State:        pb.AgentStatus_IDLE,
					RunningTasks: int32(runningTasks),
				},
			},
		},
	}

	if runningTasks > 0 {
		msg.Payload.(*pb.AgentMessage_Heartbeat).Heartbeat.Status.State = pb.AgentStatus_BUSY
	}

	return stream.Send(msg)
}

func (c *Client) receiveLoop() error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch payload := msg.Payload.(type) {
		case *pb.HubMessage_RegistrationAck:
			c.handleRegistrationAck(payload.RegistrationAck)
			
		case *pb.HubMessage_TaskAssignment:
			c.handleTaskAssignment(payload.TaskAssignment)
			
		case *pb.HubMessage_TaskCancel:
			c.handleTaskCancel(payload.TaskCancel)
		}
	}
}

func (c *Client) handleRegistrationAck(ack *pb.RegistrationAck) {
	if ack.Success {
		log.Info().Msg("Successfully registered with hub")
	} else {
		log.Error().Str("message", ack.Message).Msg("Registration failed")
	}
}

func (c *Client) handleTaskAssignment(task *pb.TaskAssignment) {
	log.Info().
		Str("task_id", task.TaskId).
		Str("type", task.TaskType).
		Msg("Received task assignment")

	// Execute the task
	go func() {
		result := c.executor.Execute(task)
		if err := c.sendTaskResult(result); err != nil {
			log.Error().Err(err).Str("task_id", task.TaskId).Msg("Failed to send task result")
		}
	}()
}

func (c *Client) handleTaskCancel(cancel *pb.TaskCancel) {
	log.Info().
		Str("task_id", cancel.TaskId).
		Str("reason", cancel.Reason).
		Msg("Received task cancellation")

	c.executor.Cancel(cancel.TaskId)
}

func (c *Client) sendTaskResult(result *pb.TaskResult) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return ErrNotConnected
	}

	msg := &pb.AgentMessage{
		AgentId: c.agentID,
		Payload: &pb.AgentMessage_TaskResult{
			TaskResult: result,
		},
	}

	return stream.Send(msg)
}

// SendProgress sends task progress update to the hub
func (c *Client) SendProgress(progress *pb.TaskProgress) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return ErrNotConnected
	}

	msg := &pb.AgentMessage{
		AgentId: c.agentID,
		Payload: &pb.AgentMessage_TaskProgress{
			TaskProgress: progress,
		},
	}

	return stream.Send(msg)
}

// Shutdown gracefully shuts down the client
func (c *Client) Shutdown() {
	close(c.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stream != nil {
		c.stream.CloseSend()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
