package httpclient

import (
	"fmt"
	"net"
	"syscall"
	"time"
)

var dialer = &net.Dialer{
	Timeout:   30 * time.Second,
	KeepAlive: 30 * time.Second,
	Control: func(network, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if host != "" && IsPrivateIP(host) && !isInternalIPAllowed(host) {
			return fmt.Errorf("SSRF detected: connection to %s is prohibited", host)
		}
		return nil
	},
}
