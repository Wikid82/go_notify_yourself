// Package email implements notify.Sender for email notifications. Unlike
// the HTTP-based provider packages, this module never dials SMTP and never
// renders HTML directly by default — both concerns are isolated behind the
// Mailer and TemplateRenderer interfaces, supplied by the host application.
package email

import (
	"context"
	"fmt"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
)

// Mailer transports an already-composed email. The host application owns
// SMTP configuration, authentication, and connection lifecycle — this
// package never sees credentials.
type Mailer interface {
	Send(ctx context.Context, recipients []string, subject, htmlBody string) error
}

// TemplateRenderer renders an HTML email body for a Message using a
// host-selected template name. Optional: if the host doesn't supply one,
// Client falls back to its own single neutral built-in template.
type TemplateRenderer interface {
	Render(templateName string, msg notify.Message) (htmlBody string, err error)
}

// Config configures an email Sender.
type Config struct {
	// Recipients are the destination email addresses. Empty/whitespace-only
	// entries are ignored; Send errors if none remain.
	Recipients []string

	// SubjectPrefix is prepended to msg.Title to build the email subject.
	// Empty by default — this package has no branding of its own; a host
	// application supplies its own prefix (e.g. "[MyApp Alert] ").
	SubjectPrefix string

	// TemplateName maps a Message to a host-defined template name, passed
	// to Renderer.Render. Optional; nil selects the constant "default".
	TemplateName func(msg notify.Message) string

	// Renderer renders the HTML body. Optional; nil uses this package's
	// built-in neutral, unbranded template.
	Renderer TemplateRenderer

	// Mailer transports the composed email. Required.
	Mailer Mailer
}

// Client dispatches notify.Message values as email.
type Client struct {
	cfg Config
}

var _ notify.Sender = (*Client)(nil)

// New constructs an email Client.
func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

// Send sanitizes msg's Title/Body, renders an HTML body (via cfg.Renderer
// or the built-in default template), and hands the composed email to
// cfg.Mailer.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	if c.cfg.Mailer == nil {
		return fmt.Errorf("email: no Mailer configured")
	}

	recipients := make([]string, 0, len(c.cfg.Recipients))
	for _, r := range c.cfg.Recipients {
		if trimmed := strings.TrimSpace(r); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	if len(recipients) == 0 {
		return fmt.Errorf("email: no recipients configured")
	}

	msg = msg.Normalized()
	msg.Title = sanitizeForEmail(msg.Title)
	msg.Body = sanitizeForEmail(msg.Body)

	templateName := "default"
	if c.cfg.TemplateName != nil {
		templateName = c.cfg.TemplateName(msg)
	}

	renderer := c.cfg.Renderer
	if renderer == nil {
		renderer = defaultTemplateRenderer{}
	}

	htmlBody, err := renderer.Render(templateName, msg)
	if err != nil {
		return fmt.Errorf("email: render template: %w", err)
	}

	subject := c.cfg.SubjectPrefix + msg.Title

	if err := c.cfg.Mailer.Send(ctx, recipients, subject, htmlBody); err != nil {
		return fmt.Errorf("email: send: %w", err)
	}
	return nil
}

// sanitizeForEmail strips ASCII control characters (0x00-0x1F and 0x7F DEL)
// and trims leading/trailing whitespace from untrusted strings before they
// enter the email pipeline. The result is a normalized, single-line-safe
// string. Defense-in-depth alongside whatever CRLF/header-injection
// protections the host's Mailer implementation applies at the SMTP layer.
func sanitizeForEmail(s string) string {
	stripped := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(stripped)
}
