package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// passthroughValidator is a fake URLValidator that accepts every URL
// unchanged. Tests that don't care about SSRF policy use this so they can
// focus on wrapper behavior (retries, size caps, header handling, etc).
func passthroughValidator(rawURL string, _ bool) (string, error) {
	return rawURL, nil
}

// rejectSchemeValidator is a fake URLValidator that rejects any URL whose
// scheme is not http/https — enough to exercise the redirect-guard path
// without depending on DefaultURLValidator (tested separately).
func rejectSchemeValidator(rawURL string, _ bool) (string, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("disallowed scheme %q", u.Scheme)
	}
	return rawURL, nil
}

func newTestWrapper(opts ...Option) *Wrapper {
	base := []Option{
		WithURLValidator(passthroughValidator),
		WithAllowHTTP(true),
	}
	return NewWrapper(append(base, opts...)...)
}

func TestWrapperSendRejectsOversizedRequestBody(t *testing.T) {
	wrapper := newTestWrapper()

	payload := make([]byte, MaxRequestBodyBytes+1)
	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://example.com/hook",
		Body: payload,
	})
	if err == nil || !strings.Contains(err.Error(), "request payload exceeds") {
		t.Fatalf("expected oversized request body error, got: %v", err)
	}
}

func TestWrapperSendRejectsTokenizedQueryURL(t *testing.T) {
	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://example.com/hook?token=secret",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "query authentication is not allowed") {
		t.Fatalf("expected query token rejection, got: %v", err)
	}
}

func TestWrapperSendRejectsQueryAuthCaseVariants(t *testing.T) {
	testCases := []string{
		"http://example.com/hook?Token=secret",
		"http://example.com/hook?AUTH=secret",
		"http://example.com/hook?apiKey=secret",
	}

	for _, testURL := range testCases {
		t.Run(testURL, func(t *testing.T) {
			wrapper := newTestWrapper()

			_, err := wrapper.Send(context.Background(), Request{
				URL:  testURL,
				Body: []byte(`{"message":"hello"}`),
			})
			if err == nil || !strings.Contains(err.Error(), "query authentication is not allowed") {
				t.Fatalf("expected query auth rejection for %q, got: %v", testURL, err)
			}
		})
	}
}

