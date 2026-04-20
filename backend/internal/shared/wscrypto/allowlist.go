package wscrypto

import (
	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"google.golang.org/protobuf/proto"
)

// Only low-sensitivity control messages (Ping / Pong) are permitted unencrypted.
func AllowedUnencrypted(msg proto.Message) bool {
	switch m := msg.(type) {
	case *messages.ServerMessage:
		_, ok := m.Payload.(*messages.ServerMessage_Ping)
		return ok
	case *messages.ClientMessage:
		_, ok := m.Payload.(*messages.ClientMessage_Pong)
		return ok
	default:
		return false
	}
}
