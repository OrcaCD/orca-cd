package agenttoken

import (
	"crypto/ed25519"
	"testing"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	const jwt = "header.payload.signature"

	token, err := Encode(jwt, priv.Seed())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	gotJWT, gotKey, err := Decode(token)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if gotJWT != jwt {
		t.Errorf("gotJWT = %q, want %q", gotJWT, jwt)
	}
	if !gotKey.Public().(ed25519.PublicKey).Equal(pub) {
		t.Error("decoded signing key does not match original public key")
	}
}

func TestEncode_InvalidSeedLength(t *testing.T) {
	_, err := Encode("header.payload.signature", make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for invalid seed length")
	}
}

func TestDecode_MissingSegment(t *testing.T) {
	_, _, err := Decode("header.payload.signature")
	if err == nil {
		t.Fatal("expected error for token missing signing-key segment")
	}
}

func TestDecode_InvalidBase64Segment(t *testing.T) {
	_, _, err := Decode("header.payload.signature.not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64 signing-key segment")
	}
}

func TestDecode_WrongSeedLength(t *testing.T) {
	// valid base64url but wrong decoded length
	_, _, err := Decode("header.payload.signature.YWJj")
	if err == nil {
		t.Fatal("expected error for wrong seed length")
	}
}
