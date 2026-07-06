package wscrypto

import (
	"bytes"
	"crypto/subtle"
	"testing"
)

func TestFullHandshake(t *testing.T) {
	hubKeys, err := GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}

	mlkemCiphertext, agentX25519Pub, agentSessionKey, err := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-1")
	if err != nil {
		t.Fatalf("AgentHandshake: %v", err)
	}

	hubSessionKey, err := HubDeriveSessionKey(hubKeys, mlkemCiphertext, agentX25519Pub, "agent-1")
	if err != nil {
		t.Fatalf("HubDeriveSessionKey: %v", err)
	}

	if subtle.ConstantTimeCompare(agentSessionKey, hubSessionKey) != 1 {
		t.Fatal("agent and hub derived different session keys")
	}
}

func TestHandshakeDifferentAgentIDs(t *testing.T) {
	hubKeys, _ := GenerateHubKeys()
	mlkemCiphertext, agentX25519Pub, agentKey, _ := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-A")
	hubKey, _ := HubDeriveSessionKey(hubKeys, mlkemCiphertext, agentX25519Pub, "agent-B")

	if bytes.Equal(agentKey, hubKey) {
		t.Fatal("expected different keys for different agentIDs")
	}
}

// ML-KEM uses implicit rejection: a corrupt ciphertext does not return an error —
// it produces a wrong (pseudo-random) shared secret. The session mismatch is only
// detectable when the AEGIS-256 authentication tag fails on the first encrypted message.
// This test verifies that a corrupt ciphertext produces a different session key.
func TestHandshakeCorruptCiphertextProducesDifferentKey(t *testing.T) {
	hubKeys, _ := GenerateHubKeys()
	mlkemCiphertext, agentX25519Pub, agentKey, _ := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-1")

	corruptedCiphertext := make([]byte, len(mlkemCiphertext))
	copy(corruptedCiphertext, mlkemCiphertext)
	corruptedCiphertext[0] ^= 0xFF

	hubKey, err := HubDeriveSessionKey(hubKeys, corruptedCiphertext, agentX25519Pub, "agent-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes.Equal(agentKey, hubKey) {
		t.Fatal("corrupt ciphertext should produce a different session key (implicit rejection)")
	}
}

func TestGenerateHubKeys_KeySizes(t *testing.T) {
	keys, err := GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	if len(keys.MLKEMEncapKey) != 1184 {
		t.Errorf("MLKEMEncapKey: got %d bytes, want 1184", len(keys.MLKEMEncapKey))
	}
	if len(keys.X25519PublicKey) != 32 {
		t.Errorf("X25519PublicKey: got %d bytes, want 32", len(keys.X25519PublicKey))
	}
}

func TestHandshakeSessionKeyLength(t *testing.T) {
	hubKeys, _ := GenerateHubKeys()
	mlkemCiphertext, agentX25519Pub, sessionKey, _ := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-1")
	if len(sessionKey) != 32 {
		t.Errorf("agentSessionKey: got %d bytes, want 32", len(sessionKey))
	}
	hubKey, _ := HubDeriveSessionKey(hubKeys, mlkemCiphertext, agentX25519Pub, "agent-1")
	if len(hubKey) != 32 {
		t.Errorf("hubSessionKey: got %d bytes, want 32", len(hubKey))
	}
}

func TestAgentHandshake_InvalidMLKEMKey(t *testing.T) {
	_, _, _, err := AgentHandshake([]byte("not-a-valid-mlkem-key"), make([]byte, 32), "agent-1")
	if err == nil {
		t.Fatal("expected error for invalid ML-KEM encapsulation key")
	}
}

func TestAgentHandshake_InvalidX25519Key(t *testing.T) {
	hubKeys, err := GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	_, _, _, err = AgentHandshake(hubKeys.MLKEMEncapKey, []byte("not-a-valid-x25519-key"), "agent-1")
	if err == nil {
		t.Fatal("expected error for invalid X25519 public key")
	}
}

func TestHubDeriveSessionKey_InvalidCiphertextSize(t *testing.T) {
	hubKeys, err := GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	_, agentX25519Pub, _, err := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-1")
	if err != nil {
		t.Fatalf("AgentHandshake: %v", err)
	}
	// ML-KEM-768 ciphertext must be exactly 1088 bytes; wrong size causes an error.
	_, err = HubDeriveSessionKey(hubKeys, []byte("too-short-ciphertext"), agentX25519Pub, "agent-1")
	if err == nil {
		t.Fatal("expected error for wrong-size ML-KEM ciphertext")
	}
}

func TestHubDeriveSessionKey_InvalidX25519Key(t *testing.T) {
	hubKeys, err := GenerateHubKeys()
	if err != nil {
		t.Fatalf("GenerateHubKeys: %v", err)
	}
	mlkemCiphertext, _, _, err := AgentHandshake(hubKeys.MLKEMEncapKey, hubKeys.X25519PublicKey, "agent-1")
	if err != nil {
		t.Fatalf("AgentHandshake: %v", err)
	}
	_, err = HubDeriveSessionKey(hubKeys, mlkemCiphertext, []byte("not-a-valid-x25519-key"), "agent-1")
	if err == nil {
		t.Fatal("expected error for invalid agent X25519 public key")
	}
}
