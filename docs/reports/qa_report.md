# QA / Security Report — `providers/webpush` (GitHub issue #14)

- **Scope**: `providers/webpush`, `providers/internal/regconfig` (`IntField`), `transport`
  (`sanitizeOutboundHeaders` allowlist), `providers/all` (registration wiring), and the
  documentation updates (`README.md`, `docs/INTEGRATION.md`, `ARCHITECTURE.md`) shipped across
  commits `1205a98`..`8c34939` on `development`.
- **Reviewer**: QA/Security gate (final stage of the plan → implement → review pipeline).
- **Reference**: `docs/plans/current_spec.md` (full spec read, including §3.8's error-handling
  table and §7's acceptance criteria).
- **Verdict: PASS WITH TWO NON-BLOCKING FINDINGS.** Build/vet/staticcheck/test/coverage gates are
  all green, the no-Charon-import gate is clean, and the RFC 8291 fixed-vector test is exact-byte
  and passing. Two findings below (one MEDIUM, one LOW) are real, reproduced defects in
  error-message/example-code hygiene around credential-equivalent values — neither blocks a merge
  by itself (both require a genuinely malformed/misconfigured input to trigger, and neither is the
  runtime's normal/happy path), but both should be fixed before this ships to reduce the chance of
  a `PushSubscription.endpoint`'s bearer-token-equivalent path segment ending up in a log.

---

## 1. Gate results

| Gate | Result |
|---|---|
| `go build ./...` | PASS |
| `go vet ./...` | PASS (clean) |
| `staticcheck ./...` | PASS (clean) |
| `go test ./...` | PASS, all packages |
| `grep -r "Wikid82/charon" --include=*.go .` | **0 hits** — blocking gate clean |
| `scripts/test-coverage.sh` (repo-wide) | PASS — 94.6% (floor 85%) |
| `providers/webpush` package coverage | 89.6% (floor 85%, +4.6pt margin) |
| `transport` package coverage | 94.2% |
| `providers/internal/regconfig` package coverage | 100.0% |
| `providers/all` | No statements of its own (blank-import file); exercised via `providers/all/all_test.go`'s `TestAll_RegistersEveryBuiltInProvider`, which passes with `wantProviderCount = 9` |
| RFC 8291 Appendix A fixed-vector test | PASS, exact-byte (`TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector` + a header-only cross-check) — not weakened to round-trip-only |
| Scope discipline (§1.3/§9 of spec) | No provider other than `webpush` added; diff touches exactly the files the spec's commit-slicing plan (§6) called for, nothing else |

Diff confirmed via `git diff --stat 1205a98~1..8c34939`: 17 files, all within the spec's declared
scope (`ARCHITECTURE.md`, `README.md`, `docs/INTEGRATION.md`, `providers/all/{all,all_test}.go`,
`providers/internal/regconfig/*`, `providers/webpush/*`, `transport/{wrapper,wrapper_test}.go`).
No CI/CodeQL/release config touched.

---

## 2. Security review

### 2.1 SSRF surface — re-verified independently, not taken on faith

Per this task's explicit instruction to re-verify Supervisor's "zero diff" claim rather than trust
it: confirmed by direct diff (`git diff 1205a98..99549d1 -- transport/`) that commit `99549d1`
("feat(transport): allow Content-Encoding/TTL/Urgency/Topic outbound headers") touches **only**
`sanitizeOutboundHeaders`'s `allowed` map in `transport/wrapper.go`, adding
`content-encoding`/`ttl`/`urgency`/`topic`. `transport/validate_default.go`
(`DefaultURLValidator`, `isPrivateIP`, `isAllowedIP`) and `hasDisallowedQueryAuthKey` in
`transport/wrapper.go` have **zero lines changed** anywhere in the webpush feature's commit range.
Confirmed clean, independently.

The four newly-allowed headers are simple metadata (`Content-Encoding: aes128gcm`, a numeric
`TTL`, an `Urgency` enum, and a `Topic` string already regex-constrained by `webpush.go`'s
`topicPattern`) — none of them affect host resolution, redirect handling, or the request line, so
this is not a vector for request smuggling or host-header injection. `TestSanitizeOutboundHeadersAllowsWebPushHeaders`
proves the change is additive (an unlisted header, `X-Foo`, is still stripped) rather than a
wholesale relaxation.

### 2.2 Retry/backoff — no new amplification vector

`transport/retry.go` is untouched by this feature. `NewWrapper`'s default `RetryPolicy` (3
attempts, 200ms/2s capped exponential backoff, jitter via `crypto/rand`) is unchanged and
applies to webpush requests identically to every other provider. `shouldRetry` does not retry on
4xx (a push service's 404/410 "subscription gone" is surfaced once, not retried — matches spec
§3.8's table). No unbounded loop, no provider-specific retry override introduced.

### 2.3 Cryptography surface (new ground for this module)

- **Randomness**: every place randomness is required uses `crypto/rand`, not `math/rand`:
  `ecdh.P256().GenerateKey(rand.Reader)` (ephemeral ECDH keypair, `encrypt.go:53`), `rand.Read(salt)`
  (16-byte salt, `encrypt.go:59`), `ecdsa.GenerateKey(elliptic.P256(), rand.Reader)`
  (`GenerateVAPIDKeyPair`, `vapid.go:107`), and `ecdsa.Sign(rand.Reader, ...)` (VAPID JWT signing,
  `vapid.go:82`). Confirmed via direct read — no `math/rand` import anywhere in
  `providers/webpush`, no test-only randomness path reachable from a production call (the
  deterministic `encryptAES128GCMWithKeys` seam is only ever called from the production
  `encryptAES128GCM` with freshly-generated random inputs; tests call the seam directly with fixed
  RFC vectors, which is the intended, documented test-only use).
- **VAPID JWT `exp` bound**: confirmed `vapidJWTLifetime = 12 * time.Hour` (`vapid.go:21`) and
  `Exp: time.Now().Add(vapidJWTLifetime).Unix()` (`vapid.go:71`) — bounded, comfortably inside RFC
  8292's 24-hour recommended maximum, and re-signed fresh on every `Send` call (no caching/reuse
  that could let a stale-but-still-valid token linger unnecessarily). `TestBuildVAPIDHeader_ProducesValidSignedJWT`
  asserts the `exp` claim lands within 5 seconds of the expected value.
- **`ecdsa.Sign` not `ecdsa.SignASN1`**: confirmed (`vapid.go:82`), with manual raw `r||s`
  left-zero-padded 32+32-byte encoding (`vapid.go:87-90`) — the correct JOSE/JWS ES256 shape, not
  DER. `TestBuildVAPIDHeader_ProducesValidSignedJWT` round-trips this through `ecdsa.Verify`
  against the reconstructed `big.Int`s, so this isn't just "compiles," it's verified-correct.
- **`MaxPlaintextSize = 3800` enforcement — fail-fast, confirmed by reading the control flow, not
  assumed**: `encryptAES128GCMWithKeys` (`encrypt.go:75-78`) checks `len(plaintext) >
  MaxPlaintextSize` as its **first** statement, before any base64 decoding, ECDH, HKDF, or AES
  work. Both required boundary tests exist and pass:
  `TestEncryptAES128GCMWithKeys_AcceptsPlaintextAtBoundary` (exactly `MaxPlaintextSize` succeeds)
  and `TestEncryptAES128GCMWithKeys_RejectsOversizedPlaintext` (`MaxPlaintextSize+1` fails with the
  named error). `TestClientSend_RejectsOversizedPlaintext` additionally exercises this through the
  full `Send` path. Survived from plan to implementation intact.
- **RFC 8291 Appendix A fixed-vector test**: exact-byte assertion against the RFC's published
  ciphertext (`TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector`), plus an isolated
  header-framing cross-check and a supplementary (explicitly-labeled-as-insufficient-alone)
  self-round-trip test. This is the single highest-value correctness gate in the feature and it is
  intact, not weakened.

### 2.4 Credential-handling review — two findings (below)

Applied the "no provider logs or echoes back a full credential in error messages, test fixtures, or
example code" check to all four flagged fields (`VAPIDPrivateKey`, `Endpoint`, `P256dh`, `Auth`)
plus `GenerateVAPIDKeyPair`'s returned private key.

- `VAPIDPrivateKey`, `P256dh`, `Auth`: **clean.** Every error path that can fire on these fields
  (`webpush.go`'s required-field checks, `encrypt.go`'s base64/length validation,
  `verifyVAPIDKeyPairMatches`'s decode errors) names the *field*, not the *value* — confirmed by
  reading every `fmt.Errorf` call site in `encrypt.go`, `vapid.go`, and `webpush.go`. No `t.Logf`/
  `fmt.Print*`/`log.*` call anywhere in the package touches a real key/secret value. Doc comments
  (`GenerateVAPIDKeyPair`, `VAPIDPrivateKey`) both carry an explicit "don't log this" warning.
- `Endpoint`: **not clean** — see Finding 1 (MEDIUM) and Finding 2 (LOW) below. Both are real,
  reproduced defects, not false positives.

---

## 3. Findings

### Finding 1 (MEDIUM): `buildVAPIDHeader` echoes the raw `Endpoint` — including any embedded
bearer-token-equivalent path segment — into the returned error when the endpoint fails URL parsing

**Location**: `providers/webpush/vapid.go:59-62`

```go
parsedEndpoint, err := neturl.Parse(endpoint)
if err != nil {
    return "", fmt.Errorf("webpush: parse endpoint for VAPID audience: %w", err)
}
```

**Reproduced** (not a guess — ran directly against the package):

```go
secretEndpoint := "https://fcm.googleapis.com/fcm/send/SECRET-BEARER-TOKEN-1234\x7f"
_, err := buildVAPIDHeader(pub, priv, "mailto:ops@example.com", secretEndpoint)
// err.Error() == `webpush: parse endpoint for VAPID audience: parse "https://fcm.googleapis.com/fcm/send/SECRET-BEARER-TOKEN-1234\x7f": net/url: invalid control character in URL`
```

`net/url.Parse`'s own error text embeds the full raw input string it failed to parse. Because
`vapid.go` wraps that error with `%w` instead of a generic message, a malformed `Endpoint` —
itself the "credential-equivalent" field this review was asked to scrutinize (per this task's own
framing: FCM/etc. endpoints commonly carry a bearer-token-equivalent path segment) — ends up
verbatim inside the error `Send` returns to the caller, which a host application is very likely to
log (see Finding 2, which shows the module's own recommended example code doing exactly that).

**Why this matters despite the narrow trigger condition**: the trigger isn't attacker-controlled in
a typical deployment (a host's own stored `Endpoint` would have to become malformed — e.g. DB
corruption, a bad copy-paste, or a buggy upstream `PushSubscription` feed), so this isn't a remote
exploit. But it's a real gap relative to this module's own established convention:
`transport/wrapper.go`'s own destination-URL-parse-failure path (`buildSafeRequestURL`, around
line 267-270) deliberately does **not** wrap the underlying `net/url` error — it returns a generic
`"destination URL validation failed"` specifically to avoid this class of leak. `vapid.go`'s
endpoint-parse error is inconsistent with that precedent inside the same module.

**Remediation** (concrete, minimal):

```go
parsedEndpoint, err := neturl.Parse(endpoint)
if err != nil {
    return "", fmt.Errorf("webpush: endpoint is not a valid URL")
}
```

(Matches `transport/wrapper.go`'s own generic-error convention for this exact class of failure.)

**Severity**: MEDIUM. Not remotely exploitable by a third party in the normal flow, but a genuine,
reproduced instance of exactly the credential-echo pattern this review was asked to hunt for, with
a trivial fix and clear in-module precedent for the correct behavior.

### Finding 2 (LOW): `docs/INTEGRATION.md`'s recommended webpush example logs the full subscriber
`Endpoint` on every send failure

**Location**: `docs/INTEGRATION.md:181-183`

```go
if err := sender.Send(ctx, msg); err != nil {
    log.Printf("webpush to %s failed: %v", sub.Endpoint, err)
}
```

This is the module's own documented "here's how to fan a message out to every subscriber" example
— the pattern a host application is expected to copy. Per this task's framing, `Endpoint` "often
embeds a bearer-token-equivalent path segment for push services like FCM," so the sanctioned
example teaches host authors to put that value in their own logs on every failed delivery
(including the common case where `Send` fails downstream in `transport.Wrapper` for an entirely
unrelated reason, e.g. a transient 503).

**Severity**: LOW. This is documentation/example code, not a runtime code path in the library
itself — it doesn't cause `go_notify_yourself` to leak anything on its own. But it's still exactly
the "example code" surface this review was asked to check, and it actively steers a host toward
logging a credential-equivalent value, which running `go vet`/`staticcheck`/tests cannot catch
since it's prose, not compiled code.

**Remediation** (concrete): log a non-secret identifier instead of the raw endpoint — e.g. a
subscriber ID the host already tracks, or at most the endpoint's *host* (`neturl.Parse(sub.Endpoint).Host`,
which is stable across subscriptions to the same push service and carries no token):

```go
if err := sender.Send(ctx, msg); err != nil {
    log.Printf("webpush to subscriber %s failed: %v", sub.ID, err) // sub.ID, not sub.Endpoint
}
```

---

## 4. Coverage detail (packages touched by this feature)

| Package | Coverage | Notes |
|---|---|---|
| `providers/webpush` | 89.6% | See below — remaining gap is unexported crypto-library error branches, not untested product behavior |
| `transport` | 94.2% | Unchanged by this feature except the additive header-allowlist entries, which are covered |
| `providers/internal/regconfig` | 100.0% | `IntField`'s 5 shape cases (missing key, wrong type, `int`, `int64`, `float64`) all present |
| `providers/all` | N/A — no statements of its own | The file is a pure blank-import list; its *effect* (webpush registered, count = 9) is exercised by `providers/all/all_test.go`, which passes |

`providers/webpush`'s remaining uncovered branches, function by function (`go tool cover -func`
after this review's added tests):

- `encryptAES128GCM` 71.4%, `GenerateVAPIDKeyPair` 70.0%, `mustMarshalJSON` 75.0%: the uncovered
  lines are exclusively `if err != nil` branches immediately following
  `ecdh.P256().GenerateKey(rand.Reader)`, `rand.Read(...)`, `ecdsa.GenerateKey(...)`,
  `priv.Bytes()`/`priv.PublicKey.Bytes()`, and (for `mustMarshalJSON`) `json.Marshal` of a
  fixed, hardcoded `map[string]string` literal. None of these are reachable without fault-injecting
  `crypto/rand` itself or making `encoding/json` fail on a value that cannot fail to marshal — this
  module has (correctly, per its dependency-free/no-extra-DI-seam design) no injectable RNG seam,
  and adding one solely to hit these lines would be test-driven production-code complexity, not a
  real coverage improvement. Consistent with CLAUDE.md's "coverage should not need artificial
  padding tests" guidance from the spec's own Phase 3 notes.
- `verifyVAPIDKeyPairMatches` 86.7%, `encryptAES128GCMWithKeys` 87.8%: improved this session (see
  §5) by adding tests for the base64-*decode-error* branches (invalid characters), which are
  distinct from and previously not covered by the existing wrong-*length*-after-decode tests. The
  remaining gap in these two functions is the same class of practically-unreachable
  `priv.PublicKey.Bytes()`/HKDF/AES-construction error branches as above.
- `buildVAPIDHeader` 91.9%, `Send` 96.4%: high coverage; remaining gaps are the same class of
  effectively-infallible stdlib error branches (`json.Marshal` of a small fixed struct,
  `ecdsa.Sign` failure).

**Assessment**: the aggregate 89.6% (package) / 94.6% (repo-wide) figures are not inflated by
avoiding hard cases — §3.8's full error-handling table (missing fields, malformed VAPID subject
prefix, mismatched keypair, invalid urgency/topic, oversized plaintext, non-JSON custom template,
wrapper/4xx/5xx propagation) is each backed by its own passing test in `webpush_test.go`. The
uncovered remainder is genuinely-infeasible-without-fault-injection stdlib error handling, which is
the correct and expected shape for well-tested Go crypto code — not a coverage gap that should
block this gate.

---

## 5. Tests added by this QA pass

Five new tests added to close real (non-fault-injection) coverage gaps identified during this
review — all pass, none touch production code:

- `providers/webpush/encrypt_test.go`:
  `TestEncryptAES128GCMWithKeys_RejectsInvalidBase64P256dh`,
  `TestEncryptAES128GCMWithKeys_RejectsInvalidBase64Auth` — invalid-base64 (not merely
  wrong-length-after-decode) `p256dh`/`auth` values, distinct branches from the existing malformed
  tests.
- `providers/webpush/webpush_test.go`:
  `TestClientSend_RejectsMalformedVAPIDPrivateKeyBase64`,
  `TestClientSend_RejectsMalformedVAPIDPublicKeyBase64` — malformed VAPID keys reached through the
  real `Client.Send` path (`verifyVAPIDKeyPairMatches`), not only through the lower-level
  `buildVAPIDHeader` seam `vapid_test.go` already exercised.

Net effect: `providers/webpush` package coverage 87.7% → 89.6%. Committed separately (see below);
`docs/reports/qa_report.md` is committed alongside it.

**Note**: Findings 1 and 2 above are **not** fixed by this QA pass — per this agent's role
(testing + vulnerability assessment, reporting actionable findings), production-code and
documentation fixes are left for a follow-up commit rather than made unilaterally outside the
plan → implement → review pipeline this feature went through. Both have concrete, minimal
remediations included above and should be applied before this release ships.

---

## 6. CodeQL caveat (per CLAUDE.md, not this feature's fault)

Per CLAUDE.md's documented known gap: this repo's `go.mod` directive is `go 1.27.1`, and as of this
review CodeQL's bundled Go extractor still trails that version, so a green CodeQL run on this
feature's commits is **not** independently trustworthy evidence of a clean scan — it likely means
"0 findings because extraction failed," not "0 findings because the scan ran and found nothing."
This report's SSRF/crypto/credential-handling conclusions above come from direct code
review and reproduced tests in this session, not from CodeQL. Per CLAUDE.md, confirming real
CodeQL coverage requires checking the `autobuild` step's log for `requires newer Go version` on
this feature's CI run — not done as part of this local review (no CI run was triggered by this
session), flagged here so the maintainer checks it before treating CodeQL as having covered this
feature.

