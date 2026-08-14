//go:build integration
// +build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wikid82/go_notify_yourself/transport"
)

func newIntegrationWrapper() *transport.Wrapper {
	return transport.NewWrapper(transport.WithAllowHTTP(true))
}

func TestWrapperIntegration_RetriesOn429AndSucceeds(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&calls, 1)
		if current == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	wrapper := newIntegrationWrapper()
	result, err := wrapper.Send(context.Background(), transport.Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("expected retry success, got error: %v", err)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", result.Attempts)
	}
}

func TestWrapperIntegration_DoesNotRetryOn400(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	wrapper := newIntegrationWrapper()
	_, err := wrapper.Send(context.Background(), transport.Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil {
		t.Fatalf("expected non-retryable 400 error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one request attempt, got %d", calls)
	}
}

func TestWrapperIntegration_RejectsTokenizedQueryWithoutEcho(t *testing.T) {
	t.Parallel()

	wrapper := newIntegrationWrapper()
	secret := "pr1-secret-token-value"
	_, err := wrapper.Send(context.Background(), transport.Request{
		URL:  "http://example.com/hook?token=" + secret,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil {
		t.Fatalf("expected tokenized query rejection")
	}
	if !strings.Contains(err.Error(), "query authentication is not allowed") {
		t.Fatalf("expected sanitized query-auth rejection, got: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error must not echo secret token")
	}
}

// TestWrapperIntegration_HeaderAllowlistSafety exercises the outbound
// header allowlist end-to-end. Note this differs from the original
// (pre-extraction) version of this test: that variant additionally
// asserted that Authorization was stripped, but that relied on a stripping
// behavior specific to the host application's own safe HTTP client
// (supplied via ClientFactory there, not part of this module). This
// module's own sanitizeOutboundHeaders allowlist *keeps* Authorization
// deliberately — it's required for ntfy's Bearer-token auth (see
// transport's TestSanitizeOutboundHeadersAllowlist) — so this test asserts
// the module's actual documented contract: only non-allowlisted headers
// like Cookie or arbitrary custom headers are stripped.
func TestWrapperIntegration_HeaderAllowlistSafety(t *testing.T) {
	t.Parallel()

	var seenAuthHeader string
	var seenCookieHeader string
	var seenGotifyKey string
	var seenCustomHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthHeader = r.Header.Get("Authorization")
		seenCookieHeader = r.Header.Get("Cookie")
		seenGotifyKey = r.Header.Get("X-Gotify-Key")
		seenCustomHeader = r.Header.Get("X-Custom-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wrapper := newIntegrationWrapper()
	_, err := wrapper.Send(context.Background(), transport.Request{
		URL: server.URL,
		Headers: map[string]string{
			"Authorization":   "Bearer allowed-for-ntfy",
			"Cookie":          "session=should-not-leak",
			"X-Gotify-Key":    "allowed-token",
			"X-Custom-Secret": "should-not-leak",
		},
		Body: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if seenAuthHeader != "Bearer allowed-for-ntfy" {
		t.Fatalf("expected Authorization to pass through the allowlist, got %q", seenAuthHeader)
	}
	if seenCookieHeader != "" {
		t.Fatalf("cookie header must be stripped")
	}
	if seenGotifyKey != "allowed-token" {
		t.Fatalf("expected X-Gotify-Key to pass through")
	}
	if seenCustomHeader != "" {
		t.Fatalf("non-allowlisted custom header must be stripped")
	}
}
