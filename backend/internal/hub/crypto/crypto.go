package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	return string(ciphertext) + hex.EncodeToString(nonce), nil
}

func Decrypt(ciphertext string) (string, error) {
	aead, err := aegis256x2.New(globalKey, 32)
	if err != nil {
		return "", err
	}

	nonceHex := ciphertext[len(ciphertext)-hex.EncodedLen(aegis256x2.NonceSize):]
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < hex.EncodedLen(aegis256x2.NonceSize) {
		return "", fmt.Errorf("ciphertext must be at least %d characters long", hex.EncodedLen(aegis256x2.NonceSize))
	}

	ciphertext = ciphertext[:len(ciphertext)-hex.EncodedLen(aegis256x2.NonceSize)]

	decrypted, err := aead.Open(nil, nonce, []byte(ciphertext), nil)
	if err != nil {
		return "", err
	}

	return string(decrypted), nil
}
