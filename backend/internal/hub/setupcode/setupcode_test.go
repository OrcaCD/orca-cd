package setupcode

import (
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	t.Cleanup(Clear)

	if err := Generate(); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	displayed := Code()
	if len(displayed) != codeLength+codeLength/groupSize-1 {
		t.Errorf("Code() = %q, unexpected length %d", displayed, len(displayed))
	}
	for _, r := range strings.ReplaceAll(displayed, "-", "") {
		if !strings.ContainsRune(alphabet, r) {
			t.Errorf("Code() = %q contains %q outside the alphabet", displayed, r)
		}
	}
	if !Verify(displayed) {
		t.Error("Verify() rejected the displayed code")
	}
}

func TestGenerate_DifferentCodes(t *testing.T) {
	t.Cleanup(Clear)

	_ = Generate()
	first := Code()
	_ = Generate()
	if first == Code() {
		t.Error("expected a fresh code on every Generate()")
	}
}

func TestVerify_NormalizesInput(t *testing.T) {
	t.Cleanup(Clear)

	if err := Generate(); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	displayed := Code()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"exact", displayed, true},
		{"without dashes", strings.ReplaceAll(displayed, "-", ""), true},
		{"lowercase", strings.ToLower(displayed), true},
		{"surrounding whitespace", "  " + strings.ReplaceAll(displayed, "-", " ") + "\n", true},
		{"wrong last character", displayed[:len(displayed)-1] + "!", false},
		{"prefix", displayed[:9], false},
		{"too long", displayed + "-AAAA", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Verify(tt.input); got != tt.want {
				t.Errorf("Verify(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestVerify_FailsClosedWithoutCode(t *testing.T) {
	Clear()

	if Verify("") {
		t.Error("Verify(\"\") must not succeed without an active code")
	}
	if Verify("ABCD-EFGH-JKMN-PQRS") {
		t.Error("Verify() must not succeed without an active code")
	}
}

func TestClear(t *testing.T) {
	if err := Generate(); err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	displayed := Code()

	Clear()

	if Verify(displayed) {
		t.Error("expected Verify() to fail after Clear()")
	}
	if Code() != "" {
		t.Error("expected Code() to be empty after Clear()")
	}
}
