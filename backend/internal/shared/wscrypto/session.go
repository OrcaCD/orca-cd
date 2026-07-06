package wscrypto

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	"github.com/aegis-aead/go-libaegis/aegis256"
	"google.golang.org/protobuf/proto"
)

// per-connection AEGIS-256 for encrypting/decrypting WS messages.
type Session struct {
	aead cipher.AEAD
}

func NewSession(sessionKey []byte) (*Session, error) {
	aead, err := aegis256.New(sessionKey, 32)
	if err != nil {
		return nil, fmt.Errorf("aegis256 init: %w", err)
	}
	return &Session{aead: aead}, nil
}

func (s *Session) Encrypt(inner proto.Message) (*messages.EncryptedPayload, error) {
	plaintext, err := proto.Marshal(inner)
	if err != nil {
		return nil, fmt.Errorf("marshal inner message: %w", err)
	}

	nonce := make([]byte, aegis256.NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := s.aead.Seal(nil, nonce, plaintext, nil)

	return &messages.EncryptedPayload{
		Nonce:      nonce,
		Ciphertext: ciphertext,
	}, nil
}

func (s *Session) Decrypt(env *messages.EncryptedPayload, dst proto.Message) error {
	if len(env.Nonce) != aegis256.NonceSize {
		return fmt.Errorf("invalid nonce length: got %d, want %d", len(env.Nonce), aegis256.NonceSize)
	}

	plaintext, err := s.aead.Open(nil, env.Nonce, env.Ciphertext, nil)
	if err != nil {
		return fmt.Errorf("aegis256 open: %w", err)
	}

	if err := proto.Unmarshal(plaintext, dst); err != nil {
		return fmt.Errorf("unmarshal decrypted message: %w", err)
	}
	return nil
}
