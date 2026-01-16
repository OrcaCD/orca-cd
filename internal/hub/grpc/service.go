package grpc

import (
	"io"
	"sync"

	pb "github.com/OrcaCD/orca-cd/api/proto"
	"github.com/rs/zerolog/log"
)

// ConnectedAgent represents an agent connected via gRPC stream
type ConnectedAgent struct {
	ID           string
	Version      string
	Capabilities []string
	Labels       map[string]string
	Stream       pb.HubService_AgentStreamServer
	mu           sync.Mutex
}

// Send sends a message to the agent
func (a *ConnectedAgent) Send(msg *pb.HubMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Stream.Send(msg)
}

// HubService implements the gRPC HubService server
type HubService struct {
	pb.UnimplementedHubServiceServer
	agents sync.Map // map[string]*ConnectedAgent
}

// NewHubService creates a new HubService instance
func NewHubService() *HubService {
	return &HubService{}
}

// GetConnectedAgents returns a list of all connected agents
func (s *HubService) GetConnectedAgents() []*ConnectedAgent {
	var agents []*ConnectedAgent
	s.agents.Range(func(key, value any) bool {
		if agent, ok := value.(*ConnectedAgent); ok {
			agents = append(agents, agent)
		}
		return true
	})
	return agents
}

// GetAgent returns a specific agent by ID
func (s *HubService) GetAgent(id string) (*ConnectedAgent, bool) {
	if val, ok := s.agents.Load(id); ok {
		return val.(*ConnectedAgent), true
	}
	return nil, false
}

// AgentStream handles bidirectional streaming with agents
func (s *HubService) AgentStream(stream pb.HubService_AgentStreamServer) error {
	var agent *ConnectedAgent

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			log.Info().Msg("Agent stream closed by client")
			break
		}
		if err != nil {
			log.Error().Err(err).Msg("Error receiving from agent stream")
			break
		}

		switch payload := msg.Payload.(type) {
		case *pb.AgentMessage_Registration:
			agent = s.handleRegistration(msg.AgentId, payload.Registration, stream)
			
		case *pb.AgentMessage_Heartbeat:
			s.handleHeartbeat(msg.AgentId, payload.Heartbeat)
			
		case *pb.AgentMessage_TaskResult:
			s.handleTaskResult(msg.AgentId, payload.TaskResult)
			
		case *pb.AgentMessage_TaskProgress:
			s.handleTaskProgress(msg.AgentId, payload.TaskProgress)
		}
	}

	// Clean up agent connection
	if agent != nil {
		s.agents.Delete(agent.ID)
		log.Info().Str("agent_id", agent.ID).Msg("Agent disconnected")
	}

	return nil
}

func (s *HubService) handleRegistration(agentID string, reg *pb.AgentRegistration, stream pb.HubService_AgentStreamServer) *ConnectedAgent {
	agent := &ConnectedAgent{
		ID:           agentID,
		Version:      reg.Version,
		Capabilities: reg.Capabilities,
		Labels:       reg.Labels,
		Stream:       stream,
	}

	s.agents.Store(agentID, agent)

	log.Info().
		Str("agent_id", agentID).
		Str("version", reg.Version).
		Strs("capabilities", reg.Capabilities).
		Msg("Agent registered")

	// Send registration acknowledgment
	ack := &pb.HubMessage{
		Payload: &pb.HubMessage_RegistrationAck{
			RegistrationAck: &pb.RegistrationAck{
				Success: true,
				Message: "Registration successful",
			},
		},
	}

	if err := agent.Send(ack); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("Failed to send registration ack")
	}

	return agent
}

func (s *HubService) handleHeartbeat(agentID string, hb *pb.Heartbeat) {
	log.Debug().
		Str("agent_id", agentID).
		Int64("timestamp", hb.Timestamp).
		Msg("Heartbeat received")
}

func (s *HubService) handleTaskResult(agentID string, result *pb.TaskResult) {
	log.Info().
		Str("agent_id", agentID).
		Str("task_id", result.TaskId).
		Int32("status", int32(result.Status)).
		Msg("Task result received")

	// TODO: Store task result in database and notify relevant services
}

func (s *HubService) handleTaskProgress(agentID string, progress *pb.TaskProgress) {
	log.Debug().
		Str("agent_id", agentID).
		Str("task_id", progress.TaskId).
		Int32("percent", progress.PercentComplete).
		Str("message", progress.Message).
		Msg("Task progress received")

	// TODO: Broadcast progress updates via WebSocket to frontend
}

// AssignTask sends a task to a specific agent
func (s *HubService) AssignTask(agentID string, task *pb.TaskAssignment) error {
	agent, ok := s.GetAgent(agentID)
	if !ok {
		return ErrAgentNotFound
	}

	msg := &pb.HubMessage{
		Payload: &pb.HubMessage_TaskAssignment{
			TaskAssignment: task,
		},
	}

	return agent.Send(msg)
}

// CancelTask sends a cancellation request to an agent
func (s *HubService) CancelTask(agentID string, taskID string, reason string) error {
	agent, ok := s.GetAgent(agentID)
	if !ok {
		return ErrAgentNotFound
	}

	msg := &pb.HubMessage{
		Payload: &pb.HubMessage_TaskCancel{
			TaskCancel: &pb.TaskCancel{
				TaskId: taskID,
				Reason: reason,
			},
		},
	}

	return agent.Send(msg)
}
