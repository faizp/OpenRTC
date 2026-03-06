# OpenRTC

OpenRTC is a self-hosted OSS realtime layer for SaaS teams.

## Monorepo layout

- `server/`: realtime gateway/runtime implementation.
- `sdk-ts/`: TypeScript client SDK.
- `sdk-go/`: Go publisher SDK.
- `sdk-python/`: Python publisher SDK.
- `reference-app/`: production-style reference app (M5).
- `docs/`: protocol, contracts, config, release, and engineering docs.

## Developer commands

- `make lint`
- `make typecheck`
- `make test`
- `make test-integration`
- `make check`
