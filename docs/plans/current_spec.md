# Technical Spec: `providers/webpush` — Web Push (Push API / VAPID)

- **Source**: GitHub issue #14, "Add provider: Web Push (Push API / VAPID)".
- **Scope authorization**: the maintainer (issue author/assignee, Wikid82/Jeremy) explicitly asked
  for this provider to be planned and implemented in the conversation that produced this spec. This
  satisfies both CLAUDE.md's "no new provider integrations without an explicit ask" gate and the
  issue's own "do not implement without maintainer sign-off" note.
- **Status**: plan only — no implementation code has been written. Ready for Red/Green
  implementation per the Commit Slicing Strategy (§6).

---

## 1. Introduction

### 1.1 What this adds

A new provider package, `providers/webpush`, implementing `notify.Sender` for direct browser Web
Push delivery: sending a payload straight to a subscribed browser's push endpoint
(`PushSubscription.endpoint`), authenticated with a VAPID JSON Web Token (RFC 8292) and encrypted
per the `aes128gcm` content-coding (RFC 8291). No third-party relay is involved — this is the
mechanism ntfy's web client, Pinglet, and Pingram build on top of, exposed directly.

### 1.2 Why this is in scope now

Per issue #14: this is a "genuinely different capability from the other providers in this module
(they're all relays; this is direct delivery)," and Apprise has no equivalent plugin — this is new
ground, not a straight port. The maintainer's explicit ask (§ above) clears CLAUDE.md's scope gate
for adding a provider beyond the original Charon-ported list (Discord, Slack, Gotify, Pushover,
Ntfy, webhook, Telegram, email).

### 1.3 What this does *not* do (explicit non-goals)

- **No new provider beyond `webpush`.** Nothing here touches Twilio/PagerDuty/Matrix/etc.
- **No Charon-side wiring.** `Charon`'s `NotificationProvider` GORM model's `ServiceConfig` column
  is dead/unwired today (confirmed in `/projects/Charon/docs/plans/notify_provider_registry_spec.md`
  §2.4/§3.1) — wiring it up to actually carry a `>2`-field provider config like webpush's is
  explicitly **out of scope for this module's spec**. This plan only concerns
  `go_notify_yourself`; wiring Charon's UI/DB to construct a `webpush.Config` is separate,
  downstream work the maintainer has not asked for here.
- **No literal RFC 8030 `TTL: 0` ("attempt-only, don't store") escape hatch.** See §3.3's TTL
  discussion — this is a deliberate, documented simplification, not an oversight.
- **No subscription lifecycle management** (no code here parses/refreshes browser
  `PushSubscription.expirationTime`, handles 404/410 "subscription gone" responses specially, or
  persists subscriptions) — that is a per-recipient data-management concern for the host
  application, symmetric with how, e.g., `providers/telegram` doesn't manage chat-ID lifecycle
  either. `Send`'s error return already surfaces a 404/410 from the push service as a normal
  `transport.Wrapper` error (`provider returned status 410`); the host decides what to do with it.

---

## 2. Research Findings

### 2.1 Existing provider convention (read directly: `providers/ntfy/*`, `providers/pushover/*`,
`providers/email/register.go`, `ARCHITECTURE.md` §3)

Every HTTP-based provider package follows exactly this shape (`providers/ntfy/ntfy.go` is the
clean exemplar cited in the task, confirmed by direct read):

```go
type Config struct { /* exported fields, Template/CustomTemplate at the end */ }
type Client struct { cfg Config; wrapper *transport.Wrapper }
var _ notify.Sender = (*Client)(nil)
func New(cfg Config, w *transport.Wrapper) *Client
func (c *Client) Send(ctx context.Context, msg notify.Message) error
```

`register.go` (separate file) does `init()`-time `notify.Register("<name>", factory)`, type-asserts
`config["transport"]` as `*transport.Wrapper`, and builds `Config` from `regconfig.StringField`/
`StringSliceField` calls keyed by the field's lowercase snake_case name. Factories return
`fmt.Errorf`, never panic, on bad/missing config (`ARCHITECTURE.md` §3.5, §3.9).

`providers/internal/render` (read directly) supplies `SelectTemplate`/`Render`/`TemplateData` —
the shared Go `text/template` engine every HTTP provider uses to turn `notify.Message` into a JSON
payload string, with `MinimalTemplate`/`DetailedTemplate` built-ins and a `toJSON` helper. Webpush
reuses this unchanged — the payload it encrypts is exactly this rendered JSON string, not a new
shape.

