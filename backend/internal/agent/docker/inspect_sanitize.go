package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const redactedValue = "[redacted]"

// Labels that carry credentials by design.
var secretLabelSuffixes = []string{
	"basicauth.users",
	"digestauth.users",
}

// sanitizeInspect removes the environment and redacts known credential
// labels. All other fields pass through unchanged.
func sanitizeInspect(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("decode container inspect: %w", err)
	}

	if config, _ := doc["Config"].(map[string]any); config != nil {
		delete(config, "Env")
		redactLabels(config)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("encode container inspect: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func redactLabels(config map[string]any) {
	labels, _ := config["Labels"].(map[string]any)
	for key := range labels {
		if isSecretLabelKey(key) {
			labels[key] = redactedValue
		}
	}
}

func isSecretLabelKey(key string) bool {
	lower := strings.ToLower(key)
	return slices.ContainsFunc(secretLabelSuffixes, func(suffix string) bool {
		return strings.HasSuffix(lower, "."+suffix)
	})
}
