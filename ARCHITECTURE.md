# Architecture

This document describes how `go_notify_yourself` is put together, with enough precision that a
human contributor **or a coding agent** can add a new provider correctly without guessing. For a
usage-focused quick-start, see [README.md](./README.md). For integrating this module into your own
project end-to-end, see [docs/INTEGRATION.md](./docs/INTEGRATION.md).

## 1. Module overview

`go_notify_yourself` is a standalone, dependency-free Go module for outbound notification delivery.
It has four layers:

1. **Root package (`notify`)** — `Message` (the generic, provider-agnostic payload), `Sender` (the
   uniform dispatch interface every provider implements), and the provider **registry**
   (`Register`/`New`/`RegisteredTypes` in `factory.go`) that ties everything below together.
2. **`transport`** — the shared, SSRF-safe outbound HTTP primitive (`transport.Wrapper`) that every
   HTTP-based provider dispatches through: destination validation, retry/backoff, redirect
   re-validation, and request/response size caps.
3. **`providers/*`** — one package per notification service (`discord`, `slack`, `gotify`,
   `pushover`, `ntfy`, `telegram`, `webhook`, `email`), each exposing a typed `Config` struct, a
   `New(...)` constructor, and a `Client` implementing `notify.Sender`.
4. **`providers/all`** — a blank-import bundle that registers every built-in provider package with
   the root registry in one line, for consumers who want zero-touch discovery.

The module never assumes anything about the host application. Every environment-specific concern —
the HTTP client, the SSRF policy, the SMTP transport, HTML templates — is a constructor-injected
interface supplied by the caller (`transport.ClientFactory`, `transport.URLValidator`,
`email.Mailer`, `email.TemplateRenderer`). This is why `go_notify_yourself` must never import
anything from a host application's own module — see the non-negotiable design rule in
[CLAUDE.md](./CLAUDE.md).

## 2. The `Sender` contract

```go
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
```

Every provider package's `Client` implements this one method, and every implementation is expected
to:

- **Respect `ctx`** — honor cancellation/deadlines rather than blocking indefinitely (the HTTP-based
  providers get this for free via `transport.Wrapper.Send`, which threads `ctx` through
  `http.NewRequestWithContext`).
- **Wrap errors, never swallow them** — `Send` returns a descriptive, `fmt.Errorf`-wrapped error on
  failure; it never logs-and-returns-nil.
- **Never panic** — a bad destination, a malformed template, or a provider's non-2xx response is
  always a returned `error`, never a panic. Panicking is reserved for the registry's own
  programmer-error checks (§5) — a `Sender.Send` call is a runtime data-flow path, not a
  configuration-time assertion.

The "why": a host application typically fans one `Message` out to N configured destinations
(`[]notify.Sender`). A uniform contract — same method, same error-vs-panic discipline, same
context-respecting behavior — lets that fan-out loop treat every provider identically, regardless
of whether it's an HTTP webhook or email:

```go
for _, s := range senders {
	if err := s.Send(ctx, msg); err != nil {
		log.Printf("notify: %v", err)
	}
}
```

## 3. Adding a new provider — step-by-step

### 3.1 File/package layout

```
providers/<name>/
  <name>.go         # Config, Client, New(cfg, w) / New(cfg), Send — the core implementation
  <name>_test.go    # table-driven Send tests, config validation tests
  register.go       # init()-time notify.Register("<name>", factory) — kept separate from
                     # <name>.go so registry plumbing doesn't clutter the core constructor logic
  register_test.go  # registration round-trip tests
```

`<name>` is lowercase, no underscores (`webpush`, not `web_push`) — see §3.6 on why this matters.

### 3.2 The `Config` struct convention

- An exported `Config` struct with exported fields, documented with doc comments.
- HTTP-based providers include `Template` and `CustomTemplate string` fields, feeding the shared
  `providers/internal/render` template engine (`Template` selects `"minimal"`/`"detailed"`/
  `"custom"`; `CustomTemplate` supplies the template string when `"custom"`).
