// Package netsafe builds HTTP clients for user-supplied URLs that cannot be
// pointed at the server's own network (SSRF protection).
package netsafe

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlockedAddress is returned when a connection targets a private or local address
var ErrBlockedAddress = errors.New("address not allowed")

// Transport returns an HTTP transport. Unless allowPrivate is set, connections
// to loopback, private, link-local and other non-public addresses are refused
// at dial time, which also covers redirects and DNS rebinding.
func Transport(allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !IsPublicIP(ip) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
			}
			return nil
		}
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		MaxIdleConns:          4,
	}
}

// IsPublicIP reports whether ip is a public unicast address
func IsPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		// carrier-grade NAT 100.64.0.0/10
		(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1]&0xc0 == 64))
}
