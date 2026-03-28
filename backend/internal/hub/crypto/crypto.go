package crypto

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/aegis-aead/go-libaegis/aegis256"
)

var globalAead cipher.AEAD

func Init(appSecret string) error {
	key, err := deriveKey(appSecret, "DB_ENCRYPTION_KEY")
	if err != nil {
		return err
	}
	aead, err := aegis256.New(key, 32)
	if err != nil {
		return err
	}
	globalAead = aead
	return nil
}

func Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, aegis256.NonceSize)

	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// Seal appends ciphertext to nonce, producing nonce || ciphertext.
	data := globalAead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(data), nil
}

func Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	if len(data) < aegis256.NonceSize {
		return "", fmt.Errorf("ciphertext must be at least %d bytes long", aegis256.NonceSize)
	}

	nonce := data[:aegis256.NonceSize]
	ciphertext := data[aegis256.NonceSize:]

	decrypted, err := globalAead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(decrypted), nil
}

func deriveKey(secret, info string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(secret), nil, info, 32)
}
