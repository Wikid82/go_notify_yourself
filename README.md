# go_notify_yourself

A standalone, dependency-free Go module for notification delivery: SSRF-safe outbound HTTP
dispatch with retries, and a common `Sender` interface across Discord, Slack, Gotify, Pushover,
Ntfy, Telegram, generic webhooks, and email.

```
go get github.com/Wikid82/go_notify_yourself
```

## Why

Most projects that need to fire off a Discord/Slack/email alert end up re-implementing the same
things badly: no retry on transient failures, no SSRF protection on the outbound URL, ad hoc JSON
templating, and provider-specific quirks (auth headers vs. query params vs. URL path segments)
scattered across the codebase. This module packages that once, behind a small, uniform API, so a
project can wire up outbound notifications without re-solving delivery mechanics from scratch.

**Design principle**: this module never assumes anything about your environment. It has no
database, no HTTP framework, no config system of its own. Every environment-specific concern (the
HTTP client, SSRF policy, SMTP transport, HTML templates) is a constructor-injected interface you
supply — see [Bringing your own SSRF policy](#bringing-your-own-ssrf-policy) below.

## Quick start

```go
package main

import (
	"context"
	"log"

	"github.com/Wikid82/go_notify_yourself/providers/discord"
	"github.com/Wikid82/go_notify_yourself/transport"

	notify "github.com/Wikid82/go_notify_yourself"
)

func main() {
	// One shared transport.Wrapper per process is typical — every provider
	// package dispatches through it.
	wrapper := transport.NewWrapper(transport.WithAllowHTTP(false))

	sender := discord.New(discord.Config{
		WebhookURL: "https://discord.com/api/webhooks/123456789/abcDEF",
	}, wrapper)

	err := sender.Send(context.Background(), notify.Message{
		Title:     "Disk usage high",
		Body:      "Volume /data is at 92% capacity.",
		EventType: "disk_usage",
		Data:      map[string]any{"Host": "db-01", "Percent": 92},
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

Every provider package follows the same shape: a `Config` struct, `New(cfg, wrapper) *Client`, and
`(*Client).Send(ctx, notify.Message) error` — implementing the shared `notify.Sender` interface, so
a caller that fans a notification out to several configured destinations can treat them uniformly:

```go
senders := []notify.Sender{discordSender, slackSender, webhookSender}
for _, s := range senders {
	if err := s.Send(ctx, msg); err != nil {
		log.Printf("notify: %v", err)
	}
}
```

## The `notify.Message` type

```go
type Message struct {
	Title     string         // short headline
	Body      string         // human-readable message text
	EventType string         // free-form, host-defined category (opaque to this module)
	Timestamp time.Time      // defaults to time.Now() if zero
	Data      map[string]any // arbitrary structured extras, exposed to templates as {{toJSON .Data}}
}
```

`Data` is where your application's own domain fields go (hostnames, IDs, counts, whatever) — this
module has no opinion on your event vocabulary.

## Provider packages

| Package | Config fields (beyond `Template`/`CustomTemplate`) | Notes |
|---|---|---|
| `providers/discord` | `WebhookURL` | Validates the URL is a real `discord.com`/`canary.discord.com` webhook. Normalizes payload to `content`/`embeds`. |
| `providers/slack` | `WebhookURL` | Validates against Slack's `hooks.slack.com/services/T.../B.../...` shape. Normalizes payload to `text`/`blocks`. |
| `providers/gotify` | `URL`, `Token` | Sends `Token` as `X-Gotify-Key`. |
| `providers/pushover` | `UserKey`, `APIToken`, `BaseURL` (optional override) | Injects `token`/`user` into the payload. Rejects emergency priority (2) — not yet supported. |
| `providers/ntfy` | `URL`, `Token` | Sends `Token` as `Authorization: Bearer <token>`. |
| `providers/telegram` | `BotToken`, `ChatID`, `BaseURL` (optional override) | Bot token is embedded in the dispatch URL path per Telegram's own API convention; injects `chat_id`. |
| `providers/webhook` | `URL` | Generic/custom JSON dispatch — no destination allowlist, no payload field requirements. Also exposes `RenderPreview` for validating a custom template without dispatching. |
| `providers/email` | see below | The one provider not built on `transport.Wrapper` — see [Email](#email). |

Every HTTP-based provider's `Config.Template` selects the JSON payload shape: `"minimal"` (default),
`"detailed"`, or `"custom"` (uses `Config.CustomTemplate`, a Go `text/template` string with a
`toJSON` helper function available, e.g. `{{toJSON .Message}}`).

## Transport: SSRF-safe dispatch with retries

`transport.Wrapper` is the shared delivery primitive every HTTP-based provider package dispatches
through:

- Retries on `429`/`5xx`/transient network errors with exponential backoff + jitter (configurable
  via `RetryPolicy`).
- Validates the destination URL (and every redirect target) through a pluggable `URLValidator`.
- Caps request bodies at 256 KiB and response bodies at 1 MiB.
- Allowlists outbound headers (`Content-Type`, `User-Agent`, `X-Request-ID`, `X-Gotify-Key`,
  `Authorization`) — anything else you pass in `Request.Headers` is silently dropped.
- Rejects destination URLs carrying credentials, fragments, or common auth-looking query
  parameters (`token`, `auth`, `apikey`, `api_key`).

```go
wrapper := transport.NewWrapper(
	transport.WithAllowHTTP(false),          // reject plain HTTP (and loopback) destinations
	transport.WithRetryPolicy(transport.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}),
)
```

### Bringing your own SSRF policy

With no options, `NewWrapper()` uses a conservative, dependency-free built-in `DefaultURLValidator`
(rejects non-HTTPS unless `allowHTTP`, rejects IP literals and DNS-resolved addresses in RFC 1918 /
loopback / link-local / other reserved ranges) and a plain `*http.Client` with no additional
hardening. That's enough to be useful standalone, but a production host with its own SSRF
infrastructure (DNS-rebinding protection at dial time, a shared IP-blocklist, etc.) should override
both:

```go
wrapper := transport.NewWrapper(
	transport.WithClientFactory(func(allowHTTP bool, maxRedirects int) *http.Client {
		return myapp.NewSafeHTTPClient(allowHTTP, maxRedirects) // your own hardened client
	}),
	transport.WithURLValidator(func(rawURL string, allowHTTP bool) (string, error) {
		return myapp.ValidateExternalURL(rawURL, allowHTTP) // your own SSRF policy
	}),
)
```

## Email

Unlike the HTTP providers, `providers/email` never dials SMTP and never renders HTML by default —
both are host-supplied interfaces, since this module has no opinion on your mail transport or
branding:

```go
type Mailer interface {
	Send(ctx context.Context, recipients []string, subject, htmlBody string) error
}

