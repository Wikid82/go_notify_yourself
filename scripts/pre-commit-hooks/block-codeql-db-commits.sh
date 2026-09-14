#!/usr/bin/env bash
set -euo pipefail

# Prevent accidentally committing a local CodeQL database directory (produced
# by running the CodeQL CLI locally per CLAUDE.md's CI/Release troubleshooting
# notes). Adapted from Charon's block-codeql-db-commits.sh — this repo has no
# data/backups/ path to exclude.
staged=$(git diff --cached --name-only | tr '\r' '\n' || true)
if [ -n "${staged}" ]; then
  filtered=$(echo "$staged" | grep -v '^scripts/pre-commit-hooks/' || true)
  if echo "$filtered" | grep -q "codeql-db"; then
    echo "Error: Attempting to commit CodeQL database artifacts (codeql-db)." >&2
    echo "These should not be committed. Remove them or add to .gitignore and try again." >&2
    exit 1
  fi
fi
exit 0
