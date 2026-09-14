#!/usr/bin/env bash
set -euo pipefail

# Wrapper for staticcheck so lefthook works the same whether or not the
# binary is already on PATH (mirrors the resolve-or-install pattern Charon
# uses for golangci-lint, scaled down to this repo's single linter).

preferred_bin="${GOBIN:-${GOPATH:-$HOME/go}/bin}/staticcheck"

resolve_staticcheck() {
    if command -v staticcheck >/dev/null 2>&1; then
        command -v staticcheck
        return 0
    fi
    if [[ -x "$preferred_bin" ]]; then
        printf '%s\n' "$preferred_bin"
        return 0
    fi
    return 1
}

if ! STATICCHECK="$(resolve_staticcheck)"; then
    echo "staticcheck not found — installing..." >&2
    go install honnef.co/go/tools/cmd/staticcheck@latest >&2
    if ! STATICCHECK="$(resolve_staticcheck)"; then
        echo "ERROR: failed to install staticcheck" >&2
        echo "PATH: $PATH" >&2
        exit 1
    fi
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

"$STATICCHECK" ./...
