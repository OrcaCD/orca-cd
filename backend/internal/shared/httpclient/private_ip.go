package httpclient

import (
	"net/netip"

	"github.com/OrcaCD/orca-cd/internal/shared/iplist"
)

var privateIPRangesStr = []string{
	"0.0.0.0/8",          // "This" network (RFC 1122)
	"10.0.0.0/8",         // Private-Use Networks (RFC 1918)
	"127.0.0.0/8",        // Loopback (RFC 1122)
	"192.168.0.0/16",     // Private-Use Networks (RFC 1918)
	"172.16.0.0/12",      // Private-Use Networks (RFC 1918)
	"100.64.0.0/10",      // Shared Address Space (RFC 6598)
	"169.254.0.0/16",     // Link Local (RFC 3927)
	"192.0.0.0/24",       // IETF Protocol Assignments (RFC 5736)
	"192.0.2.0/24",       // TEST-NET-1 (RFC 5737)
	"192.31.196.0/24",    // AS112 Redirection Anycast (RFC 7535)
	"192.52.193.0/24",    // Automatic Multicast Tunneling (RFC 7450)
	"192.88.99.0/24",     // 6to4 Relay Anycast (RFC 3068)
	"192.175.48.0/24",    // AS112 Redirection Anycast (RFC 7535)
	"198.18.0.0/15",      // Network Interconnect Device Benchmark Testing (RFC 2544)
	"198.51.100.0/24",    // TEST-NET-2 (RFC 5737)
	"203.0.113.0/24",     // TEST-NET-3 (RFC 5737)
	"224.0.0.0/4",        // Multicast (RFC 3171)
	"240.0.0.0/4",        // Reserved for Future Use (RFC 1112)
	"255.255.255.255/32", // Limited Broadcast (RFC 919)

	"::/128",        // Unspecified address (RFC 4291)
	"::1/128",       // Loopback address (RFC 4291)
	"fc00::/7",      // Unique local address (ULA) (RFC 4193)
	"fe80::/10",     // Link-local address (LLA) (RFC 4291)
	"100::/64",      // Discard prefix (RFC 6666)
	"2001:db8::/32", // Documentation prefix (RFC 3849)
	"3fff::/20",     // Documentation prefix (RFC 9637)
	// Local-use NAT64 prefix (RFC 8215). The gateway picks the prefix length,
	// which decides where the IPv4 address is embedded (RFC 6052), so the
	// embedded address cannot be checked reliably.
	"64:ff9b:1::/48",
}

var privateIPRanges []netip.Prefix

// IPv6 prefixes that embed an IPv4 address, which a NAT64 gateway or 6to4
// relay forwards to. The embedded address is checked instead of blocking the
// whole prefix, so public IPv4 hosts stay reachable on IPv6-only networks.
var (
	nat64Prefix     = netip.MustParsePrefix("64:ff9b::/96") // Well-known NAT64 prefix (RFC 6052)
	sixToFourPrefix = netip.MustParsePrefix("2002::/16")    // 6to4 (RFC 3056)
)

func init() {
	for _, cidr := range privateIPRangesStr {
		prefix := netip.MustParsePrefix(cidr)
		privateIPRanges = append(privateIPRanges, prefix)
	}
}

func IsPrivateIP(ip string) bool {
	if ip == "" {
		return false
	}

	ipAddress, err := iplist.ParseIP(ip)
	if err != nil {
		return false
	}

	if embedded, ok := embeddedIPv4(ipAddress); ok {
		ipAddress = embedded
	}

	for _, p := range privateIPRanges {
		if p.Contains(ipAddress) {
			return true
		}
	}

	return false
}

// embeddedIPv4 returns the IPv4 address embedded in a NAT64 or 6to4 address.
func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	bytes := ip.As16()
	if nat64Prefix.Contains(ip) {
		return netip.AddrFrom4([4]byte(bytes[12:16])), true
	}
	if sixToFourPrefix.Contains(ip) {
		return netip.AddrFrom4([4]byte(bytes[2:6])), true
	}
	return netip.Addr{}, false
}
