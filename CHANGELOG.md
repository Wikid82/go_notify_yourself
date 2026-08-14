# Changelog

All notable changes to this project are documented in this file, driven by
[Conventional Commits](https://www.conventionalcommits.org/) and generated
via [GoReleaser](https://goreleaser.com/) on every tagged release.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