func TestWrapperSendNoURLValidatorConfigured(t *testing.T) {
	wrapper := NewWrapper(WithAllowHTTP(true))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://example.com/hook",
		Body: []byte(`{"message":"hi"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "no URL validator configured") {
		t.Fatalf("expected explicit no-validator error, got: %v", err)
	}
}

func TestWrapperSendRejectsUserInfoInDestinationURL(t *testing.T) {
	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{ //nolint:gosec // test verifies rejection of credentials in URL
		URL:  "https://user:pass@example.com/hook",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected destination validation failure, got: %v", err)
	}
}

func TestWrapperSendRejectsFragmentInDestinationURL(t *testing.T) {
	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "https://example.com/hook#fragment",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected destination validation failure, got: %v", err)
	}
}

func TestWrapperSendRejectsEmptyHostname(t *testing.T) {
	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "file:///etc/passwd",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected empty-hostname rejection, got: %v", err)
	}
}

func TestWrapperSendRejectsWhenValidatorErrors(t *testing.T) {
	wrapper := newTestWrapper(WithURLValidator(func(string, bool) (string, error) {
		return "", fmt.Errorf("blocked by policy")
	}))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "https://example.com/hook",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected validator error to surface as destination validation failure, got: %v", err)
	}
}

func TestWrapperSendRejectsInvalidValidatedURL(t *testing.T) {
	wrapper := newTestWrapper(WithURLValidator(func(string, bool) (string, error) {
		return "http://[::1", nil // malformed, will fail neturl.Parse
	}))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "https://example.com/hook",
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected malformed validated URL rejection, got: %v", err)
	}
}

func TestWrapperSendRejectsInvalidRequestURL(t *testing.T) {
	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://[::1", // malformed
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid destination URL") {
		t.Fatalf("expected invalid destination URL error, got: %v", err)
	}
}

func TestWrapperSendRejectsRedirectTargetWithDisallowedScheme(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Redirect(w, r, "ftp://example.com/redirected", http.StatusFound)
	}))
	defer server.Close()

	wrapper := newTestWrapper(WithURLValidator(rejectSchemeValidator), WithRetryPolicy(RetryPolicy{MaxAttempts: 1}))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "outbound request failed") {
		t.Fatalf("expected outbound failure due to redirect target validation, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected only initial request due to blocked redirect, got %d attempts", got)
	}
}

func TestWrapperSendRejectsRedirectTargetWithQueryAuth(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		http.Redirect(w, r, "https://example.com/redirected?Token=secret", http.StatusFound)
	}))
	defer server.Close()

	wrapper := newTestWrapper(WithRetryPolicy(RetryPolicy{MaxAttempts: 1}))

	_, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "outbound request failed") {
		t.Fatalf("expected outbound failure due to redirect query auth validation, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected only initial request due to blocked redirect, got %d attempts", got)
	}
}

func TestWrapperRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&calls, 1)
		if current == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	wrapper := newTestWrapper()
	wrapper.sleep = func(time.Duration) {}
	wrapper.jitterNanos = func(int64) int64 { return 0 }

	result, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", result.Attempts)
	}
}

func TestWrapperSendSuccessWithValidatedDestination(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected default content-type, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	wrapper := newTestWrapper(
		WithRetryPolicy(RetryPolicy{MaxAttempts: 1}),
		WithClientFactory(func(bool, int) *http.Client { return server.Client() }),
	)

	result, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("expected successful send, got error: %v", err)
	}
	if result.Attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", result.Attempts)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, result.StatusCode)
	}
}

func TestWrapperDoesNotRetryOn400(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	wrapper := newTestWrapper()
	wrapper.sleep = func(time.Duration) {}
	wrapper.jitterNanos = func(int64) int64 { return 0 }

	_, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("expected non-retryable 400 error, got: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected exactly one request attempt, got %d", calls)
	}
}

func TestWrapperResponseBodyCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, strings.Repeat("x", MaxResponseBodyBytes+8))
	}))
	defer server.Close()

	wrapper := newTestWrapper()

	_, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"message":"hello"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "response payload exceeds") {
		t.Fatalf("expected capped response body error, got: %v", err)
	}
}

func TestSanitizeOutboundHeadersAllowlist(t *testing.T) {
	headers := sanitizeOutboundHeaders(map[string]string{
		"Content-Type":  "application/json",
		"User-Agent":    "notify-transport/1.0",
		"X-Request-ID":  "abc",
		"X-Gotify-Key":  "secret",
		"Authorization": "Bearer token",
		"Cookie":        "sid=1",
	})

	if len(headers) != 5 {
		t.Fatalf("expected 5 allowed headers, got %d", len(headers))
	}
	if _, ok := headers["Authorization"]; !ok {
		t.Fatalf("authorization header must be allowed for ntfy Bearer auth")
	}
	if _, ok := headers["Cookie"]; ok {
		t.Fatalf("cookie header must be stripped")
	}
}

func TestWrapperApplyRedirectGuardNilClient(t *testing.T) {
	wrapper := newTestWrapper()
	wrapper.applyRedirectGuard(nil)
}

func TestWrapperApplyRedirectGuardPreservesOriginalBehavior(t *testing.T) {
	wrapper := newTestWrapper()
	baseErr := fmt.Errorf("base redirect policy")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return baseErr
	}}

	wrapper.applyRedirectGuard(client)
	err := client.CheckRedirect(&http.Request{URL: &neturl.URL{Scheme: "https", Host: "example.com"}}, nil)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected original redirect policy error, got: %v", err)
	}
}

func TestWrapperApplyRedirectGuardRejectsNilRequestURL(t *testing.T) {
	wrapper := newTestWrapper()
	client := &http.Client{}
	wrapper.applyRedirectGuard(client)

	err := client.CheckRedirect(&http.Request{}, nil)
	if err == nil || !strings.Contains(err.Error(), "destination URL validation failed") {
		t.Fatalf("expected validation failure for nil request URL, got: %v", err)
	}
}

func TestWrapperApplyRedirectGuardAllowsValidatedDestination(t *testing.T) {
	wrapper := newTestWrapper()
	client := &http.Client{}
	wrapper.applyRedirectGuard(client)

	err := client.CheckRedirect(&http.Request{URL: &neturl.URL{Scheme: "https", Host: "example.com", Path: "/hook"}}, nil)
	if err != nil {
		t.Fatalf("expected validated destination to pass guard, got: %v", err)
	}
}

func TestBuildSafeRequestURLPreservesHostnameForTLS(t *testing.T) {
	destinationURL := &neturl.URL{
		Scheme: "https",
		Host:   "example.com",
		Path:   "/webhook",
	}

	safeURL := buildSafeRequestURL(destinationURL)

	if safeURL.Hostname() != "example.com" {
		t.Fatalf("expected hostname 'example.com' preserved in URL for TLS SNI, got %q", safeURL.Hostname())
	}
	if safeURL.Scheme != "https" {
		t.Fatalf("expected scheme 'https', got %q", safeURL.Scheme)
	}
	if safeURL.Path != "/webhook" {
		t.Fatalf("expected path '/webhook', got %q", safeURL.Path)
	}
}

func TestBuildSafeRequestURLDefaultsEmptyPathToSlash(t *testing.T) {
	destinationURL := &neturl.URL{Scheme: "http", Host: "localhost"}

	safeURL := buildSafeRequestURL(destinationURL)
	if safeURL.Path != "/" {
		t.Fatalf("expected default path '/', got %q", safeURL.Path)
	}
}

func TestBuildSafeRequestURLPreservesQueryString(t *testing.T) {
	destinationURL := &neturl.URL{
		Scheme:   "https",
		Host:     "example.com",
		Path:     "/hook",
		RawQuery: "key=value",
	}

	safeURL := buildSafeRequestURL(destinationURL)
	if safeURL.RawQuery != "key=value" {
		t.Fatalf("expected query 'key=value', got %q", safeURL.RawQuery)
	}
}

// ===== Additional coverage for uncovered paths =====

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated read error")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestShouldRetryComprehensive(t *testing.T) {
	tests := []struct {
		name     string
		resp     *http.Response
		err      error
		expected bool
	}{
		{"nil resp nil err", nil, nil, false},
		{"timeout error string", nil, errors.New("operation timeout"), true},
		{"connection error string", nil, errors.New("connection reset"), true},
		{"unrelated error", nil, errors.New("json parse error"), false},
		{"500 response", &http.Response{StatusCode: 500}, nil, true},
		{"502 response", &http.Response{StatusCode: 502}, nil, true},
		{"503 response", &http.Response{StatusCode: 503}, nil, true},
		{"429 response", &http.Response{StatusCode: 429}, nil, true},
		{"200 response", &http.Response{StatusCode: 200}, nil, false},
		{"400 response", &http.Response{StatusCode: 400}, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetry(tt.resp, tt.err); got != tt.expected {
				t.Fatalf("shouldRetry = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldRetryNetError(t *testing.T) {
	netErr := &net.DNSError{Err: "no such host", Name: "example.invalid"}
	if !shouldRetry(nil, netErr) {
		t.Fatal("expected net.Error to trigger retry via errors.As fallback")
	}
}

func TestReadCappedResponseBodyReadError(t *testing.T) {
	_, err := readCappedResponseBody(errReader{})
	if err == nil || !strings.Contains(err.Error(), "read response body") {
		t.Fatalf("expected read body error, got: %v", err)
	}
}

func TestReadCappedResponseBodyOversize(t *testing.T) {
	oversized := strings.NewReader(strings.Repeat("x", MaxResponseBodyBytes+10))
	_, err := readCappedResponseBody(oversized)
	if err == nil || !strings.Contains(err.Error(), "response payload exceeds") {
		t.Fatalf("expected oversize error, got: %v", err)
	}
}

func TestReadCappedResponseBodySuccess(t *testing.T) {
	content, err := readCappedResponseBody(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(content))
	}
}

func TestHasDisallowedQueryAuthKeyAllVariants(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"token", "token", true},
		{"auth", "auth", true},
		{"apikey", "apikey", true},
		{"api_key", "api_key", true},
		{"TOKEN uppercase", "TOKEN", true},
		{"Api_Key mixed", "Api_Key", true},
		{"safe key", "callback", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := neturl.Values{}
			query.Set(tt.key, "secret")
			if got := hasDisallowedQueryAuthKey(query); got != tt.expected {
				t.Fatalf("hasDisallowedQueryAuthKey with key %q = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestHasDisallowedQueryAuthKeyEmptyQuery(t *testing.T) {
	if hasDisallowedQueryAuthKey(neturl.Values{}) {
		t.Fatal("expected empty query to be safe")
	}
}

func TestWaitBeforeRetryBasic(t *testing.T) {
	wrapper := newTestWrapper()
	var sleptDuration time.Duration
	wrapper.sleep = func(d time.Duration) { sleptDuration = d }
	wrapper.jitterNanos = func(int64) int64 { return 0 }
	wrapper.retryPolicy.BaseDelay = 100 * time.Millisecond
	wrapper.retryPolicy.MaxDelay = 1 * time.Second

	wrapper.waitBeforeRetry(1)
	if sleptDuration != 100*time.Millisecond {
		t.Fatalf("expected 100ms delay for attempt 1, got %v", sleptDuration)
	}

	wrapper.waitBeforeRetry(2)
	if sleptDuration != 200*time.Millisecond {
		t.Fatalf("expected 200ms delay for attempt 2, got %v", sleptDuration)
	}
}

func TestWaitBeforeRetryClampedToMax(t *testing.T) {
	wrapper := newTestWrapper()
	var sleptDuration time.Duration
	wrapper.sleep = func(d time.Duration) { sleptDuration = d }
	wrapper.jitterNanos = func(int64) int64 { return 0 }
	wrapper.retryPolicy.BaseDelay = 1 * time.Second
	wrapper.retryPolicy.MaxDelay = 2 * time.Second

	wrapper.waitBeforeRetry(5)
	if sleptDuration != 2*time.Second {
		t.Fatalf("expected clamped delay of 2s, got %v", sleptDuration)
	}
}

func TestWaitBeforeRetryDefaults(t *testing.T) {
	wrapper := newTestWrapper()
	wrapper.jitterNanos = nil
	wrapper.sleep = nil
	wrapper.retryPolicy.BaseDelay = 1 * time.Millisecond
	wrapper.retryPolicy.MaxDelay = 2 * time.Millisecond
	wrapper.waitBeforeRetry(1)
}

func TestWrapperSendExhaustsRetriesOnTransportError(t *testing.T) {
	var calls int32
	wrapper := newTestWrapper(WithClientFactory(func(bool, int) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				return nil, errors.New("connection timeout failure")
			}),
		}
	}))
	wrapper.sleep = func(time.Duration) {}
	wrapper.jitterNanos = func(int64) int64 { return 0 }

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://localhost:19999/hook",
		Body: []byte(`{"msg":"test"}`),
	})
	if err == nil {
		t.Fatal("expected error after transport failures")
	}
	if !strings.Contains(err.Error(), "outbound request failed") {
		t.Fatalf("expected outbound request failed message, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestWrapperSendExhaustsRetriesOn500(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	wrapper := newTestWrapper()
	wrapper.sleep = func(time.Duration) {}
	wrapper.jitterNanos = func(int64) int64 { return 0 }

	_, err := wrapper.Send(context.Background(), Request{
		URL:  server.URL,
		Body: []byte(`{"msg":"test"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("expected 500 status error, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts for 500 retries, got %d", got)
	}
}

func TestWrapperSendTransportErrorNoRetry(t *testing.T) {
	wrapper := newTestWrapper(
		WithRetryPolicy(RetryPolicy{MaxAttempts: 1}),
		WithClientFactory(func(bool, int) *http.Client {
			return &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return nil, errors.New("some unretryable error")
				}),
			}
		}),
	)

	_, err := wrapper.Send(context.Background(), Request{
		URL:  "http://localhost:19999/hook",
		Body: []byte(`{"msg":"test"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "outbound request failed") {
		t.Fatalf("expected outbound request failed, got: %v", err)
	}
}

func TestSanitizeTransportErrorReason(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "nil error", err: nil, expected: "connection failed"},
		{name: "dns error", err: errors.New("dial tcp: lookup gotify.example: no such host"), expected: "dns lookup failed"},
		{name: "connection refused", err: errors.New("connect: connection refused"), expected: "connection refused"},
		{name: "network unreachable", err: errors.New("connect: no route to host"), expected: "network unreachable"},
		{name: "timeout", err: errors.New("context deadline exceeded"), expected: "request timed out"},
		{name: "tls failure", err: errors.New("tls: handshake failure"), expected: "tls handshake failed"},
		{name: "fallback", err: errors.New("some unexpected transport error"), expected: "connection failed"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			actual := sanitizeTransportErrorReason(testCase.err)
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

func TestSanitizeTransportErrorReasonNetworkUnreachable(t *testing.T) {
	result := sanitizeTransportErrorReason(errors.New("connect: network is unreachable"))
	if result != "network unreachable" {
		t.Fatalf("expected 'network unreachable', got %q", result)
	}
}

func TestSanitizeTransportErrorReasonCertificate(t *testing.T) {
	result := sanitizeTransportErrorReason(errors.New("x509: certificate signed by unknown authority"))
	if result != "tls handshake failed" {
		t.Fatalf("expected 'tls handshake failed', got %q", result)
	}
}

func TestExtractProviderErrorHint(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		expected string
	}{
		{
			name:     "description field",
			body:     []byte(`{"description":"Not Found: chat not found"}`),
			expected: "Not Found: chat not found",
		},
		{
			name:     "message field",
			body:     []byte(`{"message":"Unauthorized"}`),
			expected: "Unauthorized",
		},
		{
			name:     "error field",
			body:     []byte(`{"error":"rate limited"}`),
			expected: "rate limited",
		},
		{
			name:     "error_description field",
			body:     []byte(`{"error_description":"invalid token"}`),
			expected: "invalid token",
		},
		{
			name:     "empty body",
			body:     []byte{},
			expected: "",
		},
		{
			name:     "non-JSON body",
			body:     []byte(`<html>Server Error</html>`),
			expected: "",
		},
		{
			name:     "string over 100 chars truncated",
			body:     []byte(`{"description":"` + strings.Repeat("x", 120) + `"}`),
			expected: strings.Repeat("x", 100) + "...",
		},
		{
			name:     "empty string value ignored",
			body:     []byte(`{"description":"","message":"fallback hint"}`),
			expected: "fallback hint",
		},
		{
			name:     "whitespace-only value ignored",
			body:     []byte(`{"description":"   ","message":"real hint"}`),
			expected: "real hint",
		},
		{
			name:     "non-string value ignored",
			body:     []byte(`{"description":42,"message":"string hint"}`),
			expected: "string hint",
		},
		{
			name:     "priority order: description before message",
			body:     []byte(`{"message":"second","description":"first"}`),
			expected: "first",
		},
		{
			name:     "no recognized fields",
			body:     []byte(`{"status":"error","code":500}`),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractProviderErrorHint(tt.body)
			if result != tt.expected {
				t.Errorf("extractProviderErrorHint(%q) = %q, want %q", string(tt.body), result, tt.expected)
			}
		})
	}
}

func TestWrapperSendCanceledContext(t *testing.T) {
	wrapper := newTestWrapper()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := wrapper.Send(ctx, Request{
		URL:  "https://example.com/hook",
		Body: []byte(`{"msg":"test"}`),
	})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
