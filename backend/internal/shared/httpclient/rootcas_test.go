package httpclient

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func resetTLSClientConfig(t *testing.T) {
	t.Cleanup(func() { safeTransport.TLSClientConfig = nil })
}

func TestTrustExtraCACerts_TrustsCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parseAllowedInternalIPs("127.0.0.1")
	t.Cleanup(func() { parseAllowedInternalIPs("") })
	resetTLSClientConfig(t)

	if resp, err := Get(context.Background(), server.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected the self-signed certificate to be rejected before trusting it")
	}

	path := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrustExtraCACerts(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	safeTransport.CloseIdleConnections()

	resp, err := Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("expected the trusted certificate to be accepted, got: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("failed to close response body: %v", err)
	}
}

func TestTrustExtraCACerts_MissingFile(t *testing.T) {
	resetTLSClientConfig(t)

	if err := TrustExtraCACerts(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("expected an error for a missing file")
	}
	if safeTransport.TLSClientConfig != nil {
		t.Error("expected the TLS config to stay unchanged")
	}
}

func TestTrustExtraCACerts_NoCertificates(t *testing.T) {
	resetTLSClientConfig(t)

	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TrustExtraCACerts(path); err == nil {
		t.Error("expected an error for a file without certificates")
	}
	if safeTransport.TLSClientConfig != nil {
		t.Error("expected the TLS config to stay unchanged")
	}
}
