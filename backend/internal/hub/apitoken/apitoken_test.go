package apitoken

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	token := Generate()

	if !strings.HasPrefix(token.Plain, Prefix) {
		t.Errorf("expected token to start with %q, got %q", Prefix, token.Plain)
	}
	if !IsWellFormed(token.Plain) {
		t.Errorf("expected generated token %q to be well-formed", token.Plain)
	}
	if !strings.HasPrefix(token.Plain, token.Prefix) {
		t.Errorf("expected prefix %q to be the start of the token", token.Prefix)
	}
	if len(token.Prefix) != len(Prefix)+8 {
		t.Errorf("expected prefix to be %d characters, got %d", len(Prefix)+8, len(token.Prefix))
	}
	if token.Hash != Hash(token.Plain) {
		t.Error("expected hash to be the hash of the plain token")
	}
	if strings.Contains(token.Hash, token.Plain) {
		t.Error("hash must not contain the plain token")
	}
}

func TestGenerate_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		token := Generate()
		if seen[token.Plain] {
			t.Fatalf("generated the same token twice: %q", token.Plain)
		}
		seen[token.Plain] = true
	}
}

func TestHash(t *testing.T) {
	const want = "9d55affbb61b5b31bb8a2fe826662b611ca20980fb2d0fbbc6e90d7fb6bf543238081e8e5d7a426848647b49aaa14fb36e751495b7e1a3012b95b32f6231d2e2" // sha512("orca_test")

	if got := Hash("orca_test"); got != want {
		t.Errorf("Hash() = %q, want %q", got, want)
	}
	if Hash("orca_test") == Hash("orca_tesu") {
		t.Error("expected different tokens to have different hashes")
	}
}

func TestIsWellFormed(t *testing.T) {
	valid := Generate().Plain

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"generated token", valid, true},
		{"empty", "", false},
		{"prefix only", Prefix, false},
		{"missing prefix", strings.TrimPrefix(valid, Prefix), false},
		{"wrong prefix", "orcb_" + strings.TrimPrefix(valid, Prefix), false},
		{"too short", valid[:len(valid)-1], false},
		{"too long", valid + "A", false},
		{"invalid base64", Prefix + strings.Repeat("!", secretLength), false},
		{"jwt", "header.payload.signature", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWellFormed(tt.in); got != tt.want {
				t.Errorf("IsWellFormed(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
