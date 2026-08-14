package transport

import "net/http"

// executeRequest performs the outbound HTTP request. It exists purely as a
// package-level test seam (tests can shadow behavior via a custom
// http.RoundTripper on the *http.Client instead of this function directly,
// but keeping it as a thin, explicit wrapper mirrors the ported original
// and keeps Send's call sites simple to read).
func executeRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}
