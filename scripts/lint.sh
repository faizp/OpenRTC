#!/usr/bin/env bash
set -euo pipefail

if command -v go >/dev/null 2>&1; then
  unformatted="$(cd server && gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "gofmt reported unformatted server files:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
  (cd server && go vet ./...)
else
  echo "go is required for Go lint checks" >&2
  exit 1
fi
