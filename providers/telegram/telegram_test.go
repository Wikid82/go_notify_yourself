package telegram

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

func TestClientSendBuildsDispatchURLFromBotToken(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{BotToken: "12345:ABCDEF", ChatID: "999"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://api.telegram.org/bot12345:ABCDEF/sendMessage"
	if rt.lastRequest.URL.String() != want {
		t.Fatalf("unexpected dispatch URL: got %s want %s", rt.lastRequest.URL.String(), want)
	}
}

func TestClientSendUsesCustomBaseURL(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{BotToken: "tok", ChatID: "1", BaseURL: "https://telegram.test.invalid"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://telegram.test.invalid/bottok/sendMessage"
	if rt.lastRequest.URL.String() != want {
		t.Fatalf("unexpected dispatch URL: got %s want %s", rt.lastRequest.URL.String(), want)
	}
}

func TestClientSendInjectsChatID(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{BotToken: "tok", ChatID: "chat-42"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if payload["chat_id"] != "chat-42" {
		t.Fatalf("expected chat_id injected, got %#v", payload["chat_id"])
	}
}

func TestClientSendMinimalTemplateFallsBackToText(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{BotToken: "tok", ChatID: "1"}, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello telegram"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(rt.lastBody, &payload)
	if payload["text"] != "hello telegram" {
		t.Fatalf("expected text fallback from message field, got %#v", payload["text"])
	}
}

func TestClientSendRejectsPayloadWithoutTextOrMessage(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		BotToken:       "tok",
		ChatID:         "1",
		Template:       "custom",
		CustomTemplate: `{"foo": "bar"}`,
	}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "requires 'text' field") {
		t.Fatalf("expected text-field requirement error, got: %v", err)
	}
}

func TestClientSendRejectsInvalidTemplate(t *testing.T) {
	rt := &capturingRoundTripper{}
	client := New(Config{
		BotToken:       "tok",
		ChatID:         "1",
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
		BotToken:       "tok",
		ChatID:         "1",
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
	client := New(Config{BotToken: "tok", ChatID: "1"}, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to send webhook") {
		t.Fatalf("expected wrapped send error, got: %v", err)
	}
}
