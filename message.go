// Package notify provides a generic, provider-agnostic notification
// delivery API: a shared Message type, a uniform Sender interface, and a
// family of provider packages (Discord, Slack, Gotify, Pushover, Ntfy,
// Telegram, generic webhook, and email) that each implement Sender on top
// of a shared, SSRF-safe HTTP transport (see the transport package).
//
// The module never assumes a specific host application. Every point where
// it needs something environment-specific — an HTTP client, a URL
// validator, an SMTP sender, a template — is a constructor-injected
// interface supplied by the caller.
package notify

import "time"

// Message is the generic, provider-agnostic notification payload. Host
// applications map their own domain events into a Message before calling a
// Sender.
type Message struct {
	// Title is a short headline for the notification.
	Title string

	// Body is the human-readable message text.
	Body string

	// EventType is a free-form, host-defined category string. Provider
	// packages treat it as an opaque template field only — never for
	// routing, access-control, or filtering decisions. A host application
	// is free to define its own event-type vocabulary; this module has no
	// opinion on it.
	EventType string

	// Timestamp records when the event occurred. If zero, provider
	// packages default it to time.Now() when the Message is sent.
	Timestamp time.Time

	// Data holds arbitrary structured extras specific to the host
	// application's domain (e.g. hostnames, service counts, IDs). Provider
	// templates expose it as a JSON object via {{toJSON .Data}} or look up
	// individual keys.
	Data map[string]any
}

// Normalized returns a copy of the Message with Timestamp defaulted to
// time.Now() if it is zero. Provider packages call this before building a
// payload so callers never have to remember to set a timestamp themselves.
func (m Message) Normalized() Message {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}
	return m
}