`providers/internal/regconfig` (read directly) currently has **only** `StringField` and
`StringSliceField` — **confirmed no `IntField` or numeric helper exists today.** Webpush's `TTL
int` config value needs one; scoped as a small standalone first commit (§6, commit 1).

`providers/pushover/pushover.go` (read directly) is the existing exemplar of a `Config` with more
than the `URL`/`Token` two-slot shape (`UserKey, APIToken, BaseURL, Template, CustomTemplate` — 5
fields) — confirming this module's own `Config` structs are not limited to two fields the way
Charon's GORM model is. Webpush's 11-field `Config` (§3.1) is a larger step in the same direction,
not a new pattern.

`providers/email/register.go` (read directly) is the existing exemplar for factories that
type-assert *behavioral* (non-string) values directly out of the config map under a well-known key
(e.g. `config["mailer"].(Mailer)`) rather than via a `regconfig` helper. Not needed for webpush —
every webpush `Config` field is a plain string or int, so `regconfig.StringField`/`IntField` cover
all of it; no behavioral-interface config field is proposed here.

### 2.2 `transport.Wrapper` (read directly: `transport/wrapper.go`, `transport/retry.go`,
`transport/validate_default.go`)

- `sanitizeOutboundHeaders` (in `transport/wrapper.go`, confirmed by direct read) currently
  allowlists exactly: `content-type, user-agent, x-request-id, x-gotify-key, authorization`. Web
  Push requires `Content-Encoding: aes128gcm` and `TTL` (RFC 8030 — push services expect a `TTL`
  header on every request) on every request, and optionally `Urgency`/`Topic`. **None of these four
  are in the current allowlist.** This is a shared-file change affecting every provider's request
  path, not webpush-local — scoped as its own standalone commit (§6, commit 2), additive and
  backward-compatible (existing providers send none of these headers today, so nothing already
  sent changes).
- Headers are canonicalized via `http.CanonicalHeaderKey` after lowercasing (confirmed by read,
  `transport/wrapper.go` L336-354). `http.CanonicalHeaderKey("ttl")` produces `"Ttl"`, not `"TTL"`
  (Go has no acronym table) — this is harmless on the wire (HTTP/1.1 header names are
  case-insensitive per RFC 7230 §3.2; HTTP/2 lowercases all header names in transit regardless),
  but is worth an explicit test assertion (Phase 1, step 2 below) so a future reader isn't alarmed seeing `Ttl` in a
  captured request.
- `Send` (confirmed by read, `transport/wrapper.go` L167-242) treats **any** response status `<
  http.StatusBadRequest` as success and returns `*Result{StatusCode, ResponseBody, Attempts}` to
  the caller. Push services conventionally return `201 Created` (sometimes `200`/`204`) on success —
  **no `transport.Wrapper` change is needed for status-code handling**; this was verified by
  reading the exact conditional (`if resp.StatusCode >= http.StatusBadRequest`), not assumed.
- `DefaultURLValidator` (confirmed by read, `transport/validate_default.go`) only allows `https://`
  destinations unless `allowHTTP` is set. Every real push service endpoint (`fcm.googleapis.com`,
  `updates.push.services.mozilla.com`, `web.push.apple.com`, etc.) is `https://` — no conflict.
  `hasDisallowedQueryAuthKey` (in `transport/wrapper.go`) rejects destination URLs whose query
  string contains `token`/`auth`/`apikey`/`api_key` params — modern push subscription endpoints
  carry their auth material in the URL *path* (e.g.
  `https://fcm.googleapis.com/fcm/send/<registration-id>`), not query params, so this does not
  collide with a well-formed `Endpoint`. Flagged here so a reviewer doesn't have to rediscover it:
  if a host ever configures a push endpoint with such a query param (non-standard, but the RFC
  doesn't forbid it), `Send` will reject it — this module treats that identically to every other
  provider's destination URL, deliberately not special-cased.

### 2.3 Root package (read directly: `message.go`, `sender.go`, `factory.go`)

No changes needed to `notify.Message`, `notify.Sender`, or `notify.Register`/`New`/
`RegisteredTypes` — webpush fits the existing `Factory func(config map[string]any) (Sender, error)`
contract exactly like every other provider.

### 2.4 Prior art in `/projects/Charon` (read directly:
`docs/plans/notify_provider_registry_spec.md`)

