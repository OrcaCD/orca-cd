package wscrypto

import (
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func TestAllowedUnencrypted_ServerPing(t *testing.T) {
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{Ping: &messages.PingRequest{Timestamp: 1}},
	}
	if !AllowedUnencrypted(msg) {
		t.Error("expected ServerMessage Ping to be allowed unencrypted")
	}
}

func TestAllowedUnencrypted_ServerNonPing(t *testing.T) {
	msg := &messages.ServerMessage{
		Payload: &messages.ServerMessage_KeyExchangeInit{KeyExchangeInit: &messages.KeyExchangeInit{}},
	}
	if AllowedUnencrypted(msg) {
		t.Error("expected ServerMessage KeyExchangeInit to be disallowed unencrypted")
	}
}

func TestAllowedUnencrypted_ServerEmptyPayload(t *testing.T) {
	msg := &messages.ServerMessage{}
	if AllowedUnencrypted(msg) {
		t.Error("expected ServerMessage with nil payload to be disallowed unencrypted")
	}
}

func TestAllowedUnencrypted_ClientPong(t *testing.T) {
	msg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_Pong{Pong: &messages.PongResponse{Timestamp: 1}},
	}
	if !AllowedUnencrypted(msg) {
		t.Error("expected ClientMessage Pong to be allowed unencrypted")
	}
}

func TestAllowedUnencrypted_ClientNonPong(t *testing.T) {
	msg := &messages.ClientMessage{
		Payload: &messages.ClientMessage_KeyExchangeResponse{KeyExchangeResponse: &messages.KeyExchangeResponse{}},
	}
	if AllowedUnencrypted(msg) {
		t.Error("expected ClientMessage KeyExchangeResponse to be disallowed unencrypted")
	}
}

func TestAllowedUnencrypted_ClientEmptyPayload(t *testing.T) {
	msg := &messages.ClientMessage{}
	if AllowedUnencrypted(msg) {
		t.Error("expected ClientMessage with nil payload to be disallowed unencrypted")
	}
}

func TestAllowedUnencrypted_OtherType(t *testing.T) {
	if AllowedUnencrypted(&messages.PingRequest{Timestamp: 1}) {
		t.Error("expected non-ServerMessage/ClientMessage to be disallowed unencrypted")
	}
}
