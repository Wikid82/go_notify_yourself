// Package telegram implements notify.Sender for Telegram bots.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// defaultBaseURL is the Telegram Bot API's production base.
const defaultBaseURL = "https://api.telegram.org"

// Config configures a Telegram Sender.
type Config struct {
	// BotToken is the Telegram bot token, embedded directly in the
	// dispatch URL path per the Telegram Bot API's own convention (not
	// sent as a header).
	BotToken string

	// ChatID is the destination chat ID, sent as the payload's "chat_id"
	// field.
	ChatID string

	// BaseURL overrides the Telegram Bot API base (default
	// https://api.telegram.org). Intended for tests; production callers
	// should leave this empty.
	BaseURL string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured Telegram bot/chat.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a Telegram Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// dispatchURL builds the Telegram sendMessage endpoint URL for the
// configured bot token and (optionally overridden) API base.
func (c *Client) dispatchURL() (string, error) {
	base := strings.TrimSpace(c.cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	dispatchURL := base + "/bot" + c.cfg.BotToken + "/sendMessage"
	if _, err := neturl.Parse(dispatchURL); err != nil {
		return "", fmt.Errorf("telegram dispatch URL validation failed: invalid hostname")
	}
	return dispatchURL, nil
}

// Send renders msg using the configured template, normalizes the payload
// to Telegram's expected shape (text, chat_id), and dispatches it via the
// shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	tmplStr := render.SelectTemplate(c.cfg.Template, c.cfg.CustomTemplate, render.MinimalTemplate, render.DetailedTemplate)
	rendered, err := render.Render(tmplStr, render.TemplateData(msg))
	if err != nil {
		return err
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rendered), &payload); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	// Telegram requires a 'text' field; fall back to the generic 'message'
	// field if neither is present.
	if _, hasText := payload["text"]; !hasText {
		messageValue, hasMessage := payload["message"]
		if !hasMessage {
			return fmt.Errorf("telegram payload requires 'text' field")
		}
		payload["text"] = messageValue
	}

	dispatchURL, err := c.dispatchURL()
	if err != nil {
		return err
	}

	payload["chat_id"] = c.cfg.ChatID

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload with chat_id: %w", err)
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL: dispatchURL,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   render.DefaultUserAgent,
		},
		Body: body,
	}); err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}

	return nil
}
