#!/usr/bin/env bash
# Pre-commit hook helper: runs golangci-lint if available on the host;
# otherwise prints a hint to use the dev container.
set -euo pipefail

if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run --new-from-rev=HEAD~1
else
  echo "golangci-lint not on host; run inside dev container with: make lint"
fi
