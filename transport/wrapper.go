// Package transport provides the SSRF-safe outbound HTTP delivery
// primitive shared by every provider package in this module. It never
// makes assumptions about the host application's HTTP client or SSRF
// policy — both are supplied by the caller via the ClientFactory and
// URLValidator seams (see Option, WithClientFactory, WithURLValidator).
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// MaxRequestBodyBytes is the maximum outbound request payload size accepted
// by Send.
const MaxRequestBodyBytes = 256 * 1024

// MaxResponseBodyBytes is the maximum response payload size read from a
// provider before Send gives up and returns an error.
const MaxResponseBodyBytes = 1024 * 1024

// ClientFactory builds the *http.Client used for outbound provider
// requests. The host application is responsible for SSRF hardening
// (private-IP blocking, DNS-rebinding protection, redirect limits) inside
// its implementation.
type ClientFactory func(allowHTTP bool, maxRedirects int) *http.Client

// URLValidator validates and normalizes a destination URL before dispatch,
// returning the (possibly normalized) URL or an error if the destination is
// disallowed. Implementations are expected to enforce the host
// application's SSRF policy in full: scheme allowlisting, host/IP-literal
// checks, and DNS-resolved IP checks.
type URLValidator func(rawURL string, allowHTTP bool) (string, error)

// Request is a single outbound notification dispatch request.
type Request struct {
	// URL is the destination to POST Body to.
	URL string

	// Headers are additional outbound headers. Only an allowlisted subset
	// is actually sent — see sanitizeOutboundHeaders.
	Headers map[string]string

	// Body is the raw request payload (typically JSON).
	Body []byte
}

// Result describes a successful dispatch.
type Result struct {
	// StatusCode is the HTTP status code returned by the provider.
	StatusCode int

	// ResponseBody is the (size-capped) response body.
	ResponseBody []byte

	// Attempts is how many HTTP requests were made, including retries.
	Attempts int
}

// wrapperConfig accumulates Option values before a Wrapper is constructed.
type wrapperConfig struct {
	clientFactory ClientFactory
	urlValidator  URLValidator
	retryPolicy   RetryPolicy
	allowHTTP     bool
	maxRedirects  int
}

// Option configures a Wrapper at construction time.
type Option func(*wrapperConfig)

// WithClientFactory overrides the default *http.Client construction.
func WithClientFactory(f ClientFactory) Option {
	return func(c *wrapperConfig) { c.clientFactory = f }
}

// WithURLValidator overrides the destination URL validator. If never
// supplied, NewWrapper falls back to DefaultURLValidator.
func WithURLValidator(v URLValidator) Option {
	return func(c *wrapperConfig) { c.urlValidator = v }
}

// WithRetryPolicy overrides the default retry/backoff policy.
func WithRetryPolicy(p RetryPolicy) Option {
	return func(c *wrapperConfig) { c.retryPolicy = p }
}

// WithAllowHTTP allows plain-HTTP (non-TLS) destinations, and loopback
// destinations resolving to "localhost". Intended for local development
// and tests, never for production traffic to untrusted destinations.
func WithAllowHTTP(allow bool) Option {
	return func(c *wrapperConfig) { c.allowHTTP = allow }
}

// WithMaxRedirects sets the maximum number of redirects the supplied
// ClientFactory is told to honor. The Wrapper does not itself enforce this
// limit; it is passed through to ClientFactory, whose *http.Client is
// expected to implement the limit.
func WithMaxRedirects(n int) Option {
	return func(c *wrapperConfig) { c.maxRedirects = n }
}

// Wrapper dispatches outbound notification payloads with SSRF-safe
// destination validation, retry/backoff, and response-size caps.
type Wrapper struct {
	clientFactory ClientFactory
	urlValidator  URLValidator
	retryPolicy   RetryPolicy
	allowHTTP     bool
	maxRedirects  int

	// sleep and jitterNanos are test seams; production callers never set
	// them and get the real time.Sleep / crypto/rand-backed jitter.
	sleep       func(time.Duration)
	jitterNanos func(int64) int64
}

