#!/usr/bin/env bash
set -euo pipefail

if command -v pnpm >/dev/null 2>&1; then
  pnpm_cmd=(pnpm)
elif command -v corepack >/dev/null 2>&1; then
  pnpm_cmd=(corepack pnpm)
else
  echo "pnpm or corepack is required for TypeScript type checks" >&2
  exit 1
fi

"${pnpm_cmd[@]}" -r --if-present typecheck

if command -v go >/dev/null 2>&1; then
  (cd sdk-go && go test ./...)
else
  echo "go is required for Go type checks" >&2
  exit 1
fi

if command -v uv >/dev/null 2>&1; then
  (cd sdk-python && uv run mypy src)
else
  echo "uv is required for Python type checks" >&2
  exit 1
fi
