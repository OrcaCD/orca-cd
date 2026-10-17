package wscrypto

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/aegis-aead/go-libaegis/aegis256"
	"google.golang.org/protobuf/proto"
)

type Role byte

const (
	RoleHub   Role = 1
	RoleAgent Role = 2
)

func (r Role) peer() Role {
	if r == RoleHub {
		return RoleAgent
	}
	return RoleHub
}

var (
	ErrReplay           = errors.New("replayed or out-of-order message")
	ErrCounterExhausted = errors.New("session send counter exhausted, reconnect required")
)

// per-connection AEGIS-256 for encrypting/decrypting WS messages.
//
// Nonce layout: 1 byte sender role | 8 byte big-endian counter | zero padding.
// The role byte prevents reflecting a frame back to its sender, the counter prevents
// replay.
type Session struct {
	aead cipher.AEAD
	role Role

	sendCounter atomic.Uint64

	recvMu      sync.Mutex
	recvCounter uint64
}

func NewSession(sessionKey []byte, role Role) (*Session, error) {
	if role != RoleHub && role != RoleAgent {
		return nil, fmt.Errorf("invalid session role: %d", role)
	}
	aead, err := aegis256.New(sessionKey, 32)
	if err != nil {
		return nil, fmt.Errorf("aegis256 init: %w", err)
	}
	return &Session{aead: aead, role: role}, nil
}

func (s *Session) Encrypt(inner proto.Message) (*messages.EncryptedPayload, error) {
	plaintext, err := proto.Marshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal inner message: %w", err)
	}

	counter, err := s.nextCounter()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aegis256.NonceSize)
	nonce[0] = byte(s.role)
	binary.BigEndian.PutUint64(nonce[1:], counter)

	ciphertext := s.aead.Seal(nil, nonce, plaintext, nil)

	return &messages.EncryptedPayload{
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

func (s *Session) nextCounter() (uint64, error) {
	for {
		cur := s.sendCounter.Load()
		if cur == math.MaxUint64 {
			return 0, ErrCounterExhausted
		}
		if s.sendCounter.CompareAndSwap(cur, cur+1) {
			return cur + 1, nil
		}
	}
}

func (s *Session) Decrypt(env *messages.EncryptedPayload, dst proto.Message) error {
	if len(env.Nonce) != aegis256.NonceSize {
		return fmt.Errorf("invalid nonce length: got %d, want %d", len(env.Nonce), aegis256.NonceSize)
	}
	if Role(env.Nonce[0]) != s.role.peer() {
		return errors.New("invalid nonce: unexpected sender role")
	}
	counter := binary.BigEndian.Uint64(env.Nonce[1:])

	s.recvMu.Lock()
	defer s.recvMu.Unlock()

	if counter <= s.recvCounter {
		return ErrReplay
	}

	plaintext, err := s.aead.Open(nil, env.Nonce, env.Ciphertext, nil)
	if err != nil {
		return fmt.Errorf("aegis256 open: %w", err)
	}
	// Only advance after authentication so forged frames can't burn counter values.
	s.recvCounter = counter

	if err := proto.Unmarshal(plaintext, dst); err != nil {
		return fmt.Errorf("unmarshal decrypted message: %w", err)
	}
	return nil
}
