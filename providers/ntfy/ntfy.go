// Package ntfy implements notify.Sender for ntfy.sh (or a self-hosted
// ntfy server).
package ntfy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// Config configures an ntfy Sender.
type Config struct {
	// URL is the ntfy topic push endpoint, e.g.
	// https://ntfy.sh/my-topic.
	URL string

	// Token is an optional ntfy access token, sent as an
	// "Authorization: Bearer <token>" header when non-empty.
	Token string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured ntfy topic.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs an ntfy Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// Send renders msg using the configured template and dispatches it to the
// ntfy topic via the shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	url := strings.TrimSpace(c.cfg.URL)
	if url == "" {
		return fmt.Errorf("ntfy topic URL is not configured")
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
		return fmt.Errorf("ntfy payload must include a 'message' field")
	}

	headers := map[string]string{
		"Content-Type": "application/json",
		"User-Agent":   render.DefaultUserAgent,
	}
	if token := strings.TrimSpace(c.cfg.Token); token != "" {
		headers["Authorization"] = "Bearer " + token
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
