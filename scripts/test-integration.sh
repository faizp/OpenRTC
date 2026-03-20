#!/usr/bin/env bash
set -euo pipefail

if command -v go >/dev/null 2>&1; then
  (cd server && go test ./integration/...)
else
  echo "go is required for Go integration tests" >&2
  exit 1
fi
