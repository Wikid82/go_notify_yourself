#!/usr/bin/env bash
#
# Usage: notify_dep_update.sh
#
# Updates Go dependencies for this module, then runs the same checks CI
# does (build, vet, staticcheck, coverage-gated tests, integration tests,
# govulncheck) so a broken update is caught locally before it's pushed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

GOPATH_BIN="$(go env GOPATH)/bin"
export PATH="$GOPATH_BIN:$PATH"

cd "$REPO_ROOT" || exit 1

echo "============================================================================"
echo "Updating: $REPO_ROOT"
echo "============================================================================"

# Update go/toolchain directives so Renovate's golang updates have nothing to do
go get go@latest toolchain@latest
# -t includes test-only dependencies, which Renovate also tracks
go get -u -t ./...
go mod tidy
go mod verify

# Always (re)install after the toolchain bump above, not just when missing:
# a staticcheck/govulncheck binary built with the old toolchain can fail to
# parse export data from a newer one (e.g. "export data version N is greater
# than maximum supported version"). Installing here picks up the just-bumped
# go directive so the tool is built with a matching toolchain.
go install honnef.co/go/tools/cmd/staticcheck@latest
go install golang.org/x/vuln/cmd/govulncheck@latest

go vet ./...
staticcheck ./...
go build ./...
bash scripts/test-coverage.sh
go test -tags=integration ./...
govulncheck ./...

echo ""
echo "All Go module dependencies updated successfully."
