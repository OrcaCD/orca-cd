package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TrustExtraCACerts trusts the PEM encoded certificates in path in addition to the system roots.
// Unlike SSL_CERT_FILE it keeps the system roots and also works on macOS, where Go verifies
// against the keychain. It must be called before the first request.
func TrustExtraCACerts(path string) error {
	pemData, err := os.ReadFile(path) //nolint:gosec // path is operator-provided configuration
	if err != nil {
		return fmt.Errorf("failed to read extra CA certificates: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		return fmt.Errorf("failed to load system CA certificates: %w", err)
	}
	if !pool.AppendCertsFromPEM(pemData) {
		return fmt.Errorf("no PEM certificates found in %s", path)
	}

	safeTransport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return nil
}
