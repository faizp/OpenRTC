#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
package_json="$repo_root/package.json"

expected_pnpm_version="$(node -e "const pm = require(process.argv[1]).packageManager || ''; console.log(pm.startsWith('pnpm@') ? pm.slice(5) : '')" "$package_json" 2>/dev/null || true)"

if [ -n "$expected_pnpm_version" ] && command -v corepack >/dev/null 2>&1; then
  cd "$repo_root"
  exec corepack pnpm "$@"
fi

if command -v pnpm >/dev/null 2>&1; then
  actual_pnpm_version="$(pnpm --version)"
  if [ -z "$expected_pnpm_version" ] || [ "$actual_pnpm_version" = "$expected_pnpm_version" ]; then
    exec pnpm "$@"
  fi
  echo "pnpm $expected_pnpm_version is required by package.json, but pnpm $actual_pnpm_version was found" >&2
  echo "Install/enable the pinned package manager with: corepack enable" >&2
  exit 1
fi

if command -v corepack >/dev/null 2>&1; then
  cd "$repo_root"
  exec corepack pnpm "$@"
fi

echo "pnpm or corepack is required for TypeScript workspace checks" >&2
exit 1
