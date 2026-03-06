#!/usr/bin/env bash
set -euo pipefail

if command -v pnpm >/dev/null 2>&1; then
  pnpm_cmd=(pnpm)
elif command -v corepack >/dev/null 2>&1; then
  pnpm_cmd=(corepack pnpm)
else
  echo "pnpm or corepack is required for TypeScript lint checks" >&2
  exit 1
fi

"${pnpm_cmd[@]}" -r --if-present lint

if command -v go >/dev/null 2>&1; then
  unformatted="$(cd sdk-go && gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "gofmt reported unformatted files:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  (cd sdk-go && go vet ./...)
else
  echo "go is required for Go lint checks" >&2
  exit 1
fi

if command -v uv >/dev/null 2>&1; then
  (cd sdk-python && uv run ruff check .)
else
  echo "uv is required for Python lint checks" >&2
  exit 1
fi
