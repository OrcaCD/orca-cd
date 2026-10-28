package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
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

var ErrDisabled = errors.New("outbound requests are disabled in demo mode")

var disabled atomic.Bool

func Disable() {
	disabled.Store(true)
}

func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if disabled.Load() {
		return nil, ErrDisabled
	}
	return dialer.DialContext(ctx, network, address)
}
