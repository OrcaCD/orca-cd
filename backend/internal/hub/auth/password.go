package auth

import (
	"errors"

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
)

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