// defaultClientFactory returns a plain *http.Client with a sane timeout.
// It performs no SSRF hardening of its own — production callers should
// always supply a real ClientFactory via WithClientFactory.
func defaultClientFactory(bool, int) *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// NewWrapper constructs a Wrapper. With no options, it uses a plain
// *http.Client with no SSRF hardening of its own and a 3-attempt retry
// policy with 200ms/2s backoff. Callers should supply WithURLValidator
// (this package's DefaultURLValidator is a reasonable, conservative
// starting point) and, for production traffic, WithClientFactory.
func NewWrapper(opts ...Option) *Wrapper {
	cfg := wrapperConfig{
		clientFactory: defaultClientFactory,
		retryPolicy: RetryPolicy{
			MaxAttempts: 3,
			BaseDelay:   200 * time.Millisecond,
			MaxDelay:    2 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Wrapper{
		clientFactory: cfg.clientFactory,
		urlValidator:  cfg.urlValidator,
		retryPolicy:   cfg.retryPolicy,
		allowHTTP:     cfg.allowHTTP,
		maxRedirects:  cfg.maxRedirects,
		sleep:         time.Sleep,
	}
}

// Send dispatches req, validating the destination, retrying transient
// failures per the configured RetryPolicy, and capping request/response
// sizes.
func (w *Wrapper) Send(ctx context.Context, req Request) (*Result, error) {
	if len(req.Body) > MaxRequestBodyBytes {
		return nil, fmt.Errorf("request payload exceeds maximum size")
	}

	destURL, hostHeader, err := w.validateDestination(req.URL)
	if err != nil {
		return nil, err
	}

	safeRequestURL := buildSafeRequestURL(destURL)
	headers := sanitizeOutboundHeaders(req.Headers)

	client := w.clientFactory(w.allowHTTP, w.maxRedirects)
	w.applyRedirectGuard(client)

	var lastErr error
	for attempt := 1; attempt <= w.retryPolicy.MaxAttempts; attempt++ {
		httpReq, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, safeRequestURL.String(), bytes.NewReader(req.Body))
		if reqErr != nil {
			return nil, fmt.Errorf("create outbound request: %w", reqErr)
		}

		httpReq.Host = hostHeader

		for key, value := range headers {
			httpReq.Header.Set(key, value)
		}
		if httpReq.Header.Get("Content-Type") == "" {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		resp, doErr := executeRequest(client, httpReq)
		if doErr != nil {
			lastErr = doErr
			if attempt < w.retryPolicy.MaxAttempts && shouldRetry(nil, doErr) {
				w.waitBeforeRetry(attempt)
				continue
			}
			return nil, fmt.Errorf("outbound request failed: %s", sanitizeTransportErrorReason(doErr))
		}

		body, bodyErr := readCappedResponseBody(resp.Body)
		closeErr := resp.Body.Close()
		if bodyErr != nil {
			return nil, bodyErr
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close response body: %w", closeErr)
		}

		if shouldRetry(resp, nil) && attempt < w.retryPolicy.MaxAttempts {
			w.waitBeforeRetry(attempt)
			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			if hint := extractProviderErrorHint(body); hint != "" {
				return nil, fmt.Errorf("provider returned status %d: %s", resp.StatusCode, hint)
			}
			return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
		}

		return &Result{
			StatusCode:   resp.StatusCode,
			ResponseBody: body,
			Attempts:     attempt,
		}, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("provider request failed after retries: %s", sanitizeTransportErrorReason(lastErr))
	}

	return nil, fmt.Errorf("provider request failed")
}

// validateDestination runs rawURL through query-auth-key rejection and the
// configured URLValidator (which owns all scheme/host/IP-classification
// SSRF decisions), then rejects userinfo and fragments, which are generic
// URL-hygiene checks unrelated to any specific SSRF policy.
func (w *Wrapper) validateDestination(rawURL string) (destURL *neturl.URL, hostHeader string, err error) {
	parsedURL, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid destination URL")
	}

	if hasDisallowedQueryAuthKey(parsedURL.Query()) {
		return nil, "", fmt.Errorf("destination URL query authentication is not allowed")
	}

	if w.urlValidator == nil {
		return nil, "", fmt.Errorf("no URL validator configured")
	}

	validatedURL, validateErr := w.urlValidator(rawURL, w.allowHTTP)
	if validateErr != nil {
		return nil, "", fmt.Errorf("destination URL validation failed")
	}

	destURL, err = neturl.Parse(validatedURL)
	if err != nil {
		return nil, "", fmt.Errorf("destination URL validation failed")
	}

	if destURL.User != nil || destURL.Fragment != "" {
		return nil, "", fmt.Errorf("destination URL validation failed")
	}

	hostname := strings.TrimSpace(destURL.Hostname())
	if hostname == "" {
		return nil, "", fmt.Errorf("destination URL validation failed")
	}

	return destURL, destURL.Host, nil
}

// buildSafeRequestURL preserves the original hostname in the URL so Go's
// TLS layer derives the correct ServerName for SNI and certificate
// verification, while dropping any parts (userinfo, fragment) already
// rejected by validateDestination.
func buildSafeRequestURL(destURL *neturl.URL) *neturl.URL {
	safe := &neturl.URL{
		Scheme:   destURL.Scheme,
		Host:     destURL.Host,
		Path:     destURL.EscapedPath(),
		RawQuery: destURL.RawQuery,
	}
	if safe.Path == "" {
		safe.Path = "/"
	}
	return safe
}

// applyRedirectGuard wraps client's CheckRedirect (preserving any existing
// policy) so every redirect target is re-validated through the same
// destination checks as the initial request.
func (w *Wrapper) applyRedirectGuard(client *http.Client) {
	if client == nil {
		return
	}

	originalCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if originalCheckRedirect != nil {
			if err := originalCheckRedirect(req, via); err != nil {
				return err
			}
		}

		if req == nil || req.URL == nil {
			return fmt.Errorf("destination URL validation failed")
		}

		_, _, err := w.validateDestination(req.URL.String())
		return err
	}
}

