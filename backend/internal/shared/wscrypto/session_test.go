package wscrypto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"testing"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
)

func newTestSessionPair(t *testing.T) (hub, agent *Session) {
	t.Helper()
	key := make([]byte, 32)
	hub, err := NewSession(key, RoleHub)
	if err != nil {
		t.Fatalf("NewSession hub: %v", err)
	}
	agent, err = NewSession(key, RoleAgent)
	if err != nil {
		t.Fatalf("NewSession agent: %v", err)
	}
	return hub, agent
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	hub, agent := newTestSessionPair(t)

	inner := &messages.ServerMessage{
		Payload: &messages.ServerMessage_Ping{
			Ping: &messages.PingRequest{Timestamp: 12345},
		},
	}

	env, err := hub.Encrypt(inner)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	dst := &messages.ServerMessage{}
	if err := agent.Decrypt(env, dst); err != nil {
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

func TestBidirectional(t *testing.T) {
	hub, agent := newTestSessionPair(t)

	for i := range 3 {
		env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: int64(i)})
		if err := agent.Decrypt(env, &messages.PingRequest{}); err != nil {
			t.Fatalf("agent decrypt %d: %v", i, err)
		}
		env, _ = agent.Encrypt(&messages.PongResponse{Timestamp: int64(i)})
		if err := hub.Decrypt(env, &messages.PongResponse{}); err != nil {
			t.Fatalf("hub decrypt %d: %v", i, err)
		}
	}
}

func TestDecryptWrongNonce(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})
	env.Nonce[len(env.Nonce)-1] ^= 0xFF
	if err := agent.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for wrong nonce")
	}
}

func TestDecryptCorruptCiphertext(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})
	env.Ciphertext[0] ^= 0xFF
	if err := agent.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for corrupt ciphertext")
	}
}

func TestEncryptUniqueCiphertexts(t *testing.T) {
	hub, _ := newTestSessionPair(t)
	msg := &messages.PingRequest{Timestamp: 42}

	env1, _ := hub.Encrypt(msg)
	env2, _ := hub.Encrypt(msg)

	if bytes.Equal(env1.Ciphertext, env2.Ciphertext) {
		t.Fatal("expected unique ciphertexts per encryption")
	}
	if bytes.Equal(env1.Nonce, env2.Nonce) {
		t.Fatal("expected unique nonces per encryption")
	}
}

func TestEncryptConcurrentNoncesUnique(t *testing.T) {
	hub, _ := newTestSessionPair(t)
	const n = 200

	nonces := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			env, err := hub.Encrypt(&messages.PingRequest{})
			if err != nil {
				t.Errorf("Encrypt: %v", err)
				return
			}
			nonces[i] = string(env.Nonce)
		})
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, nonce := range nonces {
		if _, dup := seen[nonce]; dup {
			t.Fatal("duplicate nonce under concurrent Encrypt")
		}
		seen[nonce] = struct{}{}
	}
}

func TestEncryptFailsWhenCounterExhausted(t *testing.T) {
	hub, _ := newTestSessionPair(t)
	hub.sendCounter.Store(math.MaxUint64 - 1)

	if _, err := hub.Encrypt(&messages.PingRequest{}); err != nil {
		t.Fatalf("last counter value should still work: %v", err)
	}
	for range 3 {
		if _, err := hub.Encrypt(&messages.PingRequest{}); !errors.Is(err, ErrCounterExhausted) {
			t.Fatalf("got %v, want ErrCounterExhausted", err)
		}
	}
}

func TestDecryptRejectsReplay(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})

	if err := agent.Decrypt(env, &messages.PingRequest{}); err != nil {
		t.Fatalf("first Decrypt: %v", err)
	}
	if err := agent.Decrypt(env, &messages.PingRequest{}); !errors.Is(err, ErrReplay) {
		t.Fatalf("replayed frame: got %v, want ErrReplay", err)
	}
}

func TestDecryptRejectsOutOfOrder(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	env1, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})
	env2, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 2})

	if err := agent.Decrypt(env2, &messages.PingRequest{}); err != nil {
		t.Fatalf("Decrypt newer frame: %v", err)
	}
	if err := agent.Decrypt(env1, &messages.PingRequest{}); !errors.Is(err, ErrReplay) {
		t.Fatalf("older frame: got %v, want ErrReplay", err)
	}
}

func TestDecryptAllowsGaps(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	env1, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})
	_, _ = hub.Encrypt(&messages.PingRequest{Timestamp: 2}) // never delivered
	env3, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 3})

	if err := agent.Decrypt(env1, &messages.PingRequest{}); err != nil {
		t.Fatalf("Decrypt first: %v", err)
	}
	if err := agent.Decrypt(env3, &messages.PingRequest{}); err != nil {
		t.Fatalf("Decrypt after gap: %v", err)
	}
}

func TestDecryptRejectsReflectedFrame(t *testing.T) {
	hub, _ := newTestSessionPair(t)
	env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})

	if err := hub.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected a frame to be rejected by its own sender")
	}
}

func TestDecryptForgedFrameDoesNotAdvanceCounter(t *testing.T) {
	hub, agent := newTestSessionPair(t)

	forged := make([]byte, 32)
	forged[0] = byte(RoleHub)
	binary.BigEndian.PutUint64(forged[1:], 1<<62)
	if err := agent.Decrypt(&messages.EncryptedPayload{Nonce: forged, Ciphertext: make([]byte, 32)}, &messages.PingRequest{}); err == nil {
		t.Fatal("expected forged frame to be rejected")
	}

	env, _ := hub.Encrypt(&messages.PingRequest{Timestamp: 1})
	if err := agent.Decrypt(env, &messages.PingRequest{}); err != nil {
		t.Fatalf("legit frame rejected after forged one: %v", err)
	}
}

func TestDecryptInvalidNonceLength(t *testing.T) {
	_, agent := newTestSessionPair(t)
	env := &messages.EncryptedPayload{
		Nonce:      []byte{0x00},
		Ciphertext: []byte{0x00},
	}
	if err := agent.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error for invalid nonce length")
	}
}

func TestNewSession_InvalidKeySize(t *testing.T) {
	_, err := NewSession([]byte{0x01, 0x02}, RoleHub) // too short, aegis256 requires 32 bytes
	if err == nil {
		t.Fatal("expected error for invalid key size")
	}
}

func TestNewSession_InvalidRole(t *testing.T) {
	if _, err := NewSession(make([]byte, 32), Role(0)); err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestDecrypt_InvalidProtoBytes(t *testing.T) {
	hub, agent := newTestSessionPair(t)
	nonce := make([]byte, 32) // aegis256.NonceSize == 32
	nonce[0] = byte(RoleHub)
	binary.BigEndian.PutUint64(nonce[1:], 1)
	invalidProto := []byte{0xFF, 0xFF, 0xFF}
	ct := hub.aead.Seal(nil, nonce, invalidProto, nil)
	env := &messages.EncryptedPayload{Nonce: nonce, Ciphertext: ct}
	if err := agent.Decrypt(env, &messages.PingRequest{}); err == nil {
		t.Fatal("expected error when decrypted bytes are not valid protobuf")
	}
}
