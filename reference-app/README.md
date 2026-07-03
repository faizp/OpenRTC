# OpenRTC Reference App - Dev Console

The reference app is a local dev console for exercising OpenRTC end to end. It
starts a mock JWKS issuer, the OpenRTC runtime, the OpenRTC admin API, and a
browser UI for probing the implemented protocol surfaces.

## What it covers

- Client JWT and admin JWT issuance for local testing.
- JSON WebSocket room join, leave, events, and presence.
- Admin event publish and admin-side presence writes.
- Room CRUD, metadata, access grants, and active-user reads.
- Durable room storage, typed storage roots, JSON Patch, and realtime
  `STORAGE_GET` / `STORAGE_SET` / `STORAGE_PATCH` WebSocket probes.
- Durable threads, comments, inbox notifications, notification settings, and
  room subscription settings.
- Yjs binary endpoint connect, update write, replay, and client snapshot
  rejection probes.
- Health, readiness, metrics, and aggregate stats checks.

The in-browser Yjs panel is a transport probe for `/yjs/{room}`. CRDT merge
semantics and editor bindings live in the `@openrtc/yjs` and
`@openrtc/rich-text` packages.

## Running locally

Prerequisite: Go 1.18+.

```bash
# Start the integrated dev server from the repo root.
go run ./server/cmd/openrtc dev

# Optional: use external Redis instead of the embedded local store.
go run ./server/cmd/openrtc dev --storage redis --redis-url redis://localhost:6379/0
```

Then open:

```bash
open http://127.0.0.1:3000
```

The dev server starts:

- Local JWKS provider at `http://127.0.0.1:3000/jwks`
- Static dev console at `http://127.0.0.1:3000`
- Anonymous token helper at `http://127.0.0.1:3000/dev/token?pubkey=pk_localdev` with embedded local config and default room
- OpenRTC runtime at `http://127.0.0.1:8080`
- Runtime WebSocket at `ws://127.0.0.1:8080/ws`
- Runtime Yjs WebSocket at `ws://127.0.0.1:8080/yjs/{room}`
- OpenRTC admin API at `http://127.0.0.1:8090`
- Same-origin admin proxy at `http://127.0.0.1:3000/admin`
- Same-origin runtime proxy at `http://127.0.0.1:3000/runtime`
- Dev stack status and runtime/admin generation metadata at `http://127.0.0.1:3000/dev/status`
- Active-user reads at `http://127.0.0.1:3000/dev/connections?room=demo:room-1`
- Local runtime socket reads at `http://127.0.0.1:3000/dev/sockets`
- Seeded typed storage reads at `http://127.0.0.1:3000/dev/storage?room=demo:room-1`
- Yjs snapshot/update metadata reads at `http://127.0.0.1:3000/dev/yjs?room=demo:room-1`
- Bounded room event-log reads at `http://127.0.0.1:3000/dev/events?room=demo:room-1`
- Ops dev status, socket/event inspection, and reconnect drill with generation verification

The older reference server entrypoint still works from this directory:

```bash
go run ./cmd/server
```

## Quick flow

1. Click `Probe all` to issue local tokens and hit the main admin surfaces.
2. Open `Realtime`, click `Connect + join`, then send events or presence.
3. Open another browser tab with the same room to watch fan-out and presence.
4. Use `Rooms`, `Storage`, `Threads`, and `Notifications` to inspect local
   dev state. The `Storage` tab covers both admin REST storage and runtime
   WebSocket storage snapshots, acks, and updates.
5. Use `Yjs` to connect to the binary endpoint, send an update frame, reconnect,
   and verify replay.
