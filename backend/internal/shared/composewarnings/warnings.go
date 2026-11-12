package composewarnings

import (
	"crypto/sha256"
	"encoding/hex"
)

type Warning struct {
	Code    string `json:"code"`
	Service string `json:"service,omitempty"`
	Field   string `json:"field,omitempty"`
}

func Hash(composeFile string) string {
	sum := sha256.Sum256([]byte(composeFile))
	return hex.EncodeToString(sum[:])
}
