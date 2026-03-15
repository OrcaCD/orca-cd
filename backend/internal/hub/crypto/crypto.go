package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

var globalKey []byte

func Init(encryptionKey string) error {
	// Try to hex-decode first (common for keys generated with openssl rand -hex 32).
	decoded, err := hex.DecodeString(encryptionKey)
	if err == nil && (len(decoded) == 16 || len(decoded) == 24 || len(decoded) == 32) {
		globalKey = decoded
		return nil
	}

	// If the raw bytes are already a valid AES key size, use them directly.
	raw := []byte(encryptionKey)
	if len(raw) == 16 || len(raw) == 24 || len(raw) == 32 {
		globalKey = raw
		return nil
	}

	// Otherwise derive a 32-byte key via SHA-256.
	sum := sha256.Sum256(raw)
	globalKey = sum[:]
	return nil
}

func Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(globalKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// nonce is prepended to the ciphertext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(globalKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
