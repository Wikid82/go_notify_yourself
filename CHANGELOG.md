# Changelog

All notable changes to this project are documented in this file, driven by
[Conventional Commits](https://www.conventionalcommits.org/) and generated
via [GoReleaser](https://goreleaser.com/) on every tagged release.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-08-17

Self-registering provider factory registry, matching the `database/sql`
driver idiom:

- `notify.Register`, `notify.New`, `notify.RegisteredTypes` (`factory.go`):
  a thread-safe, name-keyed factory registry at the module root. `Register`
  panics on a nil factory or duplicate name (programmer error, caught at
  `init()` time); `New` returns an error — never panics — for an
  unregistered provider type.
- All eight existing provider packages (`discord`, `slack`, `gotify`,
  `pushover`, `ntfy`, `telegram`, `webhook`, `email`) now self-register via
  `init()` in a new `register.go`, adapting a generic `map[string]any`
  config into each package's typed `Config`. `email`'s factory takes its
  non-serializable `Mailer`/`TemplateRenderer`/`TemplateName` values
  directly out of the map rather than JSON-decoding them.
- `providers/all`: a new blank-import bundle package
  (`import _ ".../providers/all"`) that registers all eight built-in
  providers in one line, for consumers wanting zero-touch discovery.
- `ARCHITECTURE.md` (new), a new README "Provider registry" section, and
  `docs/INTEGRATION.md` (new): documentation for adding a provider to the
  registry and for integrating this module into another project.

**Behavior change, not just an addition**: importing any provider package
now has an `init()`-time side effect (registering into the global
`notify` registry) it didn't have before, and two packages registering the
same provider name in one binary now panics at startup. No existing
exported signature (`Config`, `New(cfg, w)`, `Send`) changed — this is why
the bump is `v0.2.0`, a minor rather than a patch release, despite being
purely additive at the Go-API level.

## [0.1.0] - 2026-08-14

Initial extraction of the notification delivery engine into a standalone
module:

- `transport.Wrapper`: SSRF-safe outbound HTTP dispatch with retry/backoff,
  redirect re-validation, response-size caps, and a conservative built-in
  default `URLValidator`.
- `notify.Message` / `notify.Sender`: the generic, provider-agnostic
  notification API.
- Seven HTTP provider packages: `providers/discord`, `providers/slack`,
  `providers/gotify`, `providers/pushover`, `providers/ntfy`,
  `providers/telegram`, `providers/webhook`.
- `providers/email`: `Mailer`/`TemplateRenderer` interfaces with one
  neutral, unbranded built-in HTML template.
