package transport

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestDefaultURLValidatorAllowsHTTPS(t *testing.T) {
	got, err := DefaultURLValidator("https://example.com/hook", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/hook" {
		t.Fatalf("expected URL unchanged, got %q", got)
	}
}

func TestDefaultURLValidatorRejectsHTTPByDefault(t *testing.T) {
	_, err := DefaultURLValidator("http://example.com/hook", false)
	if err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Fatalf("expected plain-HTTP rejection, got: %v", err)
	}
}

func TestDefaultURLValidatorAllowsHTTPWhenAllowed(t *testing.T) {
	_, err := DefaultURLValidator("http://1.1.1.1/hook", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultURLValidatorRejectsUnsupportedScheme(t *testing.T) {
	_, err := DefaultURLValidator("ftp://example.com/hook", true)
	if err == nil || !strings.Contains(err.Error(), "unsupported destination scheme") {
		t.Fatalf("expected scheme rejection, got: %v", err)
	}
}

func TestDefaultURLValidatorRejectsInvalidURL(t *testing.T) {
	_, err := DefaultURLValidator("http://[::1", true)
	if err == nil || !strings.Contains(err.Error(), "invalid destination URL") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestDefaultURLValidatorRejectsCredentials(t *testing.T) {
	_, err := DefaultURLValidator("https://user:pass@example.com/hook", false) //nolint:gosec // test verifies rejection
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentials rejection, got: %v", err)
	}
}

func TestDefaultURLValidatorRejectsEmptyHostname(t *testing.T) {
	_, err := DefaultURLValidator("https:///hook", false)
	if err == nil || !strings.Contains(err.Error(), "missing a hostname") {
		t.Fatalf("expected missing-hostname rejection, got: %v", err)
	}
}

func TestDefaultURLValidatorRejectsPrivateIPLiteral(t *testing.T) {
	tests := []string{
		"https://10.0.0.1/hook",
		"https://172.16.0.1/hook",
		"https://192.168.1.1/hook",
		"https://169.254.169.254/hook",
		"https://0.0.0.0/hook",
		"https://[::1]/hook",
		"https://[fc00::1]/hook",
		"https://[fe80::1]/hook",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			_, err := DefaultURLValidator(rawURL, false)
			if err == nil {
				t.Fatalf("expected rejection for %q", rawURL)
			}
		})
	}
}

func TestDefaultURLValidatorAllowsPublicIPLiteral(t *testing.T) {
	_, err := DefaultURLValidator("https://1.1.1.1/hook", false)
	if err != nil {
		t.Fatalf("unexpected error for public IP: %v", err)
	}
}

func TestDefaultURLValidatorRejectsLoopbackWithoutAllowHTTP(t *testing.T) {
	_, err := DefaultURLValidator("https://127.0.0.1/hook", false)
	if err == nil {
		t.Fatal("expected loopback rejection without allowHTTP")
	}
}

func TestDefaultURLValidatorAllowsLoopbackForLocalhostWithAllowHTTP(t *testing.T) {
	_, err := DefaultURLValidator("http://localhost/hook", true)
	if err != nil {
		t.Fatalf("expected localhost allowed with allowHTTP, got: %v", err)
	}
}

func TestDefaultURLValidatorRejectsLoopbackForNonLocalhostHostname(t *testing.T) {
	// hostname is an IP literal "127.0.0.1" but not the string "localhost" —
	// isLocalDestinationHost still accepts loopback IP literals, so use a
	// resolvable non-localhost name is impractical in a unit test; instead
	// verify the IP-literal loopback path directly via isAllowedIP.
	if isAllowedIP("not-localhost.invalid", net.ParseIP("127.0.0.1"), false) {
		t.Fatal("expected loopback rejected without allowHTTP")
	}
}

func TestDefaultURLValidatorRejectsUnresolvableHostname(t *testing.T) {
	_, err := DefaultURLValidator("https://this-host-does-not-exist.invalid/hook", false)
	if err == nil || !strings.Contains(err.Error(), "could not resolve") {
		t.Fatalf("expected DNS resolution failure, got: %v", err)
	}
}

func TestIsPrivateIPNilIP(t *testing.T) {
	if !isPrivateIP(nil) {
		t.Fatal("expected nil IP to be treated as private/blocked")
	}
}

func TestIsPrivateIPIPv4MappedIPv6(t *testing.T) {
	ip := net.ParseIP("::ffff:10.0.0.1")
	if !isPrivateIP(ip) {
		t.Fatal("expected IPv4-mapped IPv6 private address to be blocked")
	}
}

func TestIsPrivateIPMulticastAndUnspecified(t *testing.T) {
	if !isPrivateIP(net.ParseIP("224.0.0.1")) {
		t.Fatal("expected multicast to be blocked")
	}
	if !isPrivateIP(net.IPv4zero) {
		t.Fatal("expected unspecified address to be blocked")
	}
}

func TestIsPrivateIPPublicAddressAllowed(t *testing.T) {
	if isPrivateIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("expected public IP to be allowed")
	}
}

func TestIsLocalDestinationHostVariants(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"example.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := isLocalDestinationHost(tt.host); got != tt.expected {
				t.Fatalf("isLocalDestinationHost(%q) = %v, want %v", tt.host, got, tt.expected)
			}
		})
	}
}

func TestNewWrapperUsesDefaultURLValidatorEndToEnd(t *testing.T) {
	wrapper := NewWrapper(WithAllowHTTP(false), WithRetryPolicy(RetryPolicy{MaxAttempts: 1}))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "https://192.168.1.1/hook",
		Body: []byte(`{"message":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected default validator to reject a private IP destination")
	}
}
