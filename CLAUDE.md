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

- No Trivy / GORM security scans — no SQL, no web-facing surface of its own.
- No Playwright / E2E — no frontend, no UI. The agent roster below has no `frontend-dev` or
  `playwright-dev` equivalent for the same reason.
- No Docker build — this ships as a Go module via `go get`, not a binary or image. The `devops`
  agent below does not manage containers.

## Orchestration Model

Mirrors Charon's orchestration model, scaled to this repo's surface. There is no separate
"management" wrapper agent — **the main Claude Code session IS the orchestrator.** It delegates
directly to the specialized agents below, reviews their output, and enforces the Definition of
Done, rather than bouncing through an intermediate agent that does the same thing one hop removed.

The orchestrating session is not banned from reading source (`.go`) directly — read whatever's
needed for scoping, verification, or a bounded fix. What still always gets delegated is
implementation: never hand-edit library code yourself.

- **Bounded work** (a well-scoped fix, chore, or CI/docs change to an existing flow — no written
  spec needed): read what you need, then dispatch straight to the one specialist agent that owns
  it (`go-dev`, `devops`, `docs-writer`) with a self-contained prompt. No planning-agent detour
  required.
- **Feature-scale work** (a new exported API, a change to an existing provider's contract, or
  anything that changes the public surface): run the full pipeline —
  1. Delegate to `planning` to research and write `docs/plans/current_spec.md` (with a Commit
     Slicing Strategy).
  2. Delegate to `supervisor` to review the plan; iterate with `planning` until approved.
  3. Present the plan to the user and get explicit approval before implementation begins.
  4. Delegate implementation commit-by-commit to `go-dev` (and `devops` for CI/release-adjacent
     commits); each commit must pass its own validation gate before the next starts.
  5. Delegate to `supervisor` again to review the implementation against the plan — the
     no-Charon-import rule and any public-API change are blocking findings, not suggestions.
  6. Delegate to `qa-security` last — after every other change has landed — to run the lint/
     coverage/security gates and write `docs/reports/qa_report.md`. Loop back to step 1 if it finds
     blocking issues.
  7. Delegate to `docs-writer` for README/INTEGRATION.md/doc-comment updates, then summarize the
     work and provide the final conventional-commit message.

**Team roster** (`.claude/agents/`):

- **planning** — Principal Architect; writes `docs/plans/current_spec.md`.
- **supervisor** — Code Review Lead; reviews plans and implementations (read-only). Treats a
  `github.com/Wikid82/charon/*` import and any undisclosed exported-API break as blocking.
- **go-dev** — Senior Go Engineer; implements providers, transport/retry logic, and factory wiring
  (strict TDD, Red/Green).
- **qa-security** — QA & Security Engineer; lint/coverage gates, SSRF/URL-validation and
  retry-behavior review, writes `docs/reports/qa_report.md`. Always runs last.
- **devops** — CI/CD specialist for the GitHub Actions workflows, GoReleaser, and Renovate — no
  Docker, no deployable artifact.
- **docs-writer** — Technical writer for `README.md`, `docs/INTEGRATION.md`, and doc comments,
  aimed at the Go engineer integrating this module — not an end-user audience.

**Rules carried over from Charon's pipeline:**
- When multiple implementation options exist, prefer the long-term fix over a quick patch.
- Parallelize independent delegations freely, but never dispatch a second implementation pass onto
  files a previous delegation's `qa-security` review is still validating — let one delegation,
  including its QA, fully land before starting the next one on the same files.
- Every subagent prompt that involves running commands must explicitly instruct it to run them in
  the foreground/blocking (see "Execution Discipline" below) — state it in the dispatch prompt
  itself, don't assume the subagent already knows.

## Execution Discipline: Foreground-Only Commands (MANDATORY)

**All agents — the orchestrating session and every subagent — MUST run commands in the foreground
and block until they complete.** Never background a long-running command (`run_in_background:
true`, `&`, `nohup`, or any detached/async invocation) and end your turn to "check back later" or
"wait for the notification."

**Why:** Backgrounding a command and pausing your turn to wait for it does not reliably resume
you. Ending a turn on that assumption leaves whoever dispatched the work waiting on a result that
never arrives on its own.

**Rule:**
- Run `go build`, `go vet`, `staticcheck`, `go test`, `scripts/test-coverage.sh`, and integration
  tests as blocking, foreground calls with a generous timeout.
- If a command genuinely needs longer than a single call's timeout, re-issue a blocking wait within
  your own turn until you have a real result. Do not end your turn assuming something else will
  wake you back up.
- If a call auto-backgrounds anyway (the tool's own timeout forces this): that is NOT permission to
  end your turn and wait for a notification. Immediately re-attach to it in the same turn until you
  have a real result.
- Never report a task as "running, will report when it lands" and then go idle.

## CI / Release

- CI (`.github/workflows/ci.yml`): `go build`, `go vet`, `staticcheck`, `go test` + coverage gate,
  on every push/PR. Nothing heavier.
- CodeQL (`.github/workflows/codeql.yml`): `go` only — this repo has no JS/TS source, so don't
  re-add `javascript-typescript` to the matrix. Go setup points at the root `go.mod`/`go.sum`
  (unlike Charon, there is no `backend/` subdirectory here). Findings gate logic lives in
  `scripts/security/codeql-findings-gate.sh`; documented exceptions go in
  `.github/codeql/codeql-suppressions.yml`.
  **Known gap (as of 2026-08-21):** CodeQL's bundled Go extractor trails the `go 1.27.0` directive
  in `go.mod`, so extraction fails for every file and the scan finds nothing real — the job still
  goes green because "0 findings" and "extraction failed" look identical unless you check for it.
  The job summary now carries a permanent note pointing at the `autobuild` step's log (grep for
  `requires newer Go version`) as the way to confirm real coverage for a given run — don't trust
  the pass/fail color alone until GitHub ships a CodeQL bundle whose Go extractor supports 1.27.
- Release: GoReleaser (`.goreleaser.yaml`), tag-triggered, changelog + GitHub release only — no
  binary/archive/Docker artifacts (this is a library, not a deployable).
- Versioning: semver tags (`vX.Y.Z`), driven by Conventional Commits (`feat:`, `fix:`, `chore:`,
  `refactor:`, `docs:` — same convention as Charon, for the maintainer's own consistency).

## Commit Conventions

Same as Charon: `feat:`, `fix:`, `chore:`, `refactor:`, `docs:` prefixes. One logical change per
commit; each commit should build and pass tests on its own (bisectable).

Dependency bumps use `deps:` (Renovate emits this automatically —
`.github/renovate.json` → `semanticCommitType: deps`). `deps:` is a release-triggering
prefix for release-please, so a bumped transitive dep cuts a patch release and reaches
downstream consumers — that's the intent. GitHub Actions bumps stay `chore:` (CI-only,
non-releasable). Only `feat:`, `fix:`, `perf:`, `deps:`, and breaking changes trigger a
release; `chore:` / `ci:` / `docs:` do not.

## Source of Truth for Scope

`docs/plans/notifications_extraction_spec.md` in the Charon repo (`/projects/Charon`) is the
design brief this module was extracted from — provider inventory, public API shapes, DI seam
design, and commit slicing all originate there. Consult it for the "why" behind this repo's
structure rather than re-deriving decisions already made.
