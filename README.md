# OpenRTC

OpenRTC is a self-hosted OSS realtime layer for SaaS teams.

## Monorepo layout

- `server/`: Go core backend module. It builds `openrtc-runtime` and `openrtc-admin`.
- `packages/client/`: TypeScript WebSocket client for rooms, events, and presence.
- `packages/react/`: React hooks for room state, presence, and broadcast events.
- `packages/rich-text/`: Yjs binding helpers plus presence adapters for Tiptap, Lexical, and BlockNote selection/cursor state.
- `packages/yjs/`: Yjs provider for binary update/snapshot sync plus an awareness bridge over OpenRTC presence.
- `packages/yjs-compactor/`: Trusted Yjs update compactor for Redis-backed document retention.
- `reference-app/`: production-style reference app (M5).
- `docs/`: protocol, contracts, config, release, and engineering docs.

## Deployment model

- One Go image is built from `server/`.
- The image runs either `openrtc-runtime` or `openrtc-admin` via command/args.
- `openrtc-runtime` owns WebSocket traffic, room state, access-grant checks, presence, Yjs sync, limits, and cluster fan-out.
- `openrtc-admin` owns room metadata/access grants, storage documents/patches, durable threads/comments, inbox notifications/settings, active-user reads, publish, presence, stats, and admin HTTP endpoints.

## Developer commands

- `make lint`
- `make typecheck`
- `make test`
- `make test-integration`
- `make check`
