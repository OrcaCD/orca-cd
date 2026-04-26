package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDefaultClient(t *testing.T) {
	if Default == nil {
		t.Fatal("Default client is nil")
	}
	if Default.Timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, Default.Timeout)
	}
	if Default.Transport == nil {
		t.Error("expected custom transport, got nil")
	}
	if Default.CheckRedirect == nil {
		t.Error("expected CheckRedirect to be set")
	}
}

func TestDefaultTimeout(t *testing.T) {
	if DefaultTimeout != 15*time.Second {
		t.Errorf("expected 15s, got %v", DefaultTimeout)
	}
}

func TestUserAgent(t *testing.T) {
	ua := UserAgent()
	if !strings.HasPrefix(ua, "OrcaCD/") {
		t.Errorf("expected UserAgent to start with 'OrcaCD/', got %q", ua)
	}
}

func TestNewRequest_SetsUserAgent(t *testing.T) {
	req, err := NewRequest(context.Background(), http.MethodGet, "https://orcacd.dev/", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Header.Get("User-Agent") != UserAgent() {
		t.Errorf("expected User-Agent %q, got %q", UserAgent(), req.Header.Get("User-Agent"))
	}
}

func TestNewRequest_SetsMethod(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		req, err := NewRequest(context.Background(), method, "https://orcacd.dev/", nil)
		if err != nil {
			t.Fatalf("unexpected error for method %s: %v", method, err)
		}
		if req.Method != method {
			t.Errorf("expected method %s, got %s", method, req.Method)
		}
	}
}

func TestNewRequest_UsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := NewRequest(ctx, http.MethodGet, "https://orcacd.dev/", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Context().Err() == nil {
		t.Error("expected cancelled context, got nil error")
	}
}

func TestNewRequest_InvalidURL(t *testing.T) {
	_, err := NewRequest(context.Background(), http.MethodGet, "://invalid", nil)
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestNewRequest_WithBody(t *testing.T) {
	body := strings.NewReader(`{"key":"value"}`)
	req, err := NewRequest(context.Background(), http.MethodPost, "https://orcacd.dev", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Body == nil {
		t.Error("expected non-nil body")
	}
}

func TestGet_BlocksSSRFToLocalhost(t *testing.T) {
	// httptest.NewServer binds to 127.0.0.1, which the SSRF-safe dialer must reject.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := Get(context.Background(), server.URL)
	if err == nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Fatal("expected SSRF error connecting to localhost, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestGet_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := Get(ctx, server.URL)
	if err == nil {
		t.Error("expected error for cancelled context, got nil")
	}

	if resp != nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Error("expected nil response for cancelled context")
	}
}

func TestGet_InvalidURL(t *testing.T) {
	resp, err := Get(context.Background(), "://invalid")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
	if resp != nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Error("expected nil response for invalid URL")
	}
}

func TestCheckRedirect_BlocksPrivateIPLiteral(t *testing.T) {
	privateURLs := []string{
		"http://127.0.0.1/",
		"http://192.168.1.1/secret",
		"http://10.0.0.1/",
		"https://10.0.0.8/",
		"http://[::1]/",
		"http://[fc00::1]/",
		"https://[::ffff:192.168.2.1]/",
	}
	for _, u := range privateURLs {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			t.Fatalf("failed to build request for %s: %v", u, err)
		}
		if err := checkRedirect(req, nil); err == nil {
			t.Errorf("expected SSRF error for redirect to %s, got nil", u)
		}
	}
}

func TestCheckRedirect_AllowsPublicURL(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://orcacd.dev/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRedirect(req, nil); err != nil {
		t.Errorf("expected no error for public URL redirect, got: %v", err)
	}
}

