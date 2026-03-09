#!/usr/bin/env bash
set -euo pipefail

if command -v pnpm >/dev/null 2>&1; then
  pnpm_cmd=(pnpm)
elif command -v corepack >/dev/null 2>&1; then
  pnpm_cmd=(corepack pnpm)
else
  echo "pnpm or corepack is required for TypeScript tests" >&2
  exit 1
fi

"${pnpm_cmd[@]}" -r --if-present test

if command -v go >/dev/null 2>&1; then
  (cd server && go test ./...)
  (cd sdk-go && go test ./...)
else
  echo "go is required for Go tests" >&2
  exit 1
fi

if command -v uv >/dev/null 2>&1; then
  (cd sdk-python && uv run pytest -q)
else
  echo "uv is required for Python tests" >&2
  exit 1
fi
