package composewarnings

import "testing"

func TestHash(t *testing.T) {
	if got, want := Hash("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("Hash(%q) = %q, want %q", "abc", got, want)
	}
}
