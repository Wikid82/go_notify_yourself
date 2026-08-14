// Package slack implements notify.Sender for Slack Incoming Webhooks.
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// Config configures a Slack Incoming Webhook Sender.
type Config struct {
	// WebhookURL is the Slack Incoming Webhook URL, e.g.
	// https://hooks.slack.com/services/T.../B.../xxx.
	WebhookURL string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured Slack webhook.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a Slack Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

var webhookURLRegex = regexp.MustCompile(`^https://hooks\.slack\.com/services/T[A-Za-z0-9_-]+/B[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$`)

// ValidateWebhookURL checks that rawURL matches Slack's Incoming Webhook
// URL shape: https://hooks.slack.com/services/T.../B.../xxx.
func ValidateWebhookURL(rawURL string) error {
	if !webhookURLRegex.MatchString(rawURL) {
		return fmt.Errorf("invalid Slack webhook URL: must match https://hooks.slack.com/services/T.../B.../xxx")
	}
	return nil
}

// Send renders msg using the configured template, normalizes the payload
// to Slack's expected shape (text/blocks), and dispatches it via the
// shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	webhookURL := strings.TrimSpace(c.cfg.WebhookURL)
	if webhookURL == "" {
		return fmt.Errorf("slack webhook URL is not configured")
	}
	if err := ValidateWebhookURL(webhookURL); err != nil {
		return err
	}

	tmplStr := render.SelectTemplate(c.cfg.Template, c.cfg.CustomTemplate, render.MinimalTemplate, render.DetailedTemplate)
	rendered, err := render.Render(tmplStr, render.TemplateData(msg))
	if err != nil {
		return err
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(rendered), &payload); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	// Slack requires either 'text' or 'blocks'; fall back to the generic
	// 'message' field if neither is present.
	if _, hasText := payload["text"]; !hasText {
		if _, hasBlocks := payload["blocks"]; !hasBlocks {
			messageValue, hasMessage := payload["message"]
			if !hasMessage {
				return fmt.Errorf("slack payload requires 'text' or 'blocks' field")
			}
			payload["text"] = messageValue
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to normalize slack payload: %w", err)
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL: webhookURL,
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
