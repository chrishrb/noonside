// Package netguard keeps server-side requests to user-supplied hosts (data
// sources, webhooks, ntfy) away from the server's own network (SSRF).
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/chrishrb/noonside/internal/platform/errtrack"
)

// cgnat is the shared address space of carrier-grade NAT (RFC 6598).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Dialer returns a dialer for user-supplied hosts. Unless allowPrivate is
// set, it refuses loopback, private, link-local (incl. cloud metadata),
// multicast and unspecified addresses. The check runs on the resolved
// address, so DNS names pointing to such addresses are refused too.
func Dialer(timeout time.Duration, allowPrivate bool) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if !allowPrivate {
		d.Control = func(_, address string, _ syscall.RawConn) error {
			return CheckPublic(address)
		}
	}
	return d
}

// CheckPublic refuses an ip:port that is not a public address.
func CheckPublic(address string) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", address, err)
	}
	ip := ap.Addr().Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip) {
		return fmt.Errorf("connections to %s are not allowed (private or local address)", ip)
	}
	return nil
}

// HTTPClient returns a client whose connections (including redirects) go
// through Dialer. It ignores proxy settings, which would bypass the check.
func HTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = Dialer(timeout, allowPrivate).DialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: errtrack.Transport(t),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return CheckURL(req.URL.String(), allowPrivate)
		},
	}
}

// CheckURL accepts an absolute https URL with a host and no credentials; with
// allowPrivate (self-hosted setups) http is fine too. The address itself is
// checked when connecting.
func CheckURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if u.Scheme != "https" && (!allowPrivate || u.Scheme != "http") {
		return fmt.Errorf("URL must start with https://")
	}
	if u.Hostname() == "" {
		return fmt.Errorf("URL needs a host")
	}
	if u.User != nil {
		return fmt.Errorf("URL must not contain credentials")
	}
	return nil
}
