// Package setupcode guards the first-run registration. While no user exists,
// the hub generates a one-time code, prints it to its log and requires it to
// create the initial admin account.
package setupcode

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"
)

const (
	// Crockford base32 without ambiguous characters (I, L, O, U).
	alphabet   = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	codeLength = 16
	groupSize  = 4
)

var (
	mu   sync.RWMutex
	code string
)

func Generate() error {
	generated, err := generate()
	if err != nil {
		return err
	}

	mu.Lock()
	code = generated
	mu.Unlock()
	return nil
}

func Clear() {
	mu.Lock()
	code = ""
	mu.Unlock()
}

func Verify(input string) bool {
	mu.RLock()
	expected := code
	mu.RUnlock()

	if expected == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(normalize(input)), []byte(expected)) == 1
}

// Code returns the active code in its display form (groups separated by dashes).
func Code() string {
	mu.RLock()
	defer mu.RUnlock()

	if len(code) <= groupSize {
		return code
	}

	var b strings.Builder
	for i := 0; i < len(code); i += groupSize {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(code[i:min(i+groupSize, len(code))])
	}
	return b.String()
}

func generate() (string, error) {
	buf := make([]byte, codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate setup code: %w", err)
	}

	out := make([]byte, codeLength)
	for i, v := range buf {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out), nil
}

func normalize(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s))
}
