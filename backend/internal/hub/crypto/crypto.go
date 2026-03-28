package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/aegis-aead/go-libaegis/aegis256x2"
)

var globalKey []byte

func Init(appSecret string) error {
	hash := sha256.Sum256([]byte(appSecret + "_DB_ENCRYPTION_KEY"))
	globalKey = hash[:]
	return nil
}

func Encrypt(plaintext string) (string, error) {
	aead, err := aegis256x2.New(globalKey, 32)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aegis256x2.NonceSize)

	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// Seal appends ciphertext to nonce, producing nonce || ciphertext.
	data := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(data), nil
}

func Decrypt(encoded string) (string, error) {
	aead, err := aegis256x2.New(globalKey, 32)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	if len(data) < aegis256x2.NonceSize {
		return "", fmt.Errorf("ciphertext must be at least %d bytes long", aegis256x2.NonceSize)
	}

	nonce := data[:aegis256x2.NonceSize]
	ciphertext := data[aegis256x2.NonceSize:]

	decrypted, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(decrypted), nil
}
