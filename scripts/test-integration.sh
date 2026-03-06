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

echo "No Go or Python integration suites are defined yet (M1 scope)."
