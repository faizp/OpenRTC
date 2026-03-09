# OpenRTC

OpenRTC is a self-hosted OSS realtime layer for SaaS teams.

## Monorepo layout

- `server/`: Go core backend module. It builds `openrtc-runtime` and `openrtc-admin`.
- `sdk-ts/`: TypeScript integration SDK for WebSocket clients and thin admin HTTP helpers.
- `sdk-go/`: Go integration SDK for thin admin HTTP/publisher calls.
- `sdk-python/`: Python integration SDK for thin admin HTTP/publisher calls.
- `reference-app/`: production-style reference app (M5).
- `docs/`: protocol, contracts, config, release, and engineering docs.

## Deployment model

- One Go image is built from `server/`.
- The image runs either `openrtc-runtime` or `openrtc-admin` via command/args.
- `openrtc-runtime` owns WebSocket traffic, room state, presence, limits, and cluster fan-out.
- `openrtc-admin` owns publish/stats/admin HTTP endpoints.

## Developer commands

- `make lint`
- `make typecheck`
- `make test`
- `make test-integration`
- `make check`
