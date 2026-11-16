package auth

import (
	"crypto/rand"
	"encoding/base64"
)

func Init(appSecret, appURL string) error {
	if err := initJWT(appSecret, appURL); err != nil {
		return err
	}

	if err := initHandshake(appSecret); err != nil {
		return err
	}

	return nil
}

// GenerateRandomString generates opaque identifiers for agent tokens and webhooks.
func GenerateRandomString(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw)[:length], nil
}
