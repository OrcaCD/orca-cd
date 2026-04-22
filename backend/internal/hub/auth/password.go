package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode"

	"github.com/alexedwards/argon2id"
)

var ErrEmptyPassword = errors.New("password must not be empty")

func ValidatePasswordStrength(password string) bool {
	if len([]rune(password)) < 12 {
		return false
	}
	var hasUpper, hasLower, hasNumber, hasSpecial bool
	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasNumber = true
		default:
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasNumber && hasSpecial
}

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
	const (
		uppers  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		lowers  = "abcdefghijklmnopqrstuvwxyz"
		digits  = "0123456789"
		special = "!@#$%^&*()-_=+[]{}|;:,.<>?"
	)
	all := uppers + lowers + digits + special

	// Rejection sampling: returns a random byte uniformly distributed over charset.
	pick := func(charset string) (byte, error) {
		limit := byte(len(charset) * (256 / len(charset)))
		b := make([]byte, 1)
		for {
			if _, err := rand.Read(b); err != nil {
				return 0, err
			}
			if b[0] < limit {
				return charset[int(b[0])%len(charset)], nil
			}
		}
	}

	result := make([]byte, 20)
	// Guarantee one character from each required category.
	for i, charset := range []string{uppers, lowers, digits, special} {
		c, err := pick(charset)
		if err != nil {
			return "", err
		}
		result[i] = c
	}
	// Fill remaining positions from the full charset.
	for i := 4; i < 20; i++ {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		result[i] = c
	}
	// Fisher-Yates shuffle using crypto/rand.
	for i := len(result) - 1; i > 0; i-- {
		b := make([]byte, 1)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		j := int(b[0]) % (i + 1)
		result[i], result[j] = result[j], result[i]
	}
	return string(result), nil
}

func GenerateRandomString(length int) (string, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(raw)[:length], nil
}
