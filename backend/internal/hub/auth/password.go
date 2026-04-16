package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/alexedwards/argon2id"
)

var ErrEmptyPassword = errors.New("password must not be empty")

var (
	params = &argon2id.Params{
		Memory:      128 * 1024,
		Iterations:  3,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	dummyHash string
)

func initPassword() error {
	h, err := argon2id.CreateHash("orca-cd-dummy-timing-password", params)
	if err != nil {
		return fmt.Errorf("failed to create dummy password hash: %w", err)
	}
	dummyHash = h
	return nil
}

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(password, hash string) bool {
	if password == "" || hash == "" {
		return false
	}
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false
	}
	return match
}

// CompareWithDummy runs an argon2id comparison against a pre-computed dummy
// hash. The result is always false. This prevents timing attacks that could reveal
// whether a user exists based on how long the password check takes.
func CompareWithDummy(password string) {
	CheckPassword(password, dummyHash) //nolint:errcheck
}

func GenerateRandomPassword() (string, error) {
	return GenerateRandomString(20)
}

func GenerateRandomString(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw)[:length], nil
}
