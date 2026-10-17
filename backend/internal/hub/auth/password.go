package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alexedwards/argon2id"
)

const maxHashSlots = 4

var (
	ErrEmptyPassword = errors.New("password must not be empty")
	ErrBusy          = errors.New("too many concurrent password operations")

	slotWaitTimeout = 5 * time.Second
)

func ValidatePasswordStrength(password string) bool {
	if utf8.RuneCountInString(password) < 12 {
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

	dummyHash   string
	dummyHashMu sync.Mutex

	// hashSlots bounds concurrent argon2 operations (128 MiB each) across all
	// clients, so peak memory doesn't depend on how many IPs hit the login.
	hashSlots = make(chan struct{}, min(max(runtime.NumCPU(), 1), maxHashSlots))
)

func initPassword() error {
	dummyHashMu.Lock()
	defer dummyHashMu.Unlock()
	if dummyHash != "" {
		return nil
	}

	h, err := hashPassword("orca-cd-dummy-timing-password")
	if err != nil {
		return fmt.Errorf("failed to create dummy password hash: %w", err)
	}
	dummyHash = h
	return nil
}

func withSlot[T any](ctx context.Context, wait time.Duration, fn func() T) (T, error) {
	var zero T

	var timeout <-chan time.Time
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case hashSlots <- struct{}{}:
	case <-timeout:
		return zero, ErrBusy
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	defer func() { <-hashSlots }()

	return fn(), nil
}

func hashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	return argon2id.CreateHash(password, params)
}

func checkPassword(password, hash string) bool {
	if password == "" || hash == "" {
		return false
	}
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false
	}
	return match
}

type hashResult struct {
	hash string
	err  error
}

func hashWithSlot(ctx context.Context, wait time.Duration, password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	res, err := withSlot(ctx, wait, func() hashResult {
		h, err := hashPassword(password)
		return hashResult{h, err}
	})
	if err != nil {
		return "", err
	}
	return res.hash, res.err
}

// HashPasswordContext is like HashPassword but gives up with ErrBusy if no
// hashing slot frees up within slotWaitTimeout, or with ctx.Err() on cancellation.
func HashPasswordContext(ctx context.Context, password string) (string, error) {
	return hashWithSlot(ctx, slotWaitTimeout, password)
}

// CheckPasswordContext is like CheckPassword but gives up with ErrBusy if no
// hashing slot frees up within slotWaitTimeout, or with ctx.Err() on cancellation.
func CheckPasswordContext(ctx context.Context, password, hash string) (bool, error) {
	if password == "" || hash == "" {
		return false, nil
	}
	return withSlot(ctx, slotWaitTimeout, func() bool { return checkPassword(password, hash) })
}

// CompareWithDummyContext runs an argon2id comparison against a pre-computed
// dummy hash so that unknown users cost the same as known ones.
func CompareWithDummyContext(ctx context.Context, password string) error {
	_, err := CheckPasswordContext(ctx, password, dummyHash)
	return err
}

// HashPassword blocks until a hashing slot is available.
func HashPassword(password string) (string, error) {
	return hashWithSlot(context.Background(), 0, password)
}

// CheckPassword blocks until a hashing slot is available.
func CheckPassword(password, hash string) bool {
	match, err := withSlot(context.Background(), 0, func() bool { return checkPassword(password, hash) })
	return err == nil && match
}

// CompareWithDummy runs an argon2id comparison against a pre-computed dummy
// hash. The result is always false. This prevents timing attacks that could reveal
// whether a user exists based on how long the password check takes.
func CompareWithDummy(password string) {
	CheckPassword(password, dummyHash)
}

func GenerateRandomPassword() (string, error) {
	const (
		uppers  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		lowers  = "abcdefghijklmnopqrstuvwxyz"
		digits  = "0123456789"
		special = "!@#$%^&*()-_=+[]{}|;:,.<>?"
	)
	all := uppers + lowers + digits + special

	// randIndex returns a uniform random integer in [0, n) via rejection sampling.
	randIndex := func(n int) (int, error) {
		limit := n * (256 / n)
		b := make([]byte, 1)
		for {
			if _, err := rand.Read(b); err != nil {
				return 0, err
			}
			if int(b[0]) < limit {
				return int(b[0]) % n, nil
			}
		}
	}

	pick := func(charset string) (byte, error) {
		i, err := randIndex(len(charset))
		if err != nil {
			return 0, err
		}
		return charset[i], nil
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
	// Fisher-Yates shuffle using rejection sampling for unbiased j.
	for i := len(result) - 1; i > 0; i-- {
		j, err := randIndex(i + 1)
		if err != nil {
			return "", err
		}
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
