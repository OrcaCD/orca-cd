package iplist

import (
	"net/netip"
	"slices"
	"strings"
)

// List is a parsed set of single IPs and CIDR ranges to match against.
type List struct {
	addrs    []netip.Addr
	prefixes []netip.Prefix
}

// Parse builds a List from entries (single IPs or CIDR ranges). Each entry is
// trimmed of surrounding whitespace; empty and malformed entries are skipped.
func Parse(entries []string) List {
	var l List
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if prefix, err := netip.ParsePrefix(entry); err == nil {
				l.prefixes = append(l.prefixes, unmapPrefix(prefix).Masked())
			}
			continue
		}
		if addr, err := ParseIP(entry); err == nil {
			l.addrs = append(l.addrs, addr)
		}
	}
	return l
}

// Empty reports whether the list has no valid entries.
func (l List) Empty() bool {
	return len(l.addrs) == 0 && len(l.prefixes) == 0
}

// Contains reports whether ip matches an entry in the list.
func (l List) Contains(ip string) bool {
	if l.Empty() {
		return false
	}
	addr, err := ParseIP(ip)
	if err != nil {
		return false
	}
	if slices.Contains(l.addrs, addr) {
		return true
	}
	for _, p := range l.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// unmapPrefix converts a CIDR written in IPv4-mapped-IPv6 form (e.g.
// "::ffff:203.0.113.0/120") to a plain IPv4 prefix, since netip.Prefix.Contains
// requires the address family of the prefix and the checked address to match,
// and addresses passed to Contains are always unmapped to plain IPv4 first.
func unmapPrefix(p netip.Prefix) netip.Prefix {
	addr := p.Addr()
	if !addr.Is4In6() || p.Bits() < 96 {
		return p
	}
	return netip.PrefixFrom(addr.Unmap(), p.Bits()-96)
}
