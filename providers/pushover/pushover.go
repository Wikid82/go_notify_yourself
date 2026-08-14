// Package pushover implements notify.Sender for Pushover.
package pushover

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

// defaultBaseURL is Pushover's production API base.
const defaultBaseURL = "https://api.pushover.net"

// Config configures a Pushover Sender.
type Config struct {
	// UserKey is the Pushover user (or group) key that identifies the
	// notification recipient.
	UserKey string

	// APIToken is the Pushover application API token.
	APIToken string

	// BaseURL overrides Pushover's API base (default
	// https://api.pushover.net). Intended for tests; production callers
	// should leave this empty.
	BaseURL string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to Pushover.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a Pushover Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// dispatchURL builds the Pushover messages endpoint URL from the
// configured (or default) base URL.
func (c *Client) dispatchURL() (string, error) {
	base := strings.TrimSpace(c.cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	dispatchURL := base + "/1/messages.json"
	if _, err := neturl.Parse(dispatchURL); err != nil {
		return "", fmt.Errorf("pushover dispatch URL validation failed: invalid hostname")
	}
	return dispatchURL, nil
}

// Send renders msg using the configured template, validates and augments
// the payload with Pushover's required token/user fields, and dispatches
// it via the shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	apiToken := strings.TrimSpace(c.cfg.APIToken)
	if apiToken == "" {
		return fmt.Errorf("pushover API token is not configured")
	}
	userKey := strings.TrimSpace(c.cfg.UserKey)
	if userKey == "" {
		return fmt.Errorf("pushover user key is not configured")
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
		return fmt.Errorf("pushover payload requires 'message' field")
	}
	if priority, ok := payload["priority"]; ok {
		if p, isFloat := priority.(float64); isFloat && p == 2 {
			return fmt.Errorf("pushover emergency priority (2) requires retry and expire parameters; not yet supported")
		}
	}

	dispatchURL, err := c.dispatchURL()
	if err != nil {
		return err
	}

	payload["token"] = apiToken
	payload["user"] = userKey

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal pushover payload: %w", err)
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
