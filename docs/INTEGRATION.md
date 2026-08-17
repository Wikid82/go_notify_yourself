# Integrating `go_notify_yourself` into your project

This guide is for someone bringing this module into their **own, unrelated** project — not for
working on this module itself (see [ARCHITECTURE.md](../ARCHITECTURE.md) for that) and not
specific to Charon, the project this module was originally extracted from.

## Who this is for

A Go developer building a self-hosted or small-team application who needs to fire outbound alerts
to chat/push/email destinations and doesn't want to hand-roll SSRF-safe HTTP dispatch, retries, or
per-service payload quirks. As the README puts it: most projects that need this end up
re-implementing the same things badly.

**This is explicitly not for you if:**

- You need an Apprise-style URL-scheme dispatcher (`discord://...`) today — not yet implemented
  (see the README's "Project status" section for the long-term direction).
- You need inbound or two-way messaging — this module is send-only.

## What it does / doesn't do

**Does:**

- SSRF-safe outbound HTTP dispatch with retry/backoff (`transport.Wrapper`) — destination
  validation, redirect re-validation, request/response size caps.
- A uniform `Sender` interface across eight built-in provider types: Discord, Slack, Gotify,
  Pushover, Ntfy, Telegram, generic webhook, and email.
- JSON payload templating with a shared `text/template` engine plus a `toJSON` helper.
- A self-registering factory/discovery layer (`notify.Register`/`notify.New`/
  `notify.RegisteredTypes`) for constructing a `Sender` by name at runtime.

**Doesn't:**

- Own any database, config file format, or HTTP framework.
- Provide inbound webhook receiving.
- Persist retries across a process restart — retries are in-process, in-request only.
- Provide a URL-scheme dispatch convention (yet).

## When to reach for it vs. rolling your own

**Reach for it if:**

- You need two or more of {Discord, Slack, Gotify, Pushover, Ntfy, Telegram, generic webhook,
  email} dispatch.
- You want retry/backoff and SSRF hardening without writing it yourself.
- You're fine supplying your own HTTP client factory / SSRF policy / SMTP mailer via the module's
  dependency-injection seams (see "Why it's built this way" below).

**Roll your own if:**

- You need exactly one destination type with a very custom payload shape and don't want any of the
  shared machinery.
- You need transport types this module doesn't have (SMS, inbound webhooks, message queues).

## Where it fits in a typical app's architecture

```
 domain event                     startup (once)
      │                                 │
      ▼                                 ▼
 build a notify.Message      build one shared *transport.Wrapper
      │                      (+ Mailer/TemplateRenderer if using email)
      │                                 │
      └──────────────┬──────────────────┘
                      ▼
     construct Sender(s): typed New(cfg, wrapper) directly,
     OR notify.New(providerType, config) via providers/all
                      │
                      ▼
              sender.Send(ctx, msg)
                      │
                      ▼
        called from wherever your app fires alerts today
        (a notification service, an error handler, a monitor loop)
```

This module is a **dispatch layer your service layer calls into** — it does not own your request
lifecycle, your background job scheduler, or your config file format.

## Why it's built this way

The module has zero database/framework/HTTP-server knowledge by design: every environment-specific
concern is a constructor-injected interface (`transport.ClientFactory`, `transport.URLValidator`,
`email.Mailer`, `email.TemplateRenderer`). This is what makes it equally usable from a Gin+GORM web
app, a CLI tool, or a serverless function — nothing in the module assumes any of those. It's also
why the provider registry (`notify.New`) accepts a generic `map[string]any` rather than forcing a
specific config-file format, struct tag convention, or framework binding: the registry boundary has
to work for both plain-data providers (Discord's `WebhookURL` string) and providers whose config
carries actual Go values a host constructs at startup (email's `Mailer`/`TemplateRenderer`
interfaces) — see [ARCHITECTURE.md](../ARCHITECTURE.md) for the full rationale.

## How to integrate — end-to-end walkthrough

**1. Get the module:**

```sh
go get github.com/Wikid82/go_notify_yourself@v0.2.0
```

**2. Register the providers you want.** Either blank-import everything for zero-touch discovery:

```go
import _ "github.com/Wikid82/go_notify_yourself/providers/all"
```

or hand-pick individual providers if you'd rather control exactly what's linked into your binary
(smaller binary, no unused transitive dependencies):

```go
import (
	_ "github.com/Wikid82/go_notify_yourself/providers/discord"
	_ "github.com/Wikid82/go_notify_yourself/providers/email"
)
```

Both are equally supported; neither requires a different `notify.Register`/`New` API. Pick
`providers/all` if you want a new provider to become available automatically after a `go get`
version bump with no code change on your side; hand-pick if you'd rather that be a deliberate,
reviewed decision.

**3. Build a shared `transport.Wrapper` once, at startup:**

```go
wrapper := transport.NewWrapper(
	transport.WithAllowHTTP(false),
	// transport.WithClientFactory / transport.WithURLValidator: supply your own
	// SSRF-hardened HTTP client and destination-validation policy in production —
	// see the README's "Bringing your own SSRF policy" section.
)
```

**4. Construct a sender.** Via `notify.New` for a runtime-determined provider type (e.g. one row of
provider config loaded from your database):

```go
sender, err := notify.New("discord", map[string]any{
	"transport":   wrapper,
	"webhook_url": "https://discord.com/api/webhooks/123456789/abcDEF",
	"template":    "minimal",
})
if err != nil {
	log.Fatalf("unsupported or misconfigured provider: %v", err)
}
```

Email needs its `Mailer` (your own SMTP wrapper) constructed and passed directly — it isn't
plain data:

```go
sender, err := notify.New("email", map[string]any{
	"mailer":         myMailer,                  // implements email.Mailer
	"recipients":     []string{"ops@example.com"},
	"subject_prefix": "[MyApp] ",
})
```

Or skip the registry and call the typed constructor directly when you already know the provider
type at compile time — this keeps full compile-time type safety and is the recommended path when
you don't need runtime discovery:

```go
sender := discord.New(discord.Config{WebhookURL: "https://discord.com/api/webhooks/..."}, wrapper)
```

**5. Dispatch a message:**

```go
err = sender.Send(ctx, notify.Message{
	Title:     "Disk usage high",
	Body:      "Volume /data is at 92% capacity.",
	EventType: "disk_usage",
	Data:      map[string]any{"Host": "db-01", "Percent": 92},
})
```

**6. Handle/log the error.** `Send` never panics; a failure (bad destination, provider rejected the
payload, network error after retries) is always a returned `error`:

```go
if err != nil {
	log.Printf("notify: failed to send to discord: %v", err)
	// decide your own policy: retry later, alert on a fallback channel, just log — this
	// module makes no decision for you beyond in-request retry/backoff.
}
```

**Testing your integration:** see the README's ["Testing your own
integration"](../README.md#testing-your-own-integration) section for how to inject a fake HTTP
round-tripper and a passthrough URL validator so your tests never touch the network.