- `email` is the **documented exception**: its `Config` has no `Template`/`CustomTemplate` fields at
  all, because email doesn't dispatch a JSON payload — it composes an HTML body via a host-supplied
  `TemplateRenderer` instead. Don't force every provider into the HTTP-shaped `Config` convention;
  follow what the provider's transport actually needs.

### 3.3 The `New` constructor convention

- HTTP-based providers: `func New(cfg Config, w *transport.Wrapper) *Client` — `w` is the shared
  dispatch primitive (see `transport.NewWrapper`), constructed once per host application and
  threaded into every HTTP-based provider's `New`. Never construct your own `*http.Client` inside a
  provider package; always dispatch through the injected `*transport.Wrapper`.
- `email` is the one exception: `func New(cfg Config) *Client` — no `*transport.Wrapper` parameter,
  because email never dials HTTP itself (it hands off to `cfg.Mailer`).

### 3.4 Compile-time interface assertion

Every provider package includes, right after its `Client` type:

```go
var _ notify.Sender = (*Client)(nil)
```

This is required, not optional — it turns "`Client` stopped implementing `Sender`" into a build
failure at the point of the mistake, rather than a runtime type-assertion failure somewhere else.

### 3.5 The `Register`/factory pattern

`register.go`'s exact template (HTTP-based providers — see `providers/discord/register.go` for the
canonical example):