func TestGet_BlocksLocalhostHostname(t *testing.T) {
	resp, err := Get(context.Background(), "http://localhost/")
	if err == nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Fatal("expected SSRF error connecting to localhost hostname, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestGet_BlocksNipIODNSRebinding(t *testing.T) {
	resp, err := Get(context.Background(), "http://www.192.168.0.1.nip.io/")
	if err == nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Fatal("expected SSRF error for nip.io hostname resolving to private IP, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Skipf("local DNS resolver blocked nip.io before SSRF check could run: %v", err)
	}
}

func TestGet_BlocksSSRFViaRedirect(t *testing.T) {
	resp, err := Get(context.Background(), "http://ssrf-redirects.testssandbox.com/ssrf-test")
	if err == nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Fatal("expected SSRF error for redirect to 127.0.0.1, got nil")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestGet_BlocksSSRFViaIPv6RedirectChain(t *testing.T) {
	resp, err := Get(context.Background(), "http://ssrf-redirects.testssandbox.com/ssrf-test-ipv6-twice")
	if err == nil {
		err := resp.Body.Close()
		if err != nil {
			t.Errorf("failed to close response body: %v", err)
		}
		t.Fatal("expected SSRF error for redirect chain ending at [::1], got nil")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("expected SSRF error, got: %v", err)
	}
}

func TestParseAllowedInternalIPs_SingleIP(t *testing.T) {
	parseAllowedInternalIPs("192.168.1.5")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	if !isInternalIPAllowed("192.168.1.5") {
		t.Error("expected 192.168.1.5 to be allowed")
	}
	if isInternalIPAllowed("192.168.1.6") {
		t.Error("expected 192.168.1.6 to be blocked")
	}
}

func TestParseAllowedInternalIPs_CIDR(t *testing.T) {
	parseAllowedInternalIPs("10.0.0.0/8")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	if !isInternalIPAllowed("10.1.2.3") {
		t.Error("expected 10.1.2.3 to be allowed by 10.0.0.0/8")
	}
	if isInternalIPAllowed("192.168.1.1") {
		t.Error("expected 192.168.1.1 to be blocked (not in allowed CIDR)")
	}
}

func TestParseAllowedInternalIPs_MultipleEntries(t *testing.T) {
	parseAllowedInternalIPs("127.0.0.1, 10.0.0.0/8, ::1")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	for _, ip := range []string{"127.0.0.1", "10.5.5.5", "::1"} {
		if !isInternalIPAllowed(ip) {
			t.Errorf("expected %s to be allowed", ip)
		}
	}
	if isInternalIPAllowed("192.168.1.1") {
		t.Error("expected 192.168.1.1 to be blocked")
	}
}

func TestParseAllowedInternalIPs_Empty(t *testing.T) {
	parseAllowedInternalIPs("")
	if isInternalIPAllowed("127.0.0.1") {
		t.Error("expected 127.0.0.1 to be blocked when allow-list is empty")
	}
}

func TestParseAllowedInternalIPs_InvalidEntries(t *testing.T) {
	parseAllowedInternalIPs("notanip, , 300.300.300.300")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	if len(allowedInternalAddrs) != 0 || len(allowedInternalPrefixes) != 0 {
		t.Error("expected empty allow-list after all-invalid input")
	}
}

func TestGet_AllowsAllowlistedLocalhostIP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parseAllowedInternalIPs("127.0.0.1")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	resp, err := Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("expected successful response for allow-listed IP, got: %v", err)
	}
	err = resp.Body.Close()
	if err != nil {
		t.Errorf("failed to close response body: %v", err)
	}
}

func TestCheckRedirect_AllowsAllowlistedPrivateIP(t *testing.T) {
	parseAllowedInternalIPs("192.168.1.1")
	t.Cleanup(func() { parseAllowedInternalIPs("") })

	req, err := http.NewRequest(http.MethodGet, "http://192.168.1.1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRedirect(req, nil); err != nil {
		t.Errorf("expected allow-listed IP to pass redirect check, got: %v", err)
	}
}

func TestCheckRedirect_LimitsRedirects(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://orcacd.dev/", nil)
	if err != nil {
		t.Fatal(err)
	}
	via := make([]*http.Request, 10)
	if err := checkRedirect(req, via); err == nil {
		t.Error("expected error after 10 redirects, got nil")
	}
}
