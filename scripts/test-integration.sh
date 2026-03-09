#!/usr/bin/env bash
set -euo pipefail

if command -v pnpm >/dev/null 2>&1; then
  pnpm_cmd=(pnpm)
elif command -v corepack >/dev/null 2>&1; then
  pnpm_cmd=(corepack pnpm)
else
  echo "pnpm or corepack is required for TypeScript integration tests" >&2
  exit 1
fi

"${pnpm_cmd[@]}" -r --if-present test:integration

if command -v go >/dev/null 2>&1; then
  (cd server && go test ./integration/...)
else
  echo "go is required for Go integration tests" >&2
  exit 1
fi

echo "No Python integration suite is defined yet."