```go
package <name>

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func init() {
	notify.Register("<name>", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`<name>: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			SomeField: regconfig.StringField(config, "some_field"),
			// ... one line per Config field
		}
		return New(cfg, w), nil
	})
}
```

Key/type conventions:

- `"transport"` is reserved for the shared `*transport.Wrapper`, required by every HTTP-based
  provider's factory.
- Every other `Config` field is expected under its **lowercase snake_case field name** — e.g.
  `Config.WebhookURL` → `config["webhook_url"]`, `Config.APIToken` → `config["api_token"]`.
- Use the shared `providers/internal/regconfig` helpers (`StringField`, `StringSliceField`) to
  extract plain string/`[]string` values — don't hand-roll the same type-assert-with-default
  boilerplate in every `register.go`.
- Behavioral/non-serializable values (interfaces, closures — see `providers/email/register.go`) are
  type-asserted directly out of the map under their own well-known key; there is no generic
  extraction helper for these because they're inherently provider-specific.
- **Factories must return an error, never panic, on bad/missing config.** A missing `"transport"`
  key or a config with a required field wrong-typed is a *caller* mistake at runtime (e.g. Charon's
  adapter built the map wrong) — return `fmt.Errorf`, not `panic`. Contrast with `Register` itself
  (§5), which panics on a nil/duplicate registration — that's a *programmer* mistake, caught once at
  `init()` time.

### 3.6 Adding to `providers/all`

Add one blank-import line to `providers/all/all.go`:

```go
_ "github.com/Wikid82/go_notify_yourself/providers/<name>"
```

**This step has no compiler safety net.** A new provider that registers itself correctly but isn't
added to `providers/all` still works fine for a consumer that hand-picks
`import _ "…/providers/<name>"` directly — it just silently isn't part of the "one import gets
everything" bundle. `providers/all/all_test.go` has a `TestAll_RegistersEveryBuiltInProvider` test
asserting `len(notify.RegisteredTypes()) == wantProviderCount` — **bump `wantProviderCount` in that
test whenever you add a provider**, or the test fails loudly instead of silently missing your
addition.

### 3.7 Naming conventions

Provider package names are lowercase, no underscores, and **must match the `Register` key exactly**
(package `webpush` registers as `notify.Register("webpush", ...)`, not `"web_push"` or `"WebPush"`).
This matters because `Register` panics on a duplicate name (§5) — two providers accidentally
choosing the same string is a same-binary collision, and consistent naming is the only thing
preventing an easy, avoidable one.

### 3.8 Test expectations

Every provider package's tests should cover, mirroring the existing eight providers' patterns:

- **Table-driven `Send` tests** against a fake `transport.Wrapper`, built via an injected
  `ClientFactory` returning a `capturingRoundTripper` (see any `providers/*/*_test.go` for the
  pattern) — no real network calls.
- **Config validation error-path tests** — e.g. a missing required field returns an error before any
  dispatch is attempted.
- **≥85% coverage per package**, per this repo's `CLAUDE.md` coverage bar.
- **A registration round-trip test** (in `register_test.go`) asserting `notify.New("<name>",
  validConfig)` succeeds and returns a working `Sender` (behaviorally equivalent to the typed
  constructor).
- **A missing-required-key test** asserting a config missing `"transport"` (or, for email,
  `"mailer"`) returns an `error`, not a panic.

### 3.9 Config validation/error handling conventions

The registry's factory layer validates *structurally* (is the right type present under each
expected key?) and returns a `fmt.Errorf`-wrapped error naming exactly which key/type was expected.
The underlying `Config`-consuming `New`/`Send` still perform their own **semantic** validation
exactly as they do today (e.g. Discord's webhook-host allowlist, Slack's URL-shape regex, Pushover's
required user key/API token). The registry layer is a validation step *in front of*, not *instead
of*, each provider's existing validation.

## 4. The registry internals

`Register`, `New`, and `RegisteredTypes` live at the module root (`factory.go`, `package notify`) —
not a subpackage — because every provider package already imports the root `notify` package (for
`Sender`/`Message`), so this is the only placement with zero new import edges. Behavior:

- **`Register(name string, factory Factory)`** stores `factory` under `strings.ToLower(name)` in a
  `sync.RWMutex`-guarded map. It **panics** if `factory` is nil, if `name` is empty, or if `name` is
  already registered — mirroring `database/sql.Register` exactly. This is a programmer error caught
  at `init()` time (a build-time-discoverable defect, e.g. two packages both registering
  `"webhook"`), not a runtime condition — panicking fails the program immediately and loudly rather
  than silently shadowing one provider with another.
- **`New(name string, config map[string]any) (Sender, error)`** looks up the registered factory
  (case-insensitively) and invokes it. Returns an error — **never panics** — if `name` isn't
  registered; the opposite discipline from `Register`, because "provider type X isn't registered" is
  a legitimate runtime condition (a host forgot to blank-import the package, or a config references
  a typo'd/future type) that calling code must be able to handle gracefully.
- **`RegisteredTypes() []string`** returns the sorted list of currently registered names —
  introspection for a host that wants to validate a config value or populate a UI dropdown against
  exactly what's compiled in.
- The `sync.RWMutex` exists because registrations happen at `init()` time (effectively
  single-threaded, before `main` runs) but `New`/`RegisteredTypes` may be called concurrently from a
  host application's request-handling goroutines.

**The registry is an additive convenience/discovery layer — not a replacement for the typed
constructors.** `discord.New(discord.Config{...}, wrapper)` remains fully supported, fully
type-safe, and is the recommended path for a caller that doesn't need runtime discovery. `notify.New`
trades compile-time type safety at this one boundary (a caller can put a `string` under a key a
factory expects to be a `*transport.Wrapper` and won't find out until `New` returns an error) for a
single uniform mechanism that also accommodates providers like `email` whose `Config` can't
round-trip through a plain-data boundary (JSON) at all.

## 5. Versioning note

Under this module's semver policy:

- **Adding a new provider package** (a new `providers/<name>` with its own `Register` call) is a
  `feat:`/**minor**-version change — it's additive; no existing provider's API surface changes.
- **Changing `Register`, `New`, `Factory`, or `RegisteredTypes`'s signature** is a **breaking/major**
  -version change — every provider package's `register.go` and every host application calling
  `notify.New` directly depends on these exact shapes.
- **Changing an existing provider's typed `Config`/`New`/`Send` signature** is also
  breaking/major, unchanged from this module's policy before the registry existed — the registry
  doesn't relax this.

This is stated explicitly here so a contributor (or a coding agent) doesn't have to guess at PR/tag
time.