type TemplateRenderer interface {
	Render(templateName string, msg notify.Message) (htmlBody string, err error)
}
```

```go
sender := email.New(email.Config{
	Recipients:    []string{"ops@example.com"},
	SubjectPrefix: "[MyApp] ", // "" by default — no branding baked in
	Mailer:        myMailer,   // required: your SMTP wrapper
	// Renderer and TemplateName are optional — omit them to use the
	// built-in neutral, unbranded HTML template.
})
```

If you don't supply a `Renderer`, `Send` renders one neutral, inline-styled HTML template (no logo,
no product name) — good enough to get working email out of a fresh integration with zero config.
Supply your own `Renderer`/`TemplateName` to plug in branded, event-differentiated templates.

## Testing your own integration

Every provider's `Config` takes a `*transport.Wrapper`, and `transport.NewWrapper` takes a
`ClientFactory` — so tests can inject a fake `http.RoundTripper` that fabricates responses without
touching the network, and a passthrough `URLValidator` that skips SSRF checks against a local test
server. See any `providers/*/*_test.go` file in this repo for the pattern.

## Project status

Extracted from [Charon](https://github.com/Wikid82/charon)'s internal notification engine. The
provider list is intentionally exactly these seven HTTP providers plus email — see
`docs/plans/notifications_extraction_spec.md` in Charon's repo for the extraction design brief.
Long-term direction is an [Apprise](https://github.com/caronc/apprise)-style common interface over a
larger provider catalog; the `Sender` interface and per-package structure here are deliberately
shaped so that's additive later, not a breaking rework.

## License

MIT — see [LICENSE](./LICENSE).
