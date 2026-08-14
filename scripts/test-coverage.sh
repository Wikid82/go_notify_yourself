#!/usr/bin/env bash
# Runs the full test suite with coverage and enforces a minimum coverage
# threshold on every package that has test files. Mirrors the intent of
# Charon's own coverage gate, scaled down for this repo's size.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

MIN_COVERAGE="${NOTIFY_MIN_COVERAGE:-85}"
COVERAGE_FILE="$ROOT_DIR/coverage.out"

echo "Running tests with coverage (minimum ${MIN_COVERAGE}%)..."

# Only packages with at least one _test.go file are included — packages with
# no tests (e.g. a pure-interface file) shouldn't fail the gate for lacking
# coverage they have nothing to cover.
PKGS_WITH_TESTS=()
while IFS= read -r pkg; do
    dir="${pkg#github.com/Wikid82/go_notify_yourself}"
    dir=".${dir}"
    if compgen -G "${dir}/*_test.go" > /dev/null 2>&1; then
        PKGS_WITH_TESTS+=("$pkg")
    fi
done < <(go list ./...)

if [[ ${#PKGS_WITH_TESTS[@]} -eq 0 ]]; then
    echo "No packages with tests found; nothing to check."
    exit 0
fi

go test -coverprofile="$COVERAGE_FILE" -covermode=atomic "${PKGS_WITH_TESTS[@]}"

echo ""
echo "Per-function coverage:"
go tool cover -func="$COVERAGE_FILE"

# go tool cover -func reports per-function lines; the last line is the total.
TOTAL_LINE=$(go tool cover -func="$COVERAGE_FILE" | tail -1)
TOTAL_PCT=$(echo "$TOTAL_LINE" | awk '{print $3}' | tr -d '%')

echo "$TOTAL_LINE"

if [[ -z "$TOTAL_PCT" ]]; then
    echo "Could not determine total coverage." >&2
    exit 1
fi

# Compare using awk for float comparison (bash has no native float compare).
FAILED=0
if awk -v cov="$TOTAL_PCT" -v min="$MIN_COVERAGE" 'BEGIN { exit !(cov + 0 < min + 0) }'; then
    echo ""
    echo "FAIL: total coverage ${TOTAL_PCT}% is below the minimum ${MIN_COVERAGE}%." >&2
    FAILED=1
fi

if [[ "$FAILED" -ne 0 ]]; then
    exit 1
fi

echo ""
echo "OK: total coverage ${TOTAL_PCT}% meets the minimum ${MIN_COVERAGE}% threshold."
