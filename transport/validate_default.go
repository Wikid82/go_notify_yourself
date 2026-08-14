package transport

import (
	"fmt"
	"net"
	neturl "net/url"
	"strings"
)

// privateCIDRs enumerates private, loopback, link-local, and other reserved
// IP ranges that DefaultURLValidator refuses to dispatch to. This
// intentionally duplicates only the *public, standardized* CIDR ranges
// (RFC 1918, RFC 3927, RFC 4193, IANA-reserved blocks) — no host-specific
// business logic — so this module is immediately useful standalone without
// forcing every consumer to write SSRF logic from scratch. Production hosts
// with their own SSRF infrastructure should override this via
// WithURLValidator rather than relying on this default.
var privateCIDRs = []string{
	// IPv4 Private Networks (RFC 1918)
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",

	// IPv4 Link-Local (RFC 3927) — includes cloud metadata endpoints
	// (e.g. 169.254.169.254) reachable from within a cloud VM.
	"169.254.0.0/16",

	// IPv4 Loopback
	"127.0.0.0/8",

	// IPv4 Reserved ranges
	"0.0.0.0/8",          // "This network"
	"240.0.0.0/4",        // Reserved for future use
	"255.255.255.255/32", // Broadcast

	// IPv6 Loopback
	"::1/128",

	// IPv6 Unique Local Addresses (RFC 4193)
	"fc00::/7",

	// IPv6 Link-Local
	"fe80::/10",
}

var privateBlocks = mustParseCIDRs(privateCIDRs)

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	blocks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			// privateCIDRs is a fixed, compile-time-known list; a parse
			// failure here means the literal itself is wrong.
			panic(fmt.Sprintf("transport: invalid built-in CIDR %q: %v", cidr, err))
		}
		blocks = append(blocks, block)
	}
	return blocks
}

// isPrivateIP reports whether ip falls in a private, loopback, link-local,
// or other reserved range that DefaultURLValidator blocks.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	// Normalize IPv4-mapped IPv6 addresses (::ffff:x.x.x.x) so they're
	// checked against the IPv4 ranges above.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	for _, block := range privateBlocks {
		if block.Contains(ip) {
			return true
		}
	}

	return false
}

func isLocalDestinationHost(host string) bool {
	trimmed := strings.TrimSpace(host)
	if strings.EqualFold(trimmed, "localhost") {
		return true
	}
	parsedIP := net.ParseIP(trimmed)
	return parsedIP != nil && parsedIP.IsLoopback()
}

// DefaultURLValidator is a conservative, self-contained SSRF-safe
// URLValidator with zero dependencies beyond the standard library:
//
//   - Only https:// destinations are allowed unless allowHTTP is true.
//   - When allowHTTP is true, plain http:// is allowed, and loopback
//     destinations are allowed only when the hostname is "localhost" or a
//     loopback IP literal.
//   - IP-literal hosts are rejected if they fall in a private/reserved
//     range (RFC 1918, loopback, link-local, IANA-reserved, IPv6 ULA).
//   - Hostnames are resolved via DNS and every resolved address must pass
//     the same private/reserved-range check.
//   - URLs carrying userinfo are rejected (kept in the Wrapper itself as a
//     defense-in-depth generic check, but rejecting it here too costs
//     nothing and keeps this validator usable standalone).
//
// This is a reasonable, immediately-usable default — not a substitute for
// a host application's own SSRF infrastructure (DNS-rebinding protection at
// dial time, redirect-time re-resolution, etc). Production hosts should
// supply their own URLValidator via WithURLValidator.
func DefaultURLValidator(rawURL string, allowHTTP bool) (string, error) {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid destination URL: %w", err)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		// always allowed
	case "http":
		if !allowHTTP {
			return "", fmt.Errorf("plain HTTP destinations are not allowed")
		}
	default:
		return "", fmt.Errorf("unsupported destination scheme %q", parsed.Scheme)
	}

	if parsed.User != nil {
		return "", fmt.Errorf("destination URL must not contain credentials")
	}

	hostname := strings.TrimSpace(parsed.Hostname())
	if hostname == "" {
		return "", fmt.Errorf("destination URL is missing a hostname")
	}

	if parsedIP := net.ParseIP(hostname); parsedIP != nil {
		if !isAllowedIP(hostname, parsedIP, allowHTTP) {
			return "", fmt.Errorf("destination IP %q is not allowed", hostname)
		}
		return rawURL, nil
	}

	resolvedIPs, lookupErr := net.LookupIP(hostname)
	if lookupErr != nil || len(resolvedIPs) == 0 {
		return "", fmt.Errorf("could not resolve destination hostname %q", hostname)
	}

	for _, resolvedIP := range resolvedIPs {
		if !isAllowedIP(hostname, resolvedIP, allowHTTP) {
			return "", fmt.Errorf("destination hostname %q resolves to a disallowed address", hostname)
		}
	}

	return rawURL, nil
}

func isAllowedIP(hostname string, ip net.IP, allowHTTP bool) bool {
	if ip == nil {
		return false
	}

	if ip.IsLoopback() {
		return allowHTTP && isLocalDestinationHost(hostname)
	}

	return !isPrivateIP(ip)
}
