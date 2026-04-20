package wscrypto

import (
	"bytes"
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func newTestSession(t *testing.T) *Session {
	t.Helper()
	key := make([]byte, 32)
	s, err := NewSession(key)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return s
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := newTestSession(t)

	inner := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 12345},
		},
	}

	env, err := s.Encrypt(inner)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	dst := &messages.ServerMessage{}
	if err := s.Decrypt(env, dst); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	ping, ok := dst.Payload.(*messages.ServerMessage_Ping)
	if !ok {
		t.Fatal("unexpected payload type after decrypt")
	}
	if ping.Ping.Timestamp != 12345 {
		t.Errorf("timestamp: got %d, want 12345", ping.Ping.Timestamp)
	}
}

func TestDecryptWrongNonce(t *testing.T) {
	s := newTestSession(t)
	env, _ := s.Encrypt(&messages.PingRequest{Timestamp: 1})
	env.Nonce[0] ^= 0xFF
	if err := s.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for wrong nonce")
	}
}

func TestDecryptCorruptCiphertext(t *testing.T) {
	s := newTestSession(t)
	env, _ := s.Encrypt(&messages.PingRequest{Timestamp: 1})
	env.Ciphertext[0] ^= 0xFF
	if err := s.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for corrupt ciphertext")
	}
}

func TestEncryptUniqueCiphertexts(t *testing.T) {
	s := newTestSession(t)
	msg := &messages.PingRequest{Timestamp: 42}

	env1, _ := s.Encrypt(msg)
	env2, _ := s.Encrypt(msg)

	if bytes.Equal(env1.Ciphertext, env2.Ciphertext) {
		t.Fatal("expected unique ciphertexts per encryption (random nonce)")
	}
	if bytes.Equal(env1.Nonce, env2.Nonce) {
		t.Fatal("expected unique nonces per encryption")
	}
}

func TestDecryptInvalidNonceLength(t *testing.T) {
	s := newTestSession(t)
	env := &messages.EncryptedPayload{
		Nonce:      []byte{0x00},
		Ciphertext: []byte{0x00},
	}
	if err := s.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for invalid nonce length")
	}
}
