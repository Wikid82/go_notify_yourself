// Package discord implements notify.Sender for Discord webhooks.
package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	neturl "net/url"
	"regexp"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// Config configures a Discord webhook Sender.
type Config struct {
	// WebhookURL is the Discord webhook destination, e.g.
	// https://discord.com/api/webhooks/<id>/<token>.
	WebhookURL string

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate).
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to a configured Discord webhook.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a Discord Client. w performs the actual dispatch — see
// transport.NewWrapper. Unlike Charon's original implementation (where
// Discord dispatch bypassed the SSRF-safe wrapper entirely), Send always
// goes through w, gaining retry/backoff it previously lacked.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

var webhookURLRegex = regexp.MustCompile(`^https://discord(?:app)?\.com/api/webhooks/(\d+)/([a-zA-Z0-9_-]+)`)

var allowedWebhookHosts = map[string]struct{}{
	"discord.com":        {},
	"canary.discord.com": {},
}

// NormalizeURL converts a standard HTTPS Discord webhook URL into its
// discord://token@id shorthand form. URLs that don't match the expected
// shape are returned unchanged. This is a pure convenience/display helper —
// validation and dispatch both operate on the caller-supplied URL as-is.
func NormalizeURL(rawURL string) string {
	matches := webhookURLRegex.FindStringSubmatch(rawURL)
	if len(matches) == 3 {
		id, token := matches[1], matches[2]
		return fmt.Sprintf("discord://%s@%s", token, id)
	}
	return rawURL
}

// ValidateWebhookURL checks that rawURL is a well-formed Discord webhook
// destination: either the discord:// shorthand, or an HTTPS URL whose host
// is discord.com or canary.discord.com — never an IP literal.
func ValidateWebhookURL(rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid Discord webhook URL: failed to parse URL; use the HTTPS webhook URL provided by Discord")
	}

	if strings.EqualFold(parsed.Scheme, "discord") {
		return nil
	}

	if !strings.EqualFold(parsed.Scheme, "https") {
		return fmt.Errorf("invalid Discord webhook URL: URL must use HTTPS and the hostname URL provided by Discord")
	}

	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return fmt.Errorf("invalid Discord webhook URL: missing hostname; use the HTTPS webhook URL provided by Discord")
	}

	if net.ParseIP(hostname) != nil {
		return fmt.Errorf("invalid Discord webhook URL: IP address hosts are not allowed; use the hostname URL provided by Discord (discord.com or canary.discord.com)")
	}

	if _, ok := allowedWebhookHosts[hostname]; !ok {
		return fmt.Errorf("invalid Discord webhook URL: host must be discord.com or canary.discord.com; use the hostname URL provided by Discord")
	}

	return nil
}

// isValidRedirectURL is a generic http(s)-with-hostname sanity check
// applied to the webhook URL before dispatch (its only call site in the
// ported original was Discord dispatch).
func isValidRedirectURL(rawURL string) bool {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Hostname() != ""
}

// Send renders msg using the configured template, normalizes the payload
// to Discord's expected shape (content/embeds), and dispatches it via the
// shared transport.Wrapper.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	if err := ValidateWebhookURL(c.cfg.WebhookURL); err != nil {
		return err
	}
	if !isValidRedirectURL(c.cfg.WebhookURL) {
		return fmt.Errorf("invalid webhook url")
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

	// Discord requires either 'content' or 'embeds'; fall back to the
	// generic 'message' field if neither is present.
	if _, hasContent := payload["content"]; !hasContent {
		if _, hasEmbeds := payload["embeds"]; !hasEmbeds {
			messageValue, hasMessage := payload["message"]
			if !hasMessage {
				return fmt.Errorf("discord payload requires 'content' or 'embeds' field")
			}
			payload["content"] = messageValue
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to normalize discord payload: %w", err)
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL: c.cfg.WebhookURL,
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
