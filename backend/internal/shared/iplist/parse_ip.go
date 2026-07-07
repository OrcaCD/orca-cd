package iplist

import (
	"net/netip"
	"strings"
)

func ParseIP(ip string) (netip.Addr, error) {
	if i := strings.IndexByte(ip, '%'); i != -1 {
		ip = ip[:i]
	}

	result, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}, err
	}

	if result.Is4In6() {
		return result.Unmap(), nil
	}

	return result, nil
}
