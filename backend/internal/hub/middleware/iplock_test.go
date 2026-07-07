package middleware

import (
	"net/http"
	"net/http/httptest"
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
