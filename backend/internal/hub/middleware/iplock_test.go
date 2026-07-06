package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newIPLockRouter(allowedIPs []string) *gin.Engine {
	router := gin.New()
	router.Use(IPLock(allowedIPs))
	router.GET("/api/v1/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/api/v1/ws", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.POST("/api/v1/webhooks/repositories/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.POST("/api/v1/github-actions", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return router
}

func doRequest(router *gin.Engine, method, path, remoteAddr string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	router.ServeHTTP(w, req)
	return w
}

func TestIPLock_NoopWhenUnconfigured(t *testing.T) {
	router := newIPLockRouter(nil)

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_AllowsExactIP(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.5"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_BlocksNonMatchingIP(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.5"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.9:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_AllowsCIDRMatch(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.0/24"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.42:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_BlocksOutsideCIDR(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.0/24"})

	w := doRequest(router, http.MethodGet, "/", "198.51.100.1:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_IgnoresMalformedEntriesButKeepsValidOnes(t *testing.T) {
	router := newIPLockRouter([]string{"not-an-ip", "203.0.113.5", ""})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "203.0.113.9:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_TrimsSpacesAroundEntries(t *testing.T) {
	router := newIPLockRouter([]string{" 203.0.113.5 ", " 2001:db8::/32 "})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for trimmed IPv4 entry, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "[2001:db8::42]:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for trimmed IPv6 CIDR entry, got %d", w.Code)
	}
}

func TestParseIPAllowlist_EntryWithEmbeddedCommaIsMalformed(t *testing.T) {
	addrs, prefixes := parseIPAllowlist([]string{" 203.0.113.5 , 2001:db8::/32 "})

	if len(addrs) != 0 || len(prefixes) != 0 {
		t.Fatalf("expected addrs=nil prefixes=nil, got addrs=%v prefixes=%v", addrs, prefixes)
	}
}

func TestIPLock_AllowsExactIPv6(t *testing.T) {
	router := newIPLockRouter([]string{"2001:db8::1"})

	w := doRequest(router, http.MethodGet, "/", "[2001:db8::1]:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_BlocksNonMatchingIPv6(t *testing.T) {
	router := newIPLockRouter([]string{"2001:db8::1"})

	w := doRequest(router, http.MethodGet, "/", "[2001:db8::2]:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_AllowsIPv6CIDRMatch(t *testing.T) {
	router := newIPLockRouter([]string{"2001:db8::/32"})

	w := doRequest(router, http.MethodGet, "/", "[2001:db8:1234::5]:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_BlocksOutsideIPv6CIDR(t *testing.T) {
	router := newIPLockRouter([]string{"2001:db8::/32"})

	w := doRequest(router, http.MethodGet, "/", "[2001:db9::1]:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_AllowsIPv4MappedIPv6AllowlistEntry(t *testing.T) {
	router := newIPLockRouter([]string{"::ffff:203.0.113.5"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_AllowsIPv4MappedIPv6RemoteAddr(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.5"})

	w := doRequest(router, http.MethodGet, "/", "[::ffff:203.0.113.5]:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestIPLock_AllowsIPv4MappedIPv6CIDREntry(t *testing.T) {
	router := newIPLockRouter([]string{"::ffff:203.0.113.0/120"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.42:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "198.51.100.1:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestIPLock_MixedIPv4AndIPv6List(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.5", "2001:db8::/32"})

	w := doRequest(router, http.MethodGet, "/", "203.0.113.5:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for IPv4 entry, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "[2001:db8::42]:1234")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for IPv6 entry, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "198.51.100.1:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-matching IPv4, got %d", w.Code)
	}

	w = doRequest(router, http.MethodGet, "/", "[2001:db9::1]:1234")
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-matching IPv6, got %d", w.Code)
	}
}

func TestParseIPAllowlist_UnmapsIPv4MappedIPv6Entry(t *testing.T) {
	addrs, prefixes := parseIPAllowlist([]string{"::ffff:192.0.2.10"})

	if len(prefixes) != 0 {
		t.Fatalf("expected no prefixes, got %d", len(prefixes))
	}
	if len(addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(addrs))
	}
	if !addrs[0].Is4() {
		t.Errorf("expected unmapped 4-byte address, got %v", addrs[0])
	}
	if addrs[0] != netip.MustParseAddr("192.0.2.10") {
		t.Errorf("expected 192.0.2.10, got %v", addrs[0])
	}
}

func TestParseIPAllowlist_StripsZoneIdentifier(t *testing.T) {
	addrs, _ := parseIPAllowlist([]string{"fe80::1%eth0"})

	if len(addrs) != 1 {
		t.Fatalf("expected 1 address, got %d", len(addrs))
	}
	if addrs[0] != netip.MustParseAddr("fe80::1") {
		t.Errorf("expected fe80::1, got %v", addrs[0])
	}
}

func TestParseIPAllowlist_UnmapsIPv4MappedIPv6CIDREntry(t *testing.T) {
	addrs, prefixes := parseIPAllowlist([]string{"::ffff:203.0.113.0/120"})

	if len(addrs) != 0 {
		t.Fatalf("expected no addresses, got %d", len(addrs))
	}
	if len(prefixes) != 1 {
		t.Fatalf("expected 1 prefix, got %d", len(prefixes))
	}
	if !prefixes[0].Addr().Is4() {
		t.Errorf("expected unmapped 4-byte prefix address, got %v", prefixes[0])
	}
	if prefixes[0] != netip.MustParsePrefix("203.0.113.0/24") {
		t.Errorf("expected 203.0.113.0/24, got %v", prefixes[0])
	}
}

func TestParseIPAllowlist_MasksIPv6CIDRHostBits(t *testing.T) {
	addrs, prefixes := parseIPAllowlist([]string{"2001:db8::5/32"})

	if len(addrs) != 0 {
		t.Fatalf("expected no addresses, got %d", len(addrs))
	}
	if len(prefixes) != 1 {
		t.Fatalf("expected 1 prefix, got %d", len(prefixes))
	}
	if prefixes[0] != netip.MustParsePrefix("2001:db8::/32") {
		t.Errorf("expected masked prefix 2001:db8::/32, got %v", prefixes[0])
	}
}

func TestIPLock_ExemptPathsAlwaysReachable(t *testing.T) {
	router := newIPLockRouter([]string{"203.0.113.5"})

	nonAllowedIP := "198.51.100.1:1234"

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/health"},
		{http.MethodGet, "/api/v1/ws"},
		{http.MethodPost, "/api/v1/webhooks/repositories/123"},
		{http.MethodPost, "/api/v1/github-actions"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := doRequest(router, tc.method, tc.path, nonAllowedIP)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200 for exempt path %s, got %d", tc.path, w.Code)
			}
		})
	}
}
