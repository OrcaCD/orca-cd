package iplist

import (
	"net/netip"
	"testing"
)

func TestParse_TrimsSpacesAroundEntries(t *testing.T) {
	l := Parse([]string{" 203.0.113.5 ", " 2001:db8::/32 "})

	if !l.Contains("203.0.113.5") {
		t.Error("expected trimmed IPv4 entry to match")
	}
	if !l.Contains("2001:db8::42") {
		t.Error("expected trimmed IPv6 CIDR entry to match")
	}
}

func TestParse_EntryWithEmbeddedCommaIsMalformed(t *testing.T) {
	l := Parse([]string{" 203.0.113.5 , 2001:db8::/32 "})

	if !l.Empty() {
		t.Fatalf("expected empty list, got addrs/prefixes present")
	}
}

func TestParse_UnmapsIPv4MappedIPv6Entry(t *testing.T) {
	l := Parse([]string{"::ffff:192.0.2.10"})

	if !l.Contains("192.0.2.10") {
		t.Error("expected unmapped IPv4 address to match")
	}
}

func TestParse_StripsZoneIdentifier(t *testing.T) {
	l := Parse([]string{"fe80::1%eth0"})

	if !l.Contains("fe80::1") {
		t.Error("expected zone-stripped address to match")
	}
}

func TestParse_MasksIPv6CIDRHostBits(t *testing.T) {
	l := Parse([]string{"2001:db8::5/32"})

	if !l.Contains("2001:db8:1234::1") {
		t.Error("expected address within masked prefix to match")
	}
	if l.Contains("2001:db9::1") {
		t.Error("expected address outside masked prefix to not match")
	}
}

func TestParse_UnmapsIPv4MappedIPv6CIDREntry(t *testing.T) {
	l := Parse([]string{"::ffff:203.0.113.0/120"})

	if !l.Contains("203.0.113.42") {
		t.Error("expected plain IPv4 address to match unmapped CIDR")
	}
	if l.Contains("198.51.100.1") {
		t.Error("expected address outside range to not match")
	}
}

func TestParse_IgnoresMalformedEntriesButKeepsValidOnes(t *testing.T) {
	l := Parse([]string{"not-an-ip", "203.0.113.5", ""})

	if !l.Contains("203.0.113.5") {
		t.Error("expected valid entry to match")
	}
	if l.Contains("203.0.113.9") {
		t.Error("expected non-matching IP to not match")
	}
}

func TestList_EmptyWhenUnconfigured(t *testing.T) {
	l := Parse(nil)

	if !l.Empty() {
		t.Error("expected empty list for nil entries")
	}
	if l.Contains("203.0.113.5") {
		t.Error("expected empty list to match nothing")
	}
}

func TestList_ContainsExactIP(t *testing.T) {
	l := Parse([]string{"203.0.113.5"})

	if !l.Contains("203.0.113.5") {
		t.Error("expected exact match")
	}
	if l.Contains("203.0.113.9") {
		t.Error("expected non-matching IP to not match")
	}
}

func TestList_ContainsCIDRMatch(t *testing.T) {
	l := Parse([]string{"203.0.113.0/24"})

	if !l.Contains("203.0.113.42") {
		t.Error("expected address within CIDR to match")
	}
	if l.Contains("198.51.100.1") {
		t.Error("expected address outside CIDR to not match")
	}
}

func TestList_ContainsIPv6ExactAndCIDR(t *testing.T) {
	l := Parse([]string{"2001:db8::1", "2001:db9::/32"})

	if !l.Contains("2001:db8::1") {
		t.Error("expected exact IPv6 match")
	}
	if !l.Contains("2001:db9::42") {
		t.Error("expected IPv6 within CIDR to match")
	}
	if l.Contains("2001:db8::2") {
		t.Error("expected non-matching IPv6 to not match")
	}
}

func TestParse_UnmapPrefixResult(t *testing.T) {
	l := Parse([]string{"::ffff:203.0.113.0/120"})

	if l.Empty() {
		t.Fatalf("expected non-empty list")
	}
	if len(l.prefixes) != 1 {
		t.Fatalf("expected 1 prefix, got %d", len(l.prefixes))
	}
	if l.prefixes[0] != netip.MustParsePrefix("203.0.113.0/24") {
		t.Errorf("expected 203.0.113.0/24, got %v", l.prefixes[0])
	}
}
