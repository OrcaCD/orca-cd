// Package agenttoken encodes/decodes the compound token an admin copies from the
// hub UI into an agent's AUTH_TOKEN: a signed JWT (used for HTTP/WS bearer auth)
// with the agent's Ed25519 handshake-signing seed appended. The signing key never
// travels over the wire again after this and the agent uses it locally to sign its
// KeyExchangeResponse during the WS handshake.
package agenttoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

func Encode(jwt string, signingSeed []byte) (string, error) {
	if len(signingSeed) != ed25519.SeedSize {
		return "", fmt.Errorf("agenttoken.Encode: signing seed must be %d bytes, got %d", ed25519.SeedSize, len(signingSeed))
	}
	return jwt + "." + base64.RawURLEncoding.EncodeToString(signingSeed), nil
}

func Decode(token string) (jwt string, signingKey ed25519.PrivateKey, err error) {
	parts := strings.SplitN(token, ".", 4)
	if len(parts) != 4 {
		return "", nil, errors.New("token is missing signing-key segment; re-issue the agent token from the hub")
	}

	seed, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", nil, fmt.Errorf("agenttoken.Decode: invalid signing-key segment: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return "", nil, fmt.Errorf("agenttoken.Decode: signing-key segment must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}

	return strings.Join(parts[:3], "."), ed25519.NewKeyFromSeed(seed), nil
}
