package transport

import (
	crand "crypto/rand"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"
)

// RetryPolicy controls how many times Send retries a transient failure and
// how long it waits between attempts.
type RetryPolicy struct {
	// MaxAttempts is the total number of HTTP requests Send will attempt,
	// including the first (non-retry) attempt.
	MaxAttempts int

	// BaseDelay is the wait before the first retry. Each subsequent retry
	// doubles the previous delay (capped at MaxDelay), plus jitter.
	BaseDelay time.Duration

	// MaxDelay caps the backoff delay, before jitter is added.
	MaxDelay time.Duration
}

// shouldRetry reports whether a request should be retried given the
// response and/or transport error from the most recent attempt. Retries on
// network-level errors, 429 Too Many Requests, and 5xx server errors.
func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "timeout") || strings.Contains(errText, "connection") {
			return true
		}
		var netErr net.Error
		return errors.As(err, &netErr)
	}

	if resp == nil {
		return false
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}

	return resp.StatusCode >= http.StatusInternalServerError
}

// defaultJitterNanos returns a random duration (as nanoseconds) in
// [0, maxNanos), used to spread out retries and avoid a thundering herd.
func defaultJitterNanos(maxNanos int64) int64 {
	if maxNanos <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(maxNanos))
	if err != nil {
		return 0
	}
	return n.Int64()
}
