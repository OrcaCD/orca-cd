package httpclient

import (
	"os"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/shared/iplist"
)

var allowedInternal iplist.List

func init() {
	parseAllowedInternalIPs(os.Getenv("ALLOWED_INTERNAL_IPS"))
}

func parseAllowedInternalIPs(raw string) {
	var entries []string
	for entry := range strings.SplitSeq(raw, ",") {
		entries = append(entries, entry)
	}
	allowedInternal = iplist.Parse(entries)
}

func isInternalIPAllowed(ip string) bool {
	return allowedInternal.Contains(ip)
}
