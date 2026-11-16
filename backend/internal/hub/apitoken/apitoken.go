// Package apitoken creates the personal access tokens users call the REST API
// with, and the hashes they are stored as.
//
// A token is "orca_" followed by 32 random bytes in base64url. The tokens are
// random and long, so a plain SHA-256 hash is enough to store them; a slow
// password hash would only make every API request slower. The hash does not
// depend on the app secret, so rotating the key keeps the tokens working.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const (
	Prefix = "orca_"

	secretBytes = 32
	// displayLength is how much of the token is kept as its prefix, so users can
	// tell their tokens apart without the prefix being guessable.
	displayLength = len(Prefix) + 8
)

var secretLength = base64.RawURLEncoding.EncodedLen(secretBytes)

type Token struct {
	// Plain is the token the user gets once. It is never stored.
	Plain string
	// Prefix is the start of the plain token, which is shown in the UI.
	Prefix string
	// Hash is what is stored in the database.
	Hash string
}

func Generate() Token {
	secret := make([]byte, secretBytes)
	_, _ = rand.Read(secret) // never returns an error, see crypto/rand.Read

	plain := Prefix + base64.RawURLEncoding.EncodeToString(secret)
	return Token{
		Plain:  plain,
		Prefix: plain[:displayLength],
		Hash:   Hash(plain),
	}
}

func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// IsWellFormed reports whether s has the shape of a token, without telling
// whether the token exists.
func IsWellFormed(s string) bool {
	secret, ok := strings.CutPrefix(s, Prefix)
	if !ok || len(secret) != secretLength {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(secret)
	return err == nil
}
