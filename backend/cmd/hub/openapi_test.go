package main

import (
	"bytes"
	"os"
	"testing"
)

// The docs render openapi.json from main, so it has to match the routes.
func TestOpenAPIFileIsUpToDate(t *testing.T) {
	var spec bytes.Buffer
	if err := writeOpenAPI(&spec); err != nil {
		t.Fatalf("writeOpenAPI() error = %v", err)
	}

	committed, err := os.ReadFile("../../openapi.json")
	if err != nil {
		t.Fatalf("failed to read openapi.json: %v", err)
	}
	if !bytes.Equal(spec.Bytes(), committed) {
		t.Fatal("openapi.json is out of date, run `just openapi` in backend/")
	}
}

func TestOpenAPICmd_PrintsTheSpec(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"openapi"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("openapi command error = %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"openapi": "3.1.0"`)) || !bytes.Contains(out.Bytes(), []byte(`"/applications/{id}/deploy"`)) {
		t.Fatalf("output is not the spec: %.200s", out.String())
	}
}
