package httpclient

import (
	"net/netip"
	"os"
	"slices"
	"strings"
)

var allowedInternalAddrs []netip.Addr
var allowedInternalPrefixes []netip.Prefix

func init() {
	parseAllowedInternalIPs(os.Getenv("ALLOWED_INTERNAL_IPS"))
}

func parseAllowedInternalIPs(raw string) {
	allowedInternalAddrs = nil
	allowedInternalPrefixes = nil
	if raw == "" {
		return
	}
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err == nil {
				allowedInternalPrefixes = append(allowedInternalPrefixes, prefix.Masked())
			}
		} else {
			addr, err := ParseIP(entry)
			if err == nil {
				allowedInternalAddrs = append(allowedInternalAddrs, addr)
			}
		}
	}
}

func isInternalIPAllowed(ip string) bool {
	if len(allowedInternalAddrs) == 0 && len(allowedInternalPrefixes) == 0 {
		return false
	}

	addr, err := ParseIP(ip)
	if err != nil {
		return false
	}

	if slices.Contains(allowedInternalAddrs, addr) {
		return true
	}

	for _, p := range allowedInternalPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
