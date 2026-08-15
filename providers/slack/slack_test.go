package slack

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

// Deliberately not shaped like a real Slack token (dashes, "FAKE" markers)
// so it doesn't trip GitHub's secret-scanning push protection.
const validWebhookURL = "https://hooks.slack.com/services/T-FAKE-TEST/B-FAKE-TEST/FAKE-NOT-A-REAL-SECRET"

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"valid", validWebhookURL, false},
		{"wrong host", "https://example.com/services/T1/B1/abc", true},
		{"wrong scheme", "http://hooks.slack.com/services/T1/B1/abc", true},
		{"missing segments", "https://hooks.slack.com/services/T1/B1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebhookURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateWebhookURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
			}
		})
	}
}

func TestClientSendRejectsEmptyWebhookURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSendRejectsInvalidWebhookURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{WebhookURL: "https://example.com/not-slack"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err == nil {
		t.Fatal("expected invalid webhook URL rejection")
	}
}

func TestClientSendMinimalTemplateFallsBackToText(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{WebhookURL: validWebhookURL}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello slack"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if payload["text"] != "hello slack" {
		t.Fatalf("expected text fallback from message field, got %#v", payload["text"])
	}
}

func TestClientSendPreservesExplicitBlocks(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
		Template:       "custom",
		CustomTemplate: `{"blocks": [{"type": "section"}]}`,
	}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if _, ok := payload["blocks"]; !ok {
		t.Fatal("expected blocks field to be preserved")
	}
}

func TestClientSendRejectsPayloadWithoutTextBlocksOrMessage(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
		Template:       "custom",
		CustomTemplate: `{"foo": "bar"}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "requires 'text' or 'blocks'") {
		t.Fatalf("expected text/blocks requirement error, got: %v", err)
	}
}

func TestClientSendRejectsInvalidTemplate(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
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
		WebhookURL:     validWebhookURL,
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
	client := New(Config{WebhookURL: validWebhookURL}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to send webhook") {
		t.Fatalf("expected wrapped send error, got: %v", err)
	}
}
