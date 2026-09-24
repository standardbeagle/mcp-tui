package oauth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// errNonPublicAddress marks an auth request refused because its dial target
// is not a public address.
var errNonPublicAddress = errors.New("non-public address")

// cgnat is the RFC 6598 carrier-grade NAT range, which netip's IsPrivate
// does not report.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// nonPublicClass names the range ip belongs to when an auth request must not
// dial it, or returns "" for a public or loopback address. The ranges are
// the SDK's (go-sdk internal/util.IsPrivateOrReserved), which oauthex
// enforces at dial time for discovery; loopback stays reachable so a local
// authorization server works.
func nonPublicClass(ip netip.Addr) string {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid():
		return classInvalid
	case ip.IsPrivate():
		return classPrivate
	case ip.IsLinkLocalUnicast():
		return classLinkLocal
	case ip.IsMulticast():
		return classMulticast
	case ip.IsUnspecified():
		return classUnspecified
	case cgnat.Contains(ip):
		return classCGNAT
	}
	return ""
}

// Address classes nonPublicClass reports, as logged and in refusals.
const (
	classInvalid     = "invalid"
	classPrivate     = "private"
	classLinkLocal   = "link_local"
	classMulticast   = "multicast"
	classUnspecified = "unspecified"
	classCGNAT       = "cgnat"
)

// guardedTransport returns rt with every dial checked by nonPublicClass,
// the check the SDK's discovery client makes itself only when handed a bare
// *http.Transport — which the tracing wrapper never is. Discovered endpoints
// come from documents the server controls, so without this a hostname that
// resolves into a private range would be dialed. The check runs on the
// resolved address (net.Dialer.Control), so DNS cannot swap it afterwards.
//
// As in the SDK, a transport that dials for itself (DialContext or
// DialTLSContext set) or sends everything through a proxy (whose address is
// all the dialer sees) is used unguarded; both are logged.
func guardedTransport(rt http.RoundTripper, allowPrivateNetwork bool) http.RoundTripper {
	// A nil transport means http.DefaultTransport, whose DialContext is only
	// the stock dialer, so it is guarded like a bare transport.
	callerDials := rt != nil
	if rt == nil {
		rt = http.DefaultTransport
	}
	t, ok := rt.(*http.Transport)
	if !ok || (callerDials && (t.DialContext != nil || t.DialTLSContext != nil)) {
		authLog().Warn("Auth dial guard not applied: the HTTP client's transport does its own dialing")
		return rt
	}
	base := t.Clone()
	if proxyConfigured(base) {
		authLog().Warn("Auth dial guard not applied: an HTTP proxy is configured, so every dial goes to the proxy")
		return rt
	}
	guard := dialGuard{allowPrivateNetwork: allowPrivateNetwork}
	base.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   guard.control,
	}).DialContext
	return base
}

// proxyConfigured reports whether t sends requests to a public URL through
// a proxy (the SDK's test for the same exemption).
func proxyConfigured(t *http.Transport) bool {
	if t.Proxy == nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "https://check-proxy-existence.com", http.NoBody)
	if err != nil {
		return false
	}
	u, err := t.Proxy(req)
	return err == nil && u != nil
}

type dialGuard struct {
	allowPrivateNetwork bool
}

// control runs after name resolution, on the address about to be dialed.
func (g dialGuard) control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("oauth: auth dial target %q is not an IP address: %w", host, err)
	}
	class := nonPublicClass(ip)
	if class == "" {
		return nil
	}
	if !g.allowPrivateNetwork {
		return fmt.Errorf("oauth: refusing auth request to %s (%s): %w; pass --oauth-allow-private-network to allow it",
			ip, class, errNonPublicAddress)
	}
	authLog().Warn("Auth request to non-public address allowed (--oauth-allow-private-network)",
		debug.F("address", ip.String()), debug.F("class", class))
	return nil
}
