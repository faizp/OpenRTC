# OpenRTC Reference App - Dev Console

The reference app is a local dev console for exercising OpenRTC end to end. It
starts a mock JWKS issuer, the OpenRTC runtime, the OpenRTC admin API, and a
browser UI for probing the implemented protocol surfaces.

## What it covers

- Client JWT and admin JWT issuance for local testing.
- JSON WebSocket room join, leave, events, and presence.
- Admin event publish and admin-side presence writes.
- Room CRUD, metadata, access grants, and active-user reads.
- Durable room storage, typed storage roots, and JSON Patch.
- Durable threads, comments, inbox notifications, notification settings, and
  room subscription settings.
- Yjs binary endpoint connect, update write, replay, and client snapshot
  rejection probes.
- Health, readiness, metrics, and aggregate stats checks.

The in-browser Yjs panel is a transport probe for `/yjs/{room}`. CRDT merge
semantics and editor bindings live in the `@openrtc/yjs` and
`@openrtc/rich-text` packages.

## Running locally

Prerequisites: Go 1.18+ and Redis.

```bash
# Start Redis if one is not already running.
docker run -d --name openrtc-redis -p 6379:6379 redis:7-alpine

# Start the reference app from this directory.
cd reference-app
go run ./cmd/server
```

Then open:

```bash
open http://localhost:3000
```

The reference server starts:

- Mock JWKS provider at `http://localhost:3000/jwks`
- Static dev console at `http://localhost:3000`
- OpenRTC runtime at `http://localhost:8080`
- Runtime WebSocket at `ws://localhost:8080/ws`
- Runtime Yjs WebSocket at `ws://localhost:8080/yjs/{room}`
- OpenRTC admin API at `http://localhost:8090`
- Same-origin admin proxy at `http://localhost:3000/admin`
- Same-origin runtime proxy at `http://localhost:3000/runtime`

## Quick flow

1. Click `Probe all` to issue local tokens and hit the main admin surfaces.
2. Open `Realtime`, click `Connect + join`, then send events or presence.
3. Open another browser tab with the same room to watch fan-out and presence.
4. Use `Rooms`, `Storage`, `Threads`, and `Notifications` to inspect durable
   Redis-backed state.
5. Use `Yjs` to connect to the binary endpoint, send an update frame, reconnect,
   and verify replay.
