// Package webhook implements notify.Sender for generic/custom JSON
// webhooks — arbitrary destinations with no provider-specific payload
// shape or host allowlist.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// MinimalTemplate and DetailedTemplate are the module's built-in JSON
// templates, usable directly or via Config.Template = "minimal"/"detailed"
// on any provider package (they all share the same rendering engine).
const (
	MinimalTemplate  = render.MinimalTemplate
	DetailedTemplate = render.DetailedTemplate
)

// Config configures a generic webhook Sender.
type Config struct {
	// URL is the arbitrary destination to POST the rendered JSON payload
	// to. Unlike Discord/Slack, there is no host allowlist — the
	// transport.Wrapper's URLValidator is the only SSRF gate.
	URL string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured generic webhook.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a webhook Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// Send renders msg using the configured template and dispatches the raw
// JSON payload as-is — no provider-specific field requirements — via the
// shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	url := strings.TrimSpace(c.cfg.URL)
	if url == "" {
		return fmt.Errorf("webhook URL is not configured")
	}

	tmplStr := render.SelectTemplate(c.cfg.Template, c.cfg.CustomTemplate, render.MinimalTemplate, render.DetailedTemplate)
	rendered, err := render.Render(tmplStr, render.TemplateData(msg))
	if err != nil {
		return err
	}

	if !json.Valid([]byte(rendered)) {
		return fmt.Errorf("invalid JSON payload")
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL: url,
		Headers: map[string]string{
			"Content-Type": "application/json",
			"User-Agent":   render.DefaultUserAgent,
		},
		Body: []byte(rendered),
	}); err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}

	return nil
}
