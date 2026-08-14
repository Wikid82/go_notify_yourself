package discord

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

// capturingRoundTripper fabricates an HTTP response without ever dialing a
// real connection, capturing the outbound request for assertions. Every
// provider package's tests use this same pattern to exercise Send's full
// payload-building + dispatch path without a real network.
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

func passthroughValidator(rawURL string, _ bool) (string, error) {
	return rawURL, nil
}

func newTestWrapper(rt *capturingRoundTripper) *transport.Wrapper {
	return transport.NewWrapper(
		transport.WithURLValidator(passthroughValidator),
		transport.WithClientFactory(func(bool, int) *http.Client {
			return &http.Client{Transport: rt}
		}),
		transport.WithRetryPolicy(transport.RetryPolicy{MaxAttempts: 1}),
	)
}

const validWebhookURL = "https://discord.com/api/webhooks/123456789/abcDEF_token-123"

func TestNormalizeURL(t *testing.T) {
	got := NormalizeURL(validWebhookURL)
	if got != "discord://abcDEF_token-123@123456789" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}

	unchanged := NormalizeURL("https://example.com/not-a-webhook")
	if unchanged != "https://example.com/not-a-webhook" {
		t.Fatalf("expected non-matching URL unchanged, got %q", unchanged)
	}
}

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{"valid discord.com", "https://discord.com/api/webhooks/1/abc", false},
		{"valid canary", "https://canary.discord.com/api/webhooks/1/abc", false},
		{"discord shorthand scheme", "discord://token@123", false},
		{"invalid parse", "https://[::1", true},
		{"wrong scheme", "http://discord.com/api/webhooks/1/abc", true},
		{"missing hostname", "https:///api/webhooks/1/abc", true},
		{"ip literal host", "https://1.2.3.4/api/webhooks/1/abc", true},
		{"disallowed host", "https://evil.example.com/api/webhooks/1/abc", true},
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

func TestIsValidRedirectURL(t *testing.T) {
	if !isValidRedirectURL("https://discord.com/hook") {
		t.Fatal("expected valid https URL to pass")
	}
	if isValidRedirectURL("ftp://discord.com/hook") {
		t.Fatal("expected non-http(s) scheme to fail")
	}
	if isValidRedirectURL("https:///hook") {
		t.Fatal("expected missing hostname to fail")
	}
	if isValidRedirectURL("://bad") {
		t.Fatal("expected unparsable URL to fail")
	}
}

func TestClientSendMinimalTemplateFallsBackToContent(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{WebhookURL: validWebhookURL}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Title: "hi", Body: "hello world", EventType: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rt.lastBody, &payload); err != nil {
		t.Fatalf("failed to unmarshal dispatched payload: %v", err)
	}
	if payload["content"] != "hello world" {
		t.Fatalf("expected content fallback from message field, got %#v", payload["content"])
	}
	if rt.lastRequest.Header.Get("User-Agent") == "" {
		t.Fatal("expected a User-Agent header to be sent")
	}
}

func TestClientSendCustomTemplateWithContentField(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
		Template:       "custom",
		CustomTemplate: `{"content": {{toJSON .Message}}}`,
	}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "custom body"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if payload["content"] != "custom body" {
		t.Fatalf("expected explicit content field preserved, got %#v", payload["content"])
	}
}

func TestClientSendRejectsPayloadWithoutContentEmbedsOrMessage(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
		Template:       "custom",
		CustomTemplate: `{"foo": "bar"}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "requires 'content' or 'embeds'") {
		t.Fatalf("expected content/embeds requirement error, got: %v", err)
	}
}

func TestClientSendRejectsInvalidWebhookURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{WebhookURL: "https://evil.example.com/hook"}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil {
		t.Fatal("expected invalid webhook URL to be rejected")
	}
	if rt.lastRequest != nil {
		t.Fatal("expected no dispatch for an invalid webhook URL")
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

func TestClientSendDetailedTemplateWithEmbeds(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		WebhookURL:     validWebhookURL,
		Template:       "custom",
		CustomTemplate: `{"embeds": [{"title": {{toJSON .Title}}}]}`,
	}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Title: "embed title"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if _, ok := payload["embeds"]; !ok {
		t.Fatal("expected embeds field to be preserved")
	}
}
