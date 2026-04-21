package wscrypto

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

const sessionKeyInfo = "WS_SESSION_KEY_v1"

type HubHandshakeKeys struct {
	mlkemDecapKey   *mlkem.DecapsulationKey768
	x25519PrivKey   *ecdh.PrivateKey
	MLKEMEncapKey   []byte // 1184 bytes — sent to agent in KeyExchangeInit
	X25519PublicKey []byte // 32 bytes  — sent to agent in KeyExchangeInit
}

func GenerateHubKeys() (*HubHandshakeKeys, error) {
	decapKey, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, fmt.Errorf("mlkem key generation: %w", err)
	}

	x25519Priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("x25519 key generation: %w", err)
	}

	return &HubHandshakeKeys{
		mlkemDecapKey:   decapKey,
		x25519PrivKey:   x25519Priv,
		MLKEMEncapKey:   decapKey.EncapsulationKey().Bytes(),
		X25519PublicKey: x25519Priv.PublicKey().Bytes(),
	}, nil
}

// Called by the agent
func AgentHandshake(mlkemEncapKeyBytes, hubX25519PubBytes []byte, agentID string) (mlkemCiphertext, agentX25519PubKey, sessionKey []byte, err error) {
	encapKey, err := mlkem.NewEncapsulationKey768(mlkemEncapKeyBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse mlkem encapsulation key: %w", err)
	}

	mlkemShared, mlkemCiphertext := encapKey.Encapsulate()

	agentX25519Priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("x25519 key generation: %w", err)
	}

	hubX25519Pub, err := ecdh.X25519().NewPublicKey(hubX25519PubBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse hub x25519 public key: %w", err)
	}

	x25519Shared, err := agentX25519Priv.ECDH(hubX25519Pub)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("x25519 ecdh: %w", err)
	}

	sessionKey, err = deriveSessionKey(mlkemShared, x25519Shared, agentID)
	if err != nil {
		return nil, nil, nil, err
	}

	return mlkemCiphertext, agentX25519Priv.PublicKey().Bytes(), sessionKey, nil
}

// Called by the hub after receiving the agent's KeyExchangeResponse message
func HubDeriveSessionKey(keys *HubHandshakeKeys, mlkemCiphertext, agentX25519PubBytes []byte, agentID string) ([]byte, error) {
	mlkemShared, err := keys.mlkemDecapKey.Decapsulate(mlkemCiphertext)
	if err != nil {
		return nil, fmt.Errorf("mlkem decapsulate: %w", err)
	}

	agentX25519Pub, err := ecdh.X25519().NewPublicKey(agentX25519PubBytes)
	if err != nil {
		return nil, fmt.Errorf("parse agent x25519 public key: %w", err)
	}

	x25519Shared, err := keys.x25519PrivKey.ECDH(agentX25519Pub)
	if err != nil {
		return nil, fmt.Errorf("x25519 ecdh: %w", err)
	}

	return deriveSessionKey(mlkemShared, x25519Shared, agentID)
}

// combines the two shared secrets (both fixed-length: 32 bytes each) via HKDF-SHA256
func deriveSessionKey(mlkemShared, x25519Shared []byte, agentID string) ([]byte, error) {
	combined := make([]byte, len(mlkemShared)+len(x25519Shared))
	copy(combined[:len(mlkemShared)], mlkemShared)
	copy(combined[len(mlkemShared):], x25519Shared)

	key, err := hkdf.Key(sha256.New, combined, []byte(agentID), sessionKeyInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("hkdf key derivation: %w", err)
	}
	return key, nil
}
