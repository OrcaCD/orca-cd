package envrules

import (
	"errors"
	"fmt"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxFileBytes   = 1 << 20
	MaxKeysPerFile = 1000
	MaxValueBytes  = 16 << 10
)

const SavedRedeployFailedPrefix = "the changes were saved, but redeploying the application failed"

// The agent spends at most LockWaitTimeout + WriteTimeout + RedeployTimeout on
// an update, which the hub has to outlast when waiting for the result.
const (
	LockWaitTimeout = 30 * time.Second
	WriteTimeout    = 2 * time.Minute
	RedeployTimeout = 5 * time.Minute
)

// KeyPattern matches the keys Compose accepts: letters, digits and _ . - [ ].
var KeyPattern = regexp.MustCompile(`^[\p{L}\p{N}_.\-\[\]]+$`)

func ValidateKey(key string) error {
	if key == "" {
		return errors.New("must not be empty")
	}
	if !KeyPattern.MatchString(key) {
		return errors.New("may only contain letters, digits and _ . - [ ]")
	}
	return nil
}

func ValidateValue(value string) error {
	if len(value) > MaxValueBytes {
		return fmt.Errorf("is larger than %d bytes", MaxValueBytes)
	}
	if !utf8.ValidString(value) {
		return errors.New("is not valid UTF-8")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return errors.New("contains control characters")
		}
	}
	return nil
}