That spec (Charon repo, not this module) discusses Web Push extensively as the motivating case for
why Charon's *registry* config boundary needed to be `map[string]any` rather than
`json.RawMessage`-typed generics (§3.2 of that doc): "a provider needing more than two config
values (e.g. Web Push's VAPID public/private keypair + subscription endpoint — three values, none
of which is a natural fit for 'URL' or 'Token') cannot be expressed in [Charon's] current two-slot
scheme at all." It also confirms (§2.4) Charon's `NotificationProvider.ServiceConfig` GORM column
is declared but has **zero read/write call sites** anywhere in Charon's backend — dead schema,
earmarked for exactly this kind of provider but not wired up. Per §1.3 above, wiring that up is
explicitly out of scope here; it's cited only as confirmation that this module's own `Config`
struct is unconstrained by Charon's schema (this module doesn't share Charon's DB layer at all —
CLAUDE.md's non-negotiable import rule).

### 2.5 Cryptography — stdlib-only feasibility (verified via `go doc`, not assumed)

This module is dependency-free (non-negotiable, CLAUDE.md). Two RFC-standardized protocol layers
are needed, both confirmed achievable with only the Go standard library at `go 1.27.1` (this
module's `go.mod` directive, confirmed by direct read):

- **RFC 8292 (VAPID)**: an ES256-signed JWT. `crypto/ecdsa` (`ecdsa.Sign`, which returns `(r, s
  *big.Int, err error)` directly — **not** `ecdsa.SignASN1`, which DER-encodes; JOSE/JWT ES256
  signatures are raw, left-zero-padded, big-endian `r || s`, 32 bytes each, 64 bytes total) plus
  hand-rolled base64url JSON header/payload encoding. No JWT library needed or wanted.
- **RFC 8291 (`aes128gcm` content-coding)**: `crypto/ecdh` (confirmed present via `go doc
  crypto/ecdh`: `ecdh.P256()`, `PrivateKey`, `PublicKey`) for the ECDH step between an ephemeral
  server keypair and the subscriber's `p256dh` key; `crypto/hkdf` (confirmed present via `go doc
  crypto/hkdf`: `hkdf.Extract`, `hkdf.Expand`, `hkdf.Key`, all added to the stdlib in Go 1.24 — this
  module's `go 1.27.1` floor comfortably covers it) for the two-step key derivation; `crypto/aes` +
  `crypto/cipher` for AES-128-GCM. All confirmed present in this Go toolchain by running `go doc`
  directly, not assumed from general Go knowledge.
- **Important distinction for the implementer**: VAPID signing uses a `crypto/ecdsa.PrivateKey`
  (the application's long-lived VAPID keypair). RFC 8291 payload encryption uses `crypto/ecdh`
  keys for two *separate* P-256 keypairs — a fresh ephemeral one generated per `Send` call, and the
  subscriber's `p256dh` public key. These are three distinct P-256 keys serving two different
  purposes and two different Go stdlib APIs (`ecdsa` vs `ecdh`) — despite all being "P-256," no
  direct reuse or conversion between the VAPID signing key and the encryption keys is needed or
  correct; keep them handled by entirely separate code paths (§3.2 file layout reflects this).

### 2.6 RFC 8291 Appendix A fixed test vectors

RFC 8291 Appendix A ("A Detailed Example") publishes a complete fixed example: a receiver (UA)
P-256 keypair, an `auth` secret, a sender (application server) ephemeral P-256 keypair, a 16-byte
salt, the plaintext `"When I grow up, I want to be a watermelon"`, and the exact resulting
`aes128gcm` ciphertext bytes. This is critical for this implementation's correctness gate (Phase 1
step 3 and §6 commit 3's blocking gate, both below) —
a self-encrypt/self-decrypt round-trip test alone cannot catch a bug that is symmetric in both
directions (e.g. a wrong HKDF `info` string used consistently on both the encrypt and decrypt side
would still round-trip), and there is no external Web Push library available to interop-test
against given the dependency-free constraint. The fixed-vector test is exact-bytes, not
round-trip, and is a **blocking** part of Phase 1/2 (§5).

---

## 3. Technical Specification

### 3.1 Package layout

```
providers/webpush/
  webpush.go        # package doc comment, Config, Client, New, Send (orchestration only)
  vapid.go           # RFC 8292: GenerateVAPIDKeyPair (exported), buildVAPIDHeader (unexported)
  encrypt.go          # RFC 8291: encryptAES128GCM (unexported) + its WithKeys test seam
  webpush_test.go
  vapid_test.go
  encrypt_test.go     # includes the RFC 8291 Appendix A fixed-vector test
  register.go
  register_test.go
```

No new `providers/internal/*` subpackage — the crypto pieces are webpush-specific (unlike
`render`/`regconfig`, which are shared across every provider), so they stay as unexported
same-package files, the same way `providers/email/default_template.go` is a same-package file
alongside `email.go` rather than its own internal package.

`<name>` is `webpush` — lowercase, no underscore, already used as `ARCHITECTURE.md`'s own running
example (§3.1, §3.7) for exactly this reason. No naming decision to make.

### 3.2 `Config` (exported, `webpush.go`)

```go
// Config configures a webpush Sender. Unlike every other provider's Config,
// this one mixes two conceptually distinct groups of fields: VAPID
// application identity (shared across every subscription this application
// pushes to) and one subscriber's PushSubscription destination. A host
// application constructs one webpush.Client per subscriber, reusing the
// same VAPID* values across all of them — see the package doc comment for
// the fan-out pattern.
type Config struct {
	// --- VAPID application identity (RFC 8292) ---

	// VAPIDPublicKey is the application server's VAPID public key: an
	// uncompressed P-256 point (65 bytes: 0x04 || X || Y), base64url
	// (no padding) encoded. This is the same value the browser is given as
	// PushManager.subscribe({applicationServerKey: VAPIDPublicKey}).
	// Required.
	VAPIDPublicKey string

	// VAPIDPrivateKey is the application server's VAPID private key: a
	// 32-byte P-256 scalar, base64url (no padding) encoded. Required. This
	// value never leaves the process — Send signs a JWT with it locally
	// and never transmits it.
	VAPIDPrivateKey string

	// VAPIDSubject identifies the application server operator, per RFC
	// 8292's "sub" JWT claim: a "mailto:" or "https:" URI (e.g.
	// "mailto:ops@example.com"). Some push services (notably Mozilla's)
	// reject a VAPID JWT with an empty or malformed sub. Required.
	VAPIDSubject string

	// --- Subscriber destination (the browser's PushSubscription) ---

	// Endpoint is the subscription's push service URL, from
	// PushSubscription.endpoint. Required.
	Endpoint string

	// P256dh is the subscriber's P-256 Diffie-Hellman public key, from
	// PushSubscription.getKey('p256dh'): base64url (no padding) encoded.
	// Required.
	P256dh string

	// Auth is the subscriber's 16-byte authentication secret, from
	// PushSubscription.getKey('auth'): base64url (no padding) encoded.
	// Required.
	Auth string

	// --- Delivery hints (RFC 8030) ---

	// TTL is the number of seconds the push service should retain the
	// message if the subscriber is currently offline, sent as the "TTL"
	// header. Zero uses DefaultTTL — see that constant's doc comment.
	TTL int

	// Urgency is an optional RFC 8030 "Urgency" header value: one of
	// "very-low", "low", "normal", "high". Empty omits the header (the
	// push service's own default applies, typically "normal"). Send
	// rejects any other value.
	Urgency string

	// Topic is an optional RFC 8030 "Topic" header value: up to 32
	// characters from the URL-and-filename-safe base64 alphabet
	// ([A-Za-z0-9_-]). When set, a pending undelivered message with the
	// same Topic is replaced rather than queued alongside it. Empty omits
	// the header. Send rejects a Topic outside this charset/length.
	Topic string

	// --- Payload templating ---

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate) — same convention as
	// every other JSON-payload provider (providers/internal/render). The
	// rendered JSON is the plaintext that gets RFC 8291-encrypted; the
	// receiving service worker's `push` event handler is responsible for
	// JSON.parse-ing the decrypted payload. This module has no opinion on
	// what the service worker does with it beyond that it is valid JSON.
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// DefaultTTL is used for the RFC 8030 "TTL" header when Config.TTL is zero.
// Four weeks (2,419,200 seconds) — a conservative value inside the maximum
// retention window most push services honor before evicting an
// undelivered message. See Config.TTL's doc comment and §3.3 of this
// module's design notes for why Config.TTL's zero value is *not* treated
// as RFC 8030's spec-legal "attempt immediate delivery only, don't store"
// meaning.
const DefaultTTL = 4 * 7 * 24 * 3600
```

**Decision — `GenerateVAPIDKeyPair` (issue's open question 1): include it.**

```go
// GenerateVAPIDKeyPair generates a new P-256 VAPID application server
// keypair, returned as the same base64url (no padding) encoded strings
// Config.VAPIDPublicKey/Config.VAPIDPrivateKey expect. Intended to be
// called once at application setup time (e.g. from an init/CLI flow) and
// the results persisted by the host application — every browser
// PushSubscription is bound to the exact public key it was created with
// (PushManager.subscribe({applicationServerKey: ...})), so rotating this
// keypair invalidates every existing subscription the host has collected.
// privateKey is a credential, not a diagnostic value: callers must not log
// it (a mistake this function can't prevent, only warn against — the same
// discipline hosts already need for VAPIDPrivateKey once it's in Config).
func GenerateVAPIDKeyPair() (publicKey, privateKey string, err error)
```

Reasoning against CLAUDE.md's "keep the public surface intentionally small" guidance: that
guidance is about not accumulating unnecessary provider-to-provider surface area / avoiding
breaking-change risk, not about refusing one small, self-contained helper that removes an entire
manual-crypto step. Concretely:

- Every real-world Web Push library (`web-push` npm, `pywebpush`, Go's own `SherClockwork/webpush`)
  ships an equivalent generator, because hand-producing a correct raw-uncompressed-point-encoded
  P-256 keypair via `openssl` CLI incantations is exactly the kind of fiddly, easy-to-get-subtly-wrong
  step (compressed vs. uncompressed point, DER vs. raw, base64 vs. base64url, padded vs.
  unpadded) that silently produces a keypair the browser's `PushManager.subscribe` rejects or a
  push service 401s on — hard for a non-technical host (this project's stated target audience,
  per the maintainer's UX-friction guidance for this task) to self-diagnose.
- It is a pure, stateless, one-shot function — it doesn't grow `Send`'s per-request surface, add a
  DI seam, or create a new exported type. A host that already has externally-generated keys (e.g.
  from `web-push generate-vapid-keys`) can ignore this function entirely; `Config` only ever takes
  plain base64url strings either way, so this is strictly additive convenience, not a new
  requirement.
- Unlike a bot token or webhook URL (issued by a third-party service the host copies from a
  website), a VAPID keypair is *this application's own* identity — there is no external "go get
  this" step to document instead; the host is expected to generate it itself once. That is a
  meaningfully different case from every other provider's credentials, justifying a first-of-its-kind
  helper here without setting a precedent that every future provider needs one.

**Decision — `TTL` zero-value semantics (issue's open question 2): package-level default constant,
not the RFC's spec-legal zero-value meaning.**

RFC 8030 §5.2 specifies `TTL: 0` as legal and meaningful: "attempt to deliver the message
immediately, and if that's not possible (subscriber offline), don't store it — drop it." That is a
reasonable choice for some applications (e.g. transient live-typing indicators) but is a
**surprising silent default** for this module's stated audience: a self-hoster who configures a
notification and expects it to "eventually show up," not silently vanish because the browser tab
happened to be closed at send time. Per the maintainer's explicit UX-friction-minimization
guidance for this task, `Send` treats `Config.TTL == 0` as "unset" and substitutes `DefaultTTL`
(4 weeks — see the constant's doc comment above) rather than forwarding a literal `0` to the push
service. This produces the most reliable out-of-the-box delivery experience with zero required
host-side configuration (a host that wants the RFC's literal immediate-only semantics can still
get arbitrarily close by setting `Config.TTL` to a very small positive number, e.g. `1`; there is
deliberately no way to configure a literal `TTL: 0` request through this `Config` — flagged here
explicitly as a documented, intentional simplification, not an oversight, and not something to
silently work around later without re-opening this decision).

**This is a permanent public-API foreclosure, not just a mutable default.** Because `Config.TTL`
has no sentinel distinct from Go's own int zero value, there is no future-compatible way to add a
"no really, send a literal 0" escape hatch to this exact field later without a breaking change
(e.g. a new `Config` field, or changing `TTL`'s type) — this decision permanently removes RFC
8030's "attempt-only, don't store" semantics from this `Config`'s expressible range, for every
future caller, not merely until someone changes a default. It is deliberately made here under the
maintainer's pre-delegated UX-friction guidance for this task, but — being irreversible rather than
adjustable — it is called out explicitly so the maintainer can give it one explicit nod before
implementation begins, even though it was pre-delegated.

### 3.3 `Client` / `New` / `Send` (`webpush.go`)

```go
// Package webpush implements notify.Sender for direct browser Web Push
// delivery (RFC 8030/8291/8292) — no third-party relay involved. A single
// Config pairs one application's VAPID identity with one browser
// PushSubscription; a host application fanning a Message out to many
// subscribers constructs one *Client per subscription (cheap: New does no
// I/O) and calls Send on each, exactly like fanning out to many
// Sender values of any other provider type.
package webpush

// Client dispatches notify.Message values to one browser PushSubscription.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a webpush Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client

// Send renders msg using the configured template, RFC 8291-encrypts the
// result for the configured subscriber, and dispatches it to
// cfg.Endpoint via the shared transport.Wrapper, authenticated with an
// RFC 8292 VAPID JSON Web Token signed for this request.
func (c *Client) Send(ctx context.Context, msg notify.Message) error
```

`Send`'s algorithm, in order (fail-fast: every validation step below runs before any network
activity or expensive crypto work; each returns a `fmt.Errorf`-wrapped, field-naming error on
failure, mirroring `ntfy`/`pushover`'s style):

1. **Required-field validation**, in this order, each its own error message (mirrors
   `pushover.Send`'s "api token" / "user key" sequential-check style):
   `VAPIDPublicKey`, `VAPIDPrivateKey`, `VAPIDSubject`, `Endpoint`, `P256dh`, `Auth` — each
   `strings.TrimSpace`'d and, if empty, `fmt.Errorf("webpush: <field> is not configured")`.
   Immediately after the `VAPIDSubject` emptiness check, a cheap format check: `VAPIDSubject` must
   start with `mailto:` or `https://` (a plain `strings.HasPrefix` check on either, no full URI
   parse) or `fmt.Errorf("webpush: VAPID subject must start with %q or %q", "mailto:", "https://")`.
   This is the same fail-fast-before-any-network-work treatment as every other precondition in this
   step — added specifically because this package's own `VAPIDSubject` doc comment already warns
   that some push services (notably Mozilla's) reject a malformed `sub` claim, so silently accepting
   a clearly-malformed value here (e.g. a bare email address with no scheme) would defer a locally
   catchable error into an opaque remote 401/403.
2. **VAPID keypair consistency check**: decode `VAPIDPrivateKey`, derive its corresponding public
   key (`crypto/ecdsa` — `(*ecdsa.PrivateKey).PublicKey`, re-encoded to the same uncompressed
   base64url form), and compare byte-for-byte against the configured `VAPIDPublicKey`. Mismatch →
   `fmt.Errorf("webpush: VAPID public/private key pair does not match")`. This catches a very
   common real-world misconfiguration (copy-pasting one half of a keypair against the other half of
   a different generation) with a clear, actionable error instead of an opaque `401` surfaced later
   from the push service by `transport.Wrapper`.
3. **`Urgency` validation** (if non-empty): must be one of `very-low`, `low`, `normal`, `high`
   (case-sensitive, matching RFC 8030's literal token values) or
   `fmt.Errorf("webpush: invalid urgency %q", cfg.Urgency)`.
4. **`Topic` validation** (if non-empty): must match `^[A-Za-z0-9_-]{1,32}$` or
   `fmt.Errorf("webpush: invalid topic %q: must be 1-32 URL-safe base64 characters", cfg.Topic)`.
5. **Render the template**: `render.SelectTemplate` + `render.Render`, identical call shape to
   `ntfy`/`pushover`. Validate the rendered output is valid JSON (`json.Unmarshal` into `any`) —
   `fmt.Errorf("invalid JSON payload: %w", err)` on failure, matching `ntfy`/`pushover`'s existing
   message text convention. **Unlike `ntfy`/`pushover`, do not require a `"message"` field** — that
   requirement is specific to those providers' own remote API contract; a Web Push payload's shape
   is entirely up to the receiving service worker's own JS, which this module has no visibility
   into or opinion about.
6. **Encrypt**: `encryptAES128GCM(cfg.P256dh, cfg.Auth, renderedJSONBytes)` (§3.4) →
   `ciphertext []byte`. Decode/format errors from this step (bad base64, wrong-length key/secret,
   or `renderedJSONBytes` longer than `MaxPlaintextSize` — §3.4) propagate as
   `fmt.Errorf("webpush: encrypt payload: %w", err)`.
7. **Build the VAPID Authorization header**:
   `buildVAPIDHeader(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject, cfg.Endpoint)`
   (§3.5) → `authHeader string`, or a wrapped error. `buildVAPIDHeader` takes the three VAPID
   strings directly rather than the whole `Config` deliberately — see §3.5's signature note.
8. **Build headers**:
   ```go
   headers := map[string]string{
       "Content-Type":     "application/octet-stream",
       "Content-Encoding": "aes128gcm",
       "Authorization":    authHeader,
       "TTL":              strconv.Itoa(ttl), // ttl = cfg.TTL, or DefaultTTL if cfg.TTL == 0
   }
   if cfg.Urgency != "" { headers["Urgency"] = cfg.Urgency }
   if cfg.Topic != "" { headers["Topic"] = cfg.Topic }
   ```
9. **Dispatch**: `c.wrapper.Send(ctx, transport.Request{URL: cfg.Endpoint, Headers: headers, Body:
   ciphertext})`. Wrap any error as `fmt.Errorf("failed to send web push: %w", err)` (matching the
   existing "failed to send webhook"-style wording convention, adapted to this provider's name).

### 3.4 `encryptAES128GCM` (RFC 8291, `encrypt.go`, unexported)

```go
// encryptAES128GCM implements RFC 8291 Web Push message encryption. Given
// the subscriber's base64url (no padding) encoded p256dh public key and
// auth secret (from PushSubscription.getKey), and the plaintext
// application payload, it returns the aes128gcm content-coded ciphertext
// (RFC 8188 §2 single-record framing: salt(16) || rs(4) || idlen(1) ||
// keyid(65, the ephemeral sender public key, uncompressed) ||
// AEAD-ciphertext) ready to send as the request body. Generates a fresh
// ephemeral P-256 keypair and a fresh random 16-byte salt per call — see
// encryptAES128GCMWithKeys for the deterministic variant tests use.
func encryptAES128GCM(p256dhB64, authB64 string, plaintext []byte) ([]byte, error)

// encryptAES128GCMWithKeys is encryptAES128GCM with the ephemeral sender
// keypair and salt injected rather than randomly generated — the
// production encryptAES128GCM is a thin wrapper generating both randomly
// and delegating here. Exists so tests (in particular the RFC 8291
// Appendix A fixed-vector test, encrypt_test.go) can force the exact
// keys/salt the RFC's published example uses and assert exact-byte
// output — a capability a purely-random production path can't otherwise
// be tested against without an external reference implementation, which
// this dependency-free module cannot depend on.
func encryptAES128GCMWithKeys(ephemeral *ecdh.PrivateKey, salt []byte, p256dhB64, authB64 string, plaintext []byte) ([]byte, error)
```

```go
// MaxPlaintextSize is the largest plaintext payload encryptAES128GCM will
// accept, in bytes. RFC 8188 §2 single-record framing adds a fixed 86-byte
// record header (salt(16) + rs(4) + idlen(1) + keyid(65)) plus a 1-byte
// delimiter and a 16-byte AES-GCM tag around the plaintext (103 bytes of
// fixed overhead total), and real push services independently cap the
// resulting request body at roughly 4096 bytes (FCM and Mozilla autopush
// both document limits in this neighborhood). MaxPlaintextSize is set well
// inside that ceiling (86 + 3800 + 17 = 3903 bytes total, vs. a ~4096-byte
// external cap) rather than exactly at the boundary, so a plaintext this
// module accepts is not immediately at risk of a push-service-side
// rejection this module can't see coming.
const MaxPlaintextSize = 3800
```

Before any derivation work, `encryptAES128GCMWithKeys` (and therefore `encryptAES128GCM`, which
calls it) checks `len(plaintext) > MaxPlaintextSize` and returns
`fmt.Errorf("webpush: payload of %d bytes exceeds maximum plaintext size of %d bytes", len(plaintext), MaxPlaintextSize)`
— fail-fast, consistent with §3.3's fail-fast design: no ECDH/HKDF/AES work is attempted on an
oversized payload. `Send`'s step 6 (§3.3) wraps this the same way it wraps every other
`encryptAES128GCM` error, so no separate size-check step is needed in `Send`'s own algorithm.

Derivation steps (RFC 8291 §3.3-3.4, cited precisely for the implementer — not implemented here
per the "no implementation code" constraint), run only once the size check above passes:

1. Decode `p256dhB64`/`authB64` (base64url, no padding via `base64.RawURLEncoding`). `p256dh` must
   decode to a 65-byte uncompressed P-256 point; `auth` must decode to exactly 16 bytes. Either
   mismatch is a returned error naming which field and why (e.g. `"p256dh: expected 65-byte
   uncompressed P-256 point, got %d bytes"`).
2. Parse the subscriber's `p256dh` as an `*ecdh.PublicKey` via `ecdh.P256().NewPublicKey(raw)`.
3. Compute the ECDH shared secret between `ephemeral` (the sender's ephemeral private key) and the
   subscriber's public key: `ephemeral.ECDH(subscriberPub)`.
4. Per RFC 8291 §3.4: derive `IKM` via `HKDF-Extract(salt=auth_secret, ikm=ecdh_secret)` with an
   `HKDF-Expand` info string of `"WebPush: info" || 0x00 || ua_public(65 bytes) || as_public(65
   bytes)`, 32 bytes output — `ua_public` is the subscriber's raw `p256dh` bytes, `as_public` is
   the ephemeral sender public key's raw uncompressed bytes.
5. Derive the content-encryption key and nonce from `IKM` and `salt` (the random/injected 16-byte
   salt, distinct from the `auth` secret used as HKDF salt in step 4): `PRK = HKDF-Extract(salt,
   IKM)`; `CEK = HKDF-Expand(PRK, "Content-Encoding: aes128gcm" || 0x00, 16)`; `nonce =
   HKDF-Expand(PRK, "Content-Encoding: nonce" || 0x00, 12)`.
6. Per RFC 8188 §2: append a single `0x02` delimiter byte to `plaintext` (no padding beyond the
   delimiter, since this is always a single, final record — payloads are capped well under the
   4096-byte example record size RFC 8291 uses).
7. `AES-128-GCM` encrypt (`crypto/cipher.NewGCM` over an `crypto/aes.NewCipher(CEK)` block) the
   delimited plaintext with `nonce`, no additional authenticated data.
8. Frame per RFC 8188 §2: `salt (16 bytes) || rs (4 bytes, big-endian record size — use 4096, the
   value RFC 8291's own example uses, since this module always emits exactly one record) || idlen
   (1 byte, 65) || keyid (65 bytes, the ephemeral sender's raw uncompressed public key) ||
   ciphertext-with-tag`.

### 3.5 VAPID JWT (RFC 8292, `vapid.go`)

```go
// vapidJWTLifetime bounds the "exp" claim on the VAPID JWT Send signs for
// each request: 12 hours from the time of signing. RFC 8292 recommends an
// expiration no more than 24 hours out; 12 hours is comfortably inside
// that bound while still meaning a Client's signed header is reusable
// across a short burst of retries/sends without re-signing every time
// (though Send always signs fresh per call — see below).
const vapidJWTLifetime = 12 * time.Hour

// buildVAPIDHeader builds the RFC 8292 "Authorization: vapid t=<jwt>,
// k=<public key>" header value for a request to endpoint, signed with the
// given VAPID keypair/subject. aud is derived from endpoint's scheme+host
// (RFC 8292 §2: the JWT audience is the push service's origin, not the
// full subscription path).
//
// Takes the three VAPID strings directly rather than the whole Config by
// design, not just convenience: buildVAPIDHeader only ever reads 3 of
// Config's 11 fields, and Config itself isn't defined until webpush.go
// (§3.3/§6 commit 5) — a Config parameter here would make vapid.go (§6
// commit 4) depend on a type that doesn't exist yet at that point in the
// commit sequence, breaking §6's per-commit build/test guarantee. Taking
// plain strings keeps this function buildable and independently testable
// (vapid_test.go) two commits before Config exists.
func buildVAPIDHeader(vapidPublicKey, vapidPrivateKey, vapidSubject, endpoint string) (string, error)
```

- `aud` = `neturl.Parse(endpoint)`'s `Scheme + "://" + Host` (no path, no trailing slash).
- `exp` = `time.Now().Add(vapidJWTLifetime).Unix()`.
- `sub` = `vapidSubject` verbatim (already required non-empty, and prefix-validated, by `Send`'s
  step 1 — §3.3; `buildVAPIDHeader` itself doesn't re-validate it, since it has no `Config` to read
  a validation policy from and takes its inputs on trust from the caller. This is the same
  "structural vs. semantic" split §3.9 describes across the registry/typed-constructor boundary,
  applied here within one package: `Send` owns semantic validation, `buildVAPIDHeader` is a pure
  JWT-construction primitive.)
- JWT header: `{"typ":"JWT","alg":"ES256"}`, base64url (no padding) of the compact JSON.
- JWT payload: `{"aud":"<aud>","exp":<exp>,"sub":"<sub>"}`, same encoding.
- Signature: `ecdsa.Sign(rand.Reader, privKey, sha256(header + "." + payload))` → `(r, s)`, each
  left-zero-padded big-endian to 32 bytes, concatenated (64 bytes total), base64url (no padding)
  encoded — **not** `ecdsa.SignASN1`, which DER-encodes and is the wrong shape for JOSE/JWS ES256.
  `privKey` here is the `*ecdsa.PrivateKey` decoded from `vapidPrivateKey`.
- Returned value: `fmt.Sprintf("vapid t=%s.%s.%s, k=%s", headerB64, payloadB64, sigB64,
  vapidPublicKey)`.

`GenerateVAPIDKeyPair` (§3.2, also lives in `vapid.go`): `ecdsa.GenerateKey(elliptic.P256(),
rand.Reader)`, then encode the private key's `D` (32-byte big-endian scalar) and the public key's
uncompressed point (`0x04 || X(32) || Y(32)`, both big-endian, zero-padded) each via
`base64.RawURLEncoding`.

### 3.6 `register.go`

```go
// init registers this package's Factory under the name "webpush" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper.
//   - "vapid_public_key", "vapid_private_key", "vapid_subject" (string, required).
//   - "endpoint", "p256dh", "auth" (string, required).
//   - "ttl" (int, optional; 0 uses DefaultTTL).
//   - "urgency", "topic" (string, optional).
//   - "template", "custom_template" (string, optional).
func init() {
	notify.Register("webpush", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`webpush: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			VAPIDPublicKey:  regconfig.StringField(config, "vapid_public_key"),
			VAPIDPrivateKey: regconfig.StringField(config, "vapid_private_key"),
			VAPIDSubject:    regconfig.StringField(config, "vapid_subject"),
			Endpoint:        regconfig.StringField(config, "endpoint"),
			P256dh:          regconfig.StringField(config, "p256dh"),
			Auth:            regconfig.StringField(config, "auth"),
			TTL:             regconfig.IntField(config, "ttl"),
			Urgency:         regconfig.StringField(config, "urgency"),
			Topic:           regconfig.StringField(config, "topic"),
			Template:        regconfig.StringField(config, "template"),
			CustomTemplate:  regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
```

Follows `ARCHITECTURE.md` §3.5's template exactly; no deviation.

### 3.7 New shared-infrastructure surface (not webpush-local)

**`providers/internal/regconfig.IntField`** (new, in `regconfig.go`):

```go
// IntField returns config[key] as an int, or 0 if the key is absent or not
// an int-like value. Accepts int and int64 (the natural Go-side shapes)
// and float64 (the shape a generic JSON-style decode into map[string]any
// produces, since encoding/json decodes every JSON number as float64) —
// mirroring StringSliceField's existing dual-shape acceptance for []any.
func IntField(config map[string]any, key string) int
```

**`transport.sanitizeOutboundHeaders`** (change, in `transport/wrapper.go`): add
`"content-encoding"`, `"ttl"`, `"urgency"`, `"topic"` to the `allowed` set (§2.2). No other change
to `transport/wrapper.go`.

### 3.8 Error handling / edge cases summary

| Case | Behavior |
|---|---|
| Missing any of the 6 required `Config` fields | `Send` returns `fmt.Errorf("webpush: <field> is not configured")` before any crypto/network work |
| `VAPIDSubject` set but missing the `mailto:`/`https://` prefix | `Send` returns a named format error before any network work (§3.3 step 1) |
| `VAPIDPublicKey`/`VAPIDPrivateKey` don't form a matching pair | `Send` returns a named error before any network work (§3.3 step 2) |
| Malformed base64 or wrong-length `p256dh`/`auth`/VAPID keys | `Send` returns a wrapped decode error naming the field |
| Invalid `Urgency`/`Topic` value | `Send` returns a named validation error before any network work |
| Rendered payload exceeds `MaxPlaintextSize` (§3.4) | `Send` returns a wrapped size error (via `encryptAES128GCM`) before any encryption work is attempted |
| Custom template renders invalid JSON | `Send` returns `"invalid JSON payload: %w"`, same wording as `ntfy`/`pushover` |
| Push service returns 4xx/5xx | Surfaced unchanged via `transport.Wrapper.Send`'s existing `"provider returned status %d[: hint]"` error — no webpush-specific handling; symmetric with every other provider |
| Push service returns 404/410 (subscription gone) | Same as any other 4xx — no special-casing (§1.3); host application's responsibility to react |
| `ctx` cancelled/deadline exceeded | Propagates via `transport.Wrapper.Send`'s existing `ctx`-respecting `http.NewRequestWithContext` |
| VAPID JWT signing failure (`crypto/rand` exhausted, etc.) | `Send` returns a wrapped error; treated as any other precondition failure, not retried (this is not a transient network condition `transport.RetryPolicy` should retry) |

---

## 4. Documentation updates (Phase 4 scope, detailed in §6)

- `README.md`: add a `providers/webpush` row to the provider table (§"Provider packages", currently
  lines 90-99) and update the "Project status" paragraph (currently line 222: "the provider list is
  intentionally exactly these seven HTTP providers plus email") to note webpush as an explicit,
  deliberate, maintainer-approved addition beyond the original Charon-extraction list, distinguishing
  it from the "no new providers without an explicit ask" policy it doesn't violate.
- `docs/INTEGRATION.md`: add a `webpush.New(webpush.Config{...}, wrapper)` construction example
  alongside the existing `discord.New(...)` one (currently line 162), and a short note on the
  "one Client per subscriber, shared VAPID identity" fan-out pattern (§3.3's package doc comment).
- `ARCHITECTURE.md`: §3.2 ("The `Config` struct convention") gets one short addition noting webpush
  as the first provider whose `Config` mixes app-wide identity fields with per-recipient
  destination fields in a single struct — still one flat exported struct, still fed through the
  same `New(cfg, w)` constructor shape, so §3.3 needs no rewrite; just a sentence flagging the
  precedent for a future reader who might otherwise assume every `Config` field is per-recipient.
  No other section needs a substantive change — §3.6/3.7 already use `webpush` as their own running
  example name.

---

## 5. Implementation Plan

### Phase 1 — Failing tests (Red)

Write, in this order, before any implementation:

1. `providers/internal/regconfig/regconfig_test.go` additions: `IntField` missing key, wrong type
   (`string`), `int` value, `int64` value, `float64` value (JSON-decode shape) — 5+ cases.
2. `transport/wrapper_test.go` additions: a `Send` call with `Content-Encoding`/`TTL`/`Urgency`/
   `Topic` headers set asserts all four pass through to the captured request (case-insensitively —
   assert via `req.Header.Get`, which is itself case-insensitive, sidestepping the `Ttl` vs `TTL`
   canonicalization detail at the assertion layer); a header not in the allowlist (e.g. `X-Foo`) is
   still stripped, proving the change is additive, not a wholesale relaxation.
3. `providers/webpush/encrypt_test.go`: the RFC 8291 Appendix A fixed-vector test
   (`encryptAES128GCMWithKeys` called with the RFC's exact receiver keys/auth secret/ephemeral
   sender keypair/salt/plaintext, asserting the exact output ciphertext bytes match the RFC's
   published example byte-for-byte) — written and failing (function doesn't exist yet) first, since
   this is the single highest-value/highest-risk test in the whole feature (§2.6). Additional cases:
   malformed `p256dh` (wrong length), malformed `auth` (wrong length), a plaintext one byte over
   `MaxPlaintextSize` returning the named size error (§3.4), a plaintext exactly at
   `MaxPlaintextSize` succeeding (boundary case), and a supplementary (not sufficient-alone)
   self-encrypt/self-decrypt round-trip sanity check.
4. `providers/webpush/vapid_test.go`: `GenerateVAPIDKeyPair` produces a valid, matching pair
   (round-trip: derive public from generated private, compare); `buildVAPIDHeader` (called directly
   with plain VAPID strings — no `Config` involved, per §3.5's signature) produces a
   `vapid t=<jwt>, k=<key>` value whose JWT decodes to the expected `alg`/`typ`/`aud`/`sub`/`exp`
   and whose signature verifies against the configured public key (`ecdsa.Verify` on the decoded
   raw `r||s`, reconstructed as `big.Int`s).
5. `providers/webpush/webpush_test.go`: table-driven `Send` tests against a `capturingRoundTripper`
   (same harness pattern as `providers/ntfy/ntfy_test.go`, confirmed by direct read) — one test per
   row of §3.8's table, plus: `Content-Type`/`Content-Encoding`/`TTL`/`Authorization` headers present
   and correctly shaped on a successful send; request body is not the plaintext JSON (opaque
   ciphertext — a regression here would be a real plaintext-leak bug, worth its own explicit
   assertion); `TTL` header reflects `DefaultTTL` when `Config.TTL` is zero and the configured value
   otherwise; `Urgency`/`Topic` headers present only when configured.
6. `providers/webpush/register_test.go`: mirrors `providers/ntfy/register_test.go`'s three-test
   pattern exactly (success round-trip incl. `ttl` int passing through `regconfig.IntField`, missing
   `"transport"` returns error not panic, registered under `"webpush"` in `RegisteredTypes()`).
7. `providers/all/all_test.go`: bump `wantProviderCount` by 1 (written failing, since the count
   won't match until commit 6 of §6 lands).

### Phase 2 — Implementation (Green)

Implement, in dependency order, until each Phase 1 test file's tests pass:
`regconfig.IntField` → `transport.sanitizeOutboundHeaders` → `encrypt.go` → `vapid.go` →
`webpush.go` → `register.go` → `providers/all/all.go`.

### Phase 3 — Lint/coverage hardening

- `go vet ./...` and `staticcheck ./...` clean across every touched package.
- `go test ./... -cover` — confirm ≥85% for `providers/internal/regconfig`, `transport`,
  `providers/webpush`, `providers/all`. The crypto error-path cases in Phase 1 step 3
  (malformed `p256dh`/`auth`) and step 5 (§3.8's full error table) exist specifically to hit
  `encrypt.go`/`vapid.go`/`webpush.go`'s error branches, not just their happy paths — coverage
  should not need artificial padding tests if Phase 1 was followed as written.
- `scripts/test-coverage.sh` run to confirm the repo-wide gate, not just per-package `-cover`
  output, matches.

### Phase 4 — Doc comments and README/INTEGRATION.md updates

- Confirm every new exported identifier (`webpush.Config` and its fields, `webpush.Client`,
  `webpush.New`, `webpush.Send`, `webpush.DefaultTTL`, `webpush.MaxPlaintextSize`,
  `webpush.GenerateVAPIDKeyPair`,
  `regconfig.IntField`) has a doc comment — all drafted verbatim in §3 above; Phase 4 is
  transcription plus the cross-file documentation updates in §4, not new design.

---

## 6. Commit Slicing Strategy

One PR, seven ordered, independently buildable/testable commits (each passes `go build ./...`,
`go vet ./...`, `staticcheck ./...`, and `go test ./...` with its touched package(s) at ≥85%
coverage on its own — per CLAUDE.md's Definition of Done, applied per-commit, per this module's
existing bisectability convention):

1. **`feat(regconfig): add IntField helper`**
   Files: `providers/internal/regconfig/regconfig.go`, `regconfig_test.go`.
   Dependencies: none.
   Gate: `go test ./providers/internal/regconfig/...` passes; no other package touched, so the
   rest of the module is unaffected by construction.

2. **`feat(transport): allow Content-Encoding/TTL/Urgency/Topic outbound headers`**
   Files: `transport/wrapper.go` (`sanitizeOutboundHeaders` only), `transport/wrapper_test.go`.
   Dependencies: none (independent of commit 1).
   Gate: full `go test ./...` — every existing provider's tests must still pass unchanged,
   demonstrating the allowlist addition is non-breaking.

3. **`feat(webpush): add RFC 8291 aes128gcm payload encryption`**
   Files: `providers/webpush/encrypt.go`, `encrypt_test.go` (new package).
   Dependencies: none (pure crypto, no registry/transport wiring; tested directly via the
   unexported `encryptAES128GCMWithKeys` seam).
   Gate: `go test ./providers/webpush/...`; **the RFC 8291 Appendix A fixed-vector test passing is
   a blocking condition for this commit**, not a nice-to-have — do not proceed to commit 4 with it
   failing, skipped, or weakened to a round-trip-only check.

4. **`feat(webpush): add VAPID JWT signing (RFC 8292) and GenerateVAPIDKeyPair`**
   Files: `providers/webpush/vapid.go`, `vapid_test.go`.
   Dependencies: commit 3 only in the sense of sharing a package (no code dependency between
   `encrypt.go` and `vapid.go` themselves). Critically, `buildVAPIDHeader` takes its three VAPID
   values as plain `string` parameters, not a `Config` (§3.5) — `Config` isn't defined until commit
   5, so this commit must not (and per §3.5's signature, does not) reference it. This is what makes
   commit 4 buildable and testable in isolation, satisfying this list's own per-commit build/test
   guarantee.
   Gate: `go test ./providers/webpush/...`.

5. **`feat(webpush): add Config/Client/New/Send`**
   Files: `providers/webpush/webpush.go`, `webpush_test.go`.
   Dependencies: commits 2 (header allowlist), 3 (`encryptAES128GCM`), 4 (`buildVAPIDHeader`, called
   here with `cfg`'s fields unpacked into positional strings — this is the first commit where a
   `Config` value exists to unpack) — first commit where the full `Send` path is exercised
   end-to-end against a `capturingRoundTripper`.
   Gate: `go test ./providers/webpush/...`; this is the commit where §3.8's full error-handling
   table (including the VAPID-subject-prefix and `MaxPlaintextSize` rows) gets its test coverage.

6. **`feat(webpush): register provider and wire into providers/all`**
   Files: `providers/webpush/register.go`, `register_test.go`, `providers/all/all.go`,
   `providers/all/all_test.go` (`wantProviderCount` bump).
   Dependencies: commit 5.
   Gate: full `go test ./...`; `notify.New("webpush", ...)` and `notify.RegisteredTypes()` both
   exercise the new provider end-to-end for the first time.

7. **`docs: document providers/webpush`**
   Files: `README.md`, `docs/INTEGRATION.md`, `ARCHITECTURE.md` (§4 above).
   Dependencies: commit 6 (documents the shipped API, not a design still in flux).
   Gate: no code changes — build/vet/test gates are a no-op pass-through, included for
   completeness/bisectability symmetry with every other commit in this list.

No commit adds a provider other than `webpush`, per §1.3.

---

## 7. Acceptance Criteria

Mapped to this repo's Definition of Done (CLAUDE.md) plus feature-specific criteria:

1. `go build ./...` succeeds at every commit in §6, not just the final one.
2. `go vet ./...` and `staticcheck ./...` clean at every commit.
3. `go test ./...` passes at every commit; coverage ≥85% for every package touched
   (`providers/internal/regconfig`, `transport`, `providers/webpush`, `providers/all`).
4. Every new/changed exported identifier has a doc comment (§3's drafted comments, transcribed
   verbatim — no placeholder comments).
5. `grep -r "Wikid82/charon" --include=*.go .` (or equivalent) returns nothing under
   `providers/webpush/` or any file touched by this feature.
6. The RFC 8291 Appendix A fixed-vector test in `encrypt_test.go` passes with exact-byte
   equality — not a round-trip-only check.
7. `providers/all/all_test.go`'s `TestAll_RegistersEveryBuiltInProvider` passes with the bumped
   `wantProviderCount`.
8. `README.md`, `docs/INTEGRATION.md`, and `ARCHITECTURE.md` are updated per §4.
9. No provider package other than `providers/webpush` is added, scaffolded, or stubbed anywhere in
   this work (scope discipline, §1.3).
10. `notify.New("webpush", map[string]any{...})` and the typed `webpush.New(webpush.Config{...},
    wrapper)` constructor are both exercised by tests and behaviorally equivalent (mirrors every
    other provider's `register_test.go` round-trip pattern).