func hasDisallowedQueryAuthKey(query neturl.Values) bool {
	for key := range query {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "token", "auth", "apikey", "api_key":
			return true
		}
	}
	return false
}

func sanitizeOutboundHeaders(headers map[string]string) map[string]string {
	allowed := map[string]struct{}{
		"content-type":  {},
		"user-agent":    {},
		"x-request-id":  {},
		"x-gotify-key":  {},
		"authorization": {},
	}

	sanitized := make(map[string]string)
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, ok := allowed[normalizedKey]; !ok {
			continue
		}
		sanitized[http.CanonicalHeaderKey(normalizedKey)] = strings.TrimSpace(value)
	}
	return sanitized
}

func sanitizeTransportErrorReason(err error) string {
	if err == nil {
		return "connection failed"
	}

	errText := strings.ToLower(strings.TrimSpace(err.Error()))

	switch {
	case strings.Contains(errText, "no such host"):
		return "dns lookup failed"
	case strings.Contains(errText, "connection refused"):
		return "connection refused"
	case strings.Contains(errText, "no route to host") || strings.Contains(errText, "network is unreachable"):
		return "network unreachable"
	case strings.Contains(errText, "timeout") || strings.Contains(errText, "deadline exceeded"):
		return "request timed out"
	case strings.Contains(errText, "tls") || strings.Contains(errText, "certificate") || strings.Contains(errText, "x509"):
		return "tls handshake failed"
	default:
		return "connection failed"
	}
}

// extractProviderErrorHint attempts to extract a short, human-readable
// error description from a JSON error response body. Only well-known
// fields are extracted to avoid accidentally surfacing sensitive or
// overlong content from arbitrary providers.
func extractProviderErrorHint(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var errResp map[string]any
	if err := json.Unmarshal(body, &errResp); err != nil {
		return ""
	}
	for _, key := range []string{"description", "message", "error", "error_description"} {
		v, ok := errResp[key]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		if len(s) > 100 {
			s = s[:100] + "..."
		}
		return strings.TrimSpace(s)
	}
	return ""
}

func readCappedResponseBody(body io.Reader) ([]byte, error) {
	limited := io.LimitReader(body, MaxResponseBodyBytes+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if len(content) > MaxResponseBodyBytes {
		return nil, fmt.Errorf("response payload exceeds maximum size")
	}

	return content, nil
}

func (w *Wrapper) waitBeforeRetry(attempt int) {
	delay := w.retryPolicy.BaseDelay << (attempt - 1)
	if delay > w.retryPolicy.MaxDelay {
		delay = w.retryPolicy.MaxDelay
	}

	jitterFn := w.jitterNanos
	if jitterFn == nil {
		jitterFn = defaultJitterNanos
	}

	jitter := time.Duration(jitterFn(int64(delay) / 2))
	sleepFn := w.sleep
	if sleepFn == nil {
		sleepFn = time.Sleep
	}
	sleepFn(delay + jitter)
}
