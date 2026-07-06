package auth

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
)

// Used during the WebSocket handshake to prove hub identity to the agent.

var handshakePrivKey ed25519.PrivateKey

func initHandshake(appSecret string) error {
	seed, err := hkdf.Key(sha256.New, []byte(appSecret), nil, "HANDSHAKE_SIGNING_KEY", ed25519.SeedSize)
	if err != nil {
		return fmt.Errorf("auth.initHandshake: %w", err)
	}
	handshakePrivKey = ed25519.NewKeyFromSeed(seed)
	return nil
}

func SignHandshake(payload []byte) []byte {
	return ed25519.Sign(handshakePrivKey, payload)
}
