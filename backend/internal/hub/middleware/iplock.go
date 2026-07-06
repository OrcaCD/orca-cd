package middleware

import (
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/httpclient"
	"github.com/gin-gonic/gin"
)

var ipLockExemptPrefixes = []string{
	"/api/v1/webhooks/",
	"/api/v1/github-actions",
}

var ipLockExemptPaths = map[string]bool{
	"/api/v1/health": true,
	"/api/v1/ws":     true,
}

// IPLock returns a middleware that rejects requests from clients whose IP is
// not in allowedIPs (single addresses or CIDR ranges), except for exempt
// paths (health check, webhooks, agent WebSocket). If allowedIPs contains no
// valid entries, the middleware is a no-op.
func IPLock(allowedIPs []string) gin.HandlerFunc {
	addrs, prefixes := parseIPAllowlist(allowedIPs)
	if len(addrs) == 0 && len(prefixes) == 0 {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		if isIPLockExempt(c.Request.URL.Path) {
			c.Next()
			return
		}

		addr, err := httpclient.ParseIP(c.ClientIP())
		if err != nil || !ipAllowed(addr, addrs, prefixes) {
			c.String(http.StatusForbidden, "403 forbidden: ip not allowed")
			c.Abort()
			return
		}

		c.Next()
	}
}

func isIPLockExempt(path string) bool {
	if ipLockExemptPaths[path] {
		return true
	}
	for _, prefix := range ipLockExemptPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func parseIPAllowlist(entries []string) ([]netip.Addr, []netip.Prefix) {
	var addrs []netip.Addr
	var prefixes []netip.Prefix
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if prefix, err := netip.ParsePrefix(entry); err == nil {
				prefixes = append(prefixes, unmapPrefix(prefix).Masked())
			}
			continue
		}
		if addr, err := httpclient.ParseIP(entry); err == nil {
			addrs = append(addrs, addr)
		}
	}
	return addrs, prefixes
}

// unmapPrefix converts a CIDR written in IPv4-mapped-IPv6 form (e.g.
// "::ffff:203.0.113.0/120") to a plain IPv4 prefix, since netip.Prefix.Contains
// requires the address family of the prefix and the checked address to match,
// and client IPs are always unmapped to plain IPv4 before comparison.
func unmapPrefix(p netip.Prefix) netip.Prefix {
	addr := p.Addr()
	if !addr.Is4In6() || p.Bits() < 96 {
		return p
	}
	return netip.PrefixFrom(addr.Unmap(), p.Bits()-96)
}

func ipAllowed(addr netip.Addr, addrs []netip.Addr, prefixes []netip.Prefix) bool {
	if slices.Contains(addrs, addr) {
		return true
	}
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
