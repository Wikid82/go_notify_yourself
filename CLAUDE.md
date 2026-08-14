# go_notify_yourself — Claude Code Instructions

Do NOT use worktrees. Make all changes directly on the current working branch.

## Big Picture

A standalone, dependency-free Go module for notification delivery: SSRF-safe outbound HTTP
dispatch with retries, and a per-provider `Sender` interface (Discord, Slack, Gotify, Pushover,
Ntfy, webhook, Telegram, email). Extracted from Charon (`github.com/Wikid82/charon`) so Charon and
future projects can `go get` it instead of re-implementing notification delivery each time.

Long-term direction: a Go equivalent of [Apprise](https://github.com/caronc/apprise) — one common
interface across a large, open-ended catalog of notification services. **Near-term**: while Charon
and the maintainer's other project are still under active development, the provider list stays
exactly what's ported from Charon today. Do not add new provider integrations (Twilio, PagerDuty,
Matrix, etc.) without an explicit ask — the API is designed so that's additive later, not free rein
now.

## Non-negotiable design rule

**This module must never import anything from `github.com/Wikid82/charon/*`.** It has no GORM, no
database, no HTTP framework, no Charon config/env assumptions. Every point where the module needs
something environment-specific (an HTTP client, a URL validator, an SMTP sender, a template) is a
constructor-injected interface (`ClientFactory`, `URLValidator`, `Mailer`, `TemplateRenderer`, ...),
supplied by the host application. If you find yourself wanting to reach for a Charon package,
that's a sign the seam is wrong — stop and reconsider the interface instead.

## Workflow

- **Run**: `go run ./...` (library — no `main` package at the root; use `go build ./...` /
  `go vet ./...` to verify).
- **Test**: `go test ./...` from repo root. TDD: write the failing test first, then the
  implementation (Red/Green).
- **Coverage**: minimum 85% per package, enforced by `scripts/test-coverage.sh` (mirrors the
  intent of Charon's gate, scaled to this repo — create this script during scaffolding if it
  doesn't exist yet).
- **Lint**: `go vet ./...` and `staticcheck ./...` must be clean before a commit is considered done.
- **JSON/exported API**: keep the public surface (`notify.Message`, `notify.Sender`,
  `transport.Wrapper`, `providers/*`) intentionally small and documented with doc comments — this
  is a library other repos import, so breaking exported signatures is a breaking change for every
  consumer, not just Charon.

## Definition of Done (per commit)

1. `go build ./...` succeeds.
2. `go vet ./...` and `staticcheck ./...` clean.
3. `go test ./...` passes, coverage ≥ 85% for the package(s) touched.
4. New/changed exported identifiers have doc comments.
5. No import of `github.com/Wikid82/charon/*` anywhere in the module (grep to confirm if unsure).

## What this repo deliberately does NOT have

Scaled down from Charon's much larger surface because none of it applies to a small,
dependency-free Go library with a single maintainer:

- No CodeQL / Trivy / GORM security scans — no SQL, no web-facing surface of its own.
- No Playwright / E2E — no frontend, no UI.
- No Docker build — this ships as a Go module via `go get`, not a binary or image.
- No multi-agent orchestration pipeline — for a repo this size, direct TDD implementation is the
  right amount of process. Don't build out a Management/Planning/Supervisor agent roster here; it
  would be process for its own sake at this scale.

## CI / Release

- CI (`.github/workflows/ci.yml`): `go build`, `go vet`, `staticcheck`, `go test` + coverage gate,
  on every push/PR. Nothing heavier.
- Release: GoReleaser (`.goreleaser.yaml`), tag-triggered, changelog + GitHub release only — no
  binary/archive/Docker artifacts (this is a library, not a deployable).
- Versioning: semver tags (`vX.Y.Z`), driven by Conventional Commits (`feat:`, `fix:`, `chore:`,
  `refactor:`, `docs:` — same convention as Charon, for the maintainer's own consistency).

## Commit Conventions

Same as Charon: `feat:`, `fix:`, `chore:`, `refactor:`, `docs:` prefixes. One logical change per
commit; each commit should build and pass tests on its own (bisectable).

## Source of Truth for Scope

`docs/plans/notifications_extraction_spec.md` in the Charon repo (`/projects/Charon`) is the
design brief this module was extracted from — provider inventory, public API shapes, DI seam
design, and commit slicing all originate there. Consult it for the "why" behind this repo's
structure rather than re-deriving decisions already made.
