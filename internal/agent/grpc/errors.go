package grpc

import "errors"

var (
	// ErrNotConnected is returned when the client is not connected
	ErrNotConnected = errors.New("not connected to hub")
)
