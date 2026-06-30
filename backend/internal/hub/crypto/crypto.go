package crypto

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/aegis-aead/go-libaegis/aegis256"
)

var defaultCipher *Cipher

type Cipher struct {
	aead          cipher.AEAD
	blindIndexKey []byte
}

func New(appSecret string) (*Cipher, error) {
	key, err := deriveKey(appSecret, "DB_ENCRYPTION_KEY")
	if err != nil {
		return nil, err
	}
	aead, err := aegis256.New(key, 32)
	if err != nil {
		return nil, err
	}
	blindIndexKey, err := deriveKey(appSecret, "DB_BLIND_INDEX_KEY")
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, blindIndexKey: blindIndexKey}, nil
}

func Init(appSecret string) error {
	cipher, err := New(appSecret)
	if err != nil {
		return err
	}
	SetDefault(cipher)
	return nil
}

func SetDefault(cipher *Cipher) {
	defaultCipher = cipher
}

func Encrypt(plaintext string) (string, error) {
	return defaultCipher.Encrypt(plaintext)
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("crypto cipher is not initialized")
	}

	nonce := make([]byte, aegis256.NonceSize)

	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// Seal appends ciphertext to nonce, producing nonce || ciphertext.
	data := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(data), nil
}

func Decrypt(encoded string) (string, error) {
	return defaultCipher.Decrypt(encoded)
}

func (c *Cipher) Decrypt(encoded string) (string, error) {
	if c == nil || c.aead == nil {
		return "", errors.New("crypto cipher is not initialized")
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	if len(data) < aegis256.NonceSize {
		return "", fmt.Errorf("ciphertext must be at least %d bytes long", aegis256.NonceSize)
	}

	nonce := data[:aegis256.NonceSize]
	ciphertext := data[aegis256.NonceSize:]

	decrypted, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(decrypted), nil
}

// BlindIndex returns a deterministic, keyed hash of plaintext suitable for
// equality lookups on encrypted columns (a "blind index"). It is keyed with a
// secret derived from APP_SECRET so the stored value does not leak the plaintext
// to anyone without the key. Callers must normalize the input themselves if the
// comparison should be case- or whitespace-insensitive.
func BlindIndex(plaintext string) string {
	return defaultCipher.BlindIndex(plaintext)
}

func (c *Cipher) BlindIndex(plaintext string) string {
	mac := hmac.New(sha256.New, c.blindIndexKey)
	mac.Write([]byte(plaintext))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func deriveKey(secret, info string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(secret), nil, info, 32)
}
