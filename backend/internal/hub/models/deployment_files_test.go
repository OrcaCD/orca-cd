package models

import (
	"bytes"
	"testing"
)

func TestDeploymentFilesRoundTrip(t *testing.T) {
	const path = "config/raw.bin"
	content := []byte{0, 0xff, 0x80}
	encoded, err := EncodeDeploymentFiles(DeploymentFiles{path: content})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDeploymentFiles(encoded)
	if err != nil || !bytes.Equal(decoded[path], content) {
		t.Fatalf("decoded files = %v, error = %v", decoded, err)
	}
	if _, err := DecodeDeploymentFiles("invalid"); err == nil {
		t.Fatal("invalid stored data was accepted")
	}
}
