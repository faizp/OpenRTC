#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/openrtc-coverage.XXXXXX")"
trap 'rm -rf "$tmpdir"' EXIT

go_min="${OPENRTC_GO_COVERAGE_MIN:-80.0}"
ts_min="${OPENRTC_TS_EXPORT_COVERAGE_MIN:-90.0}"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required for coverage checks" >&2
  exit 1
fi

(
  cd "$repo_root/server"
  go test ./... -coverprofile="$tmpdir/go-cover.out" -count=1
)

go_total="$(go tool cover -func="$tmpdir/go-cover.out" | awk '/^total:/ { gsub("%", "", $3); print $3 }')"
awk -v actual="$go_total" -v minimum="$go_min" 'BEGIN {
  if ((actual + 0) < (minimum + 0)) {
    printf("Go statement coverage %.1f%% is below %.1f%%\n", actual, minimum) > "/dev/stderr";
    exit 1;
  }
}'
printf 'Go statement coverage %.1f%% meets %.1f%% threshold\n' "$go_total" "$go_min"

"$repo_root/scripts/pnpm.sh" -r --if-present test
node "$repo_root/scripts/api-coverage.mjs" "$ts_min"
