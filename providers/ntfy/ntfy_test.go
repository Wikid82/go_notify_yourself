package ntfy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

type capturingRoundTripper struct {
	lastRequest *http.Request
	lastBody    []byte
	statusCode  int
	respBody    string
}

func (c *capturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastRequest = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		c.lastBody = b
	}
	status := c.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(c.respBody)),
		Header:     make(http.Header),
	}, nil
}

func passthroughValidator(rawURL string, _ bool) (string, error) { return rawURL, nil }

func newTestWrapper(rt *capturingRoundTripper) *transport.Wrapper {
	return transport.NewWrapper(
		transport.WithURLValidator(passthroughValidator),
		transport.WithClientFactory(func(bool, int) *http.Client {
			return &http.Client{Transport: rt}
		}),
		transport.WithRetryPolicy(transport.RetryPolicy{MaxAttempts: 1}),
	)
}

const validURL = "https://ntfy.sh/my-topic"

func TestClientSendRejectsEmptyURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSendSendsBearerAuthHeader(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{URL: validURL, Token: "secret-token"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt.lastRequest.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("expected Bearer auth header, got %q", got)
	}
}

func TestClientSendOmitsAuthHeaderWhenTokenEmpty(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{URL: validURL}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt.lastRequest.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no Authorization header, got %q", got)
	}
}

func TestClientSendRejectsMissingMessageField(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		URL:            validURL,
		Template:       "custom",
		CustomTemplate: `{"foo": "bar"}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "must include a 'message' field") {
		t.Fatalf("expected message-field requirement error, got: %v", err)
	}
}

func TestClientSendRejectsInvalidTemplate(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		URL:            validURL,
		Template:       "custom",
		CustomTemplate: `{{.Unclosed`,
	}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err == nil {
		t.Fatal("expected template parse error")
	}
}

func TestClientSendRejectsNonJSONTemplateOutput(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		URL:            validURL,
		Template:       "custom",
		CustomTemplate: `not json`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON payload") {
		t.Fatalf("expected invalid JSON payload error, got: %v", err)
	}
}

func TestClientSendPropagatesWrapperError(t *testing.T) {
	rt := &capturingRoundTripper{statusCode: http.StatusInternalServerError}
	client := New(Config{URL: validURL}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to send webhook") {
		t.Fatalf("expected wrapped send error, got: %v", err)
	}
}
