package grpc

import "errors"

var (
	// ErrAgentNotFound is returned when an agent is not found
	ErrAgentNotFound = errors.New("agent not found")
)