---

## 7. Acceptance criteria cross-check (spec §7)

| # | Criterion | Status |
|---|---|---|
| 1 | `go build ./...` succeeds at every commit | Verified at HEAD; per-commit bisectability not individually re-verified (would require checking out each of the 7 commits) |
| 2 | `go vet`/`staticcheck` clean at every commit | Verified at HEAD |
| 3 | `go test ./...` passes, coverage ≥85% for touched packages | PASS — see §4 |
| 4 | New/changed exported identifiers have doc comments | Verified by direct read of `webpush.go`, `vapid.go`, `encrypt.go` (only `MaxPlaintextSize`, `Config`+fields, `Client`, `New`, `Send`, `DefaultTTL`, `GenerateVAPIDKeyPair` are exported — all documented) |
| 5 | No `Wikid82/charon` import | PASS — 0 hits, confirmed |
| 6 | RFC 8291 fixed-vector test, exact-byte | PASS |
| 7 | `providers/all` `wantProviderCount` bumped, test passes | PASS (9) |
| 8 | README/INTEGRATION/ARCHITECTURE updated | PASS — present, though INTEGRATION.md's example has Finding 2 above |
| 9 | No provider other than `webpush` added | PASS — confirmed via diff scope |
| 10 | `notify.New("webpush", ...)` and `webpush.New(...)` both exercised, behaviorally equivalent | PASS — `register_test.go`'s `TestRegister_NewReturnsWorkingSender` round-trips config through both paths and asserts equality |

---

## 8. Summary for the maintainer

Ship-blocking gates (build/vet/staticcheck/test/coverage/no-Charon-import) are all green. The
feature's core correctness claim — the RFC 8291 exact-byte vector — holds. Two real but
narrow-trigger findings around `Endpoint` handling (MEDIUM: raw endpoint echoed into an error on
malformed-URL input; LOW: the docs example logs the raw endpoint on any send failure) should be
fixed before release; both have a one-line remediation included above. Neither affects the
happy-path or the common 4xx/5xx-from-push-service path already covered by tests.
