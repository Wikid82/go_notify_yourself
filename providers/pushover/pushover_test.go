package pushover

import (
	"context"
	"encoding/json"
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

func TestClientSendRejectsMissingToken(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{UserKey: "user"}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "API token is not configured") {
		t.Fatalf("expected missing-token error, got: %v", err)
	}
}

func TestClientSendRejectsMissingUserKey(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{APIToken: "tok"}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "user key is not configured") {
		t.Fatalf("expected missing-user-key error, got: %v", err)
	}
}

func TestClientSendInjectsTokenAndUser(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{APIToken: "tok", UserKey: "user"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if payload["token"] != "tok" || payload["user"] != "user" {
		t.Fatalf("expected token/user injected, got %#v", payload)
	}
	if rt.lastRequest.URL.String() != "https://api.pushover.net/1/messages.json" {
		t.Fatalf("unexpected dispatch URL: %s", rt.lastRequest.URL.String())
	}
}

func TestClientSendUsesCustomBaseURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{APIToken: "tok", UserKey: "user", BaseURL: "https://pushover.test.invalid"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.lastRequest.URL.String() != "https://pushover.test.invalid/1/messages.json" {
		t.Fatalf("unexpected dispatch URL: %s", rt.lastRequest.URL.String())
	}
}

func TestClientSendRejectsMissingMessageField(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		APIToken:       "tok",
		UserKey:        "user",
		Template:       "custom",
		CustomTemplate: `{"foo": "bar"}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "requires 'message' field") {
		t.Fatalf("expected message-field requirement error, got: %v", err)
	}
}

func TestClientSendRejectsEmergencyPriority(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		APIToken:       "tok",
		UserKey:        "user",
		Template:       "custom",
		CustomTemplate: `{"message": "x", "priority": 2}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "emergency priority") {
		t.Fatalf("expected emergency priority rejection, got: %v", err)
	}
}

func TestClientSendAllowsNonEmergencyPriority(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		APIToken:       "tok",
		UserKey:        "user",
		Template:       "custom",
		CustomTemplate: `{"message": "x", "priority": 1}`,
	}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientSendRejectsInvalidTemplate(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		APIToken:       "tok",
		UserKey:        "user",
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
		APIToken:       "tok",
		UserKey:        "user",
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
	client := New(Config{APIToken: "tok", UserKey: "user"}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to send webhook") {
		t.Fatalf("expected wrapped send error, got: %v", err)
	}
}
