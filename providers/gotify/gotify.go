// Package gotify implements notify.Sender for Gotify push servers.
package gotify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// Config configures a Gotify Sender.
type Config struct {
	// URL is the Gotify server's message push endpoint, e.g.
	// https://gotify.example.com/message.
	URL string

	// Token is the Gotify application token, sent as the X-Gotify-Key
	// header when non-empty.
	Token string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured Gotify server.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a Gotify Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// Send renders msg using the configured template and dispatches it to the
// Gotify server via the shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	url := strings.TrimSpace(c.cfg.URL)
	if url == "" {
		return fmt.Errorf("gotify server URL is not configured")
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

	if _, hasMessage := payload["message"]; !hasMessage {
		return fmt.Errorf("gotify payload requires 'message' field")
	}

	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   render.DefaultUserAgent,
	}
	if token := strings.TrimSpace(c.cfg.Token); token != "" {
		headers["X-Gotify-Key"] = token
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL:     url,
		Headers: headers,
		Body:    []byte(rendered),
	}); err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}

	return nil
}
