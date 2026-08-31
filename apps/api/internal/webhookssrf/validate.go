// Package webhookssrf validates outbound webhook target URLs against SSRF
// (server-side request forgery). It is called at two points (nester#836):
// once at subscription registration, and again immediately before every
// delivery attempt. The second call is not redundant — DNS is not static.
// A hostname that resolved to a public address at registration time can be
// repointed at an internal address later (DNS rebinding), so send-time
// re-validation is the actual defense; registration-time validation only
// rejects the obviously-bad case early.
package webhookssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var (
	ErrNotHTTPS       = errors.New("webhook target must use https")
	ErrInvalidURL     = errors.New("webhook target is not a valid URL")
	ErrNoHost         = errors.New("webhook target has no host")
	ErrDisallowedHost = errors.New("webhook target resolves to a disallowed address")
	ErrResolveFailed  = errors.New("webhook target host could not be resolved")
)

// Resolver abstracts DNS resolution so tests can inject fake results instead
// of depending on real network lookups.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// netResolver delegates to the standard library resolver.
type netResolver struct{}

func (netResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// DefaultResolver is the production Resolver.
var DefaultResolver Resolver = netResolver{}

// Validate checks rawURL for scheme, host, and — resolving through resolver —
// that every address the host resolves to is a public, routable address.
// It rejects private (RFC1918/RFC4193), loopback, link-local (including the
// 169.254.169.254 / fd00:ec2::254 cloud metadata addresses, which fall in the
// link-local ranges), unspecified, and multicast ranges.
//
// A hostname with multiple A/AAAA records is rejected if ANY resolved address
// is disallowed — an attacker only needs one disallowed record to succeed,
// so allowing the rest through would defeat the check.
func Validate(ctx context.Context, resolver Resolver, rawURL string) error {
	if resolver == nil {
		resolver = DefaultResolver
	}

	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if u.Scheme != "https" {
		return ErrNotHTTPS
	}
	host := u.Hostname()
	if host == "" {
		return ErrNoHost
	}

	// A literal IP in the URL (https://169.254.169.254/) skips DNS but must
	// still be checked directly.
	if addr, perr := netip.ParseAddr(host); perr == nil {
		if isDisallowed(addr) {
			return fmt.Errorf("%w: %s", ErrDisallowedHost, addr)
		}
		return nil
	}

	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrResolveFailed, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: no addresses returned for %s", ErrResolveFailed, host)
	}
	for _, a := range addrs {
		addr, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			return fmt.Errorf("%w: unparseable resolved address for %s", ErrDisallowedHost, host)
		}
		addr = addr.Unmap()
		if isDisallowed(addr) {
			return fmt.Errorf("%w: %s resolves to %s", ErrDisallowedHost, host, addr)
		}
	}
	return nil
}

// isDisallowed reports whether addr falls in a private, loopback,
// link-local, unspecified, or multicast range. netip's own IsPrivate /
// IsLoopback / IsLinkLocalUnicast / IsUnspecified / IsMulticast cover
// RFC1918, RFC4193 (ULA), 127.0.0.0/8, ::1, 169.254.0.0/16, fe80::/10
// (which includes the 169.254.169.254 and fd00:ec2::254 cloud metadata
// addresses), 0.0.0.0, and multicast — the full set the issue calls out.
func isDisallowed(addr netip.Addr) bool {
	return addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified() ||
		addr.IsMulticast() ||
		addr.IsInterfaceLocalMulticast()
}
