# OpenRTC

OpenRTC is a self-hosted OSS realtime layer for SaaS teams.

## Monorepo layout

- `server/`: Go core backend module. It builds `openrtc`, `openrtc-runtime`, and `openrtc-admin`.
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

## Local dev server

`openrtc dev` starts a full local stack for integration work: a local JWKS
issuer, the runtime, the admin API, the reference UI, seeded rooms, and same
origin proxies.

```bash
# Start Redis if one is not already running.
docker run -d --name openrtc-redis -p 6379:6379 redis:7-alpine

# Start the integrated dev server from the repo root.
go run ./server/cmd/openrtc dev
```

Then open `http://127.0.0.1:3000`. The dev server exposes:

- `http://127.0.0.1:3000/jwks` for local token verification.
- `http://127.0.0.1:3000/dev/token?pubkey=pk_localdev` for anonymous client JWTs.
- `ws://127.0.0.1:8080/ws` and `ws://127.0.0.1:8080/yjs/{room}` for runtime traffic.
- `http://127.0.0.1:8090` for the admin API.
- `http://127.0.0.1:3000/dev/connections?room=demo:room-1` for active-user inspection.
- `POST http://127.0.0.1:3000/dev/crash/runtime` and `/dev/crash/admin` to restart local services.

## Client presence integration

`@openrtc/client` exposes both low-level protocol methods and a Liveblocks-style
room handle for app integrations that need ephemeral presence, live cursors,
debuggable broadcast events, and realtime room storage.

```ts
import { OpenRTCClient } from "@openrtc/client";

const client = new OpenRTCClient({
  url: "https://openrtc.example.com/ws",
  token: async () => fetch("/api/openrtc-token").then((res) => res.text()),
  lostConnectionTimeout: 5000,
  backgroundKeepAliveTimeout: 15 * 60 * 1000,
  reconnect: { initialDelayMs: 250, maxDelayMs: 5000 },
});

await client.connect();

const { room, leave } = client.enterRoom("tenant-a:canvas-1", {
  initialPresence: {
    cursor: null,
    user: { id: "user-1", name: "Ada", color: "#4fd1b6" },
  },
});

const unsubscribe = room.subscribe("others", (others, event) => {
  console.log(event.type, others);
});
const unsubscribeLostConnection = room.subscribe("lost-connection", (event) => {
  console.log(event);
});

room.setCursor({ x: 120, y: 240, mode: "comment" });
room.broadcastEvent({ type: "CANVAS_PING", at: Date.now() });

const storage = await room.getStorage<{ title: string; version: number }>();
console.log(storage.title);
room.subscribe("storage", (event) => {
  console.log(event.source, event.document);
});
await room.patchStorage([{ op: "replace", path: "/title", value: "Review" }], {
  opId: "title-edit-1",
});

unsubscribe();
unsubscribeLostConnection();
leave();
```

Presence is ephemeral. The client automatically reconnects by default, keeps the
latest local presence in memory, replays active rooms after the next `HELLO`, and
only clears stale remote collaborators after `lostConnectionTimeout`.
`lostConnectionTimeout` is clamped to the Liveblocks-compatible 1000-30000 ms
range. In browser environments, `backgroundKeepAliveTimeout` can close hidden
tabs after an inactivity window and reconnect/replay rooms when the tab is
focused again. Room handles emit `lost`, `restored`, and `failed` through the
`lost-connection` subscription; call `room.reconnect()` for an explicit retry
after a hard failure. Room storage uses the runtime `STORAGE_GET`,
`STORAGE_SET`, and `STORAGE_PATCH` protocol, keeps the latest authoritative
snapshot in memory, emits `storage` / `storage-status` updates, and requests a
fresh snapshot when an active room reconnects. Collaborative text remains owned
by the Yjs provider.

The React package exposes the same lifecycle through `useEnterRoom`,
`usePresence`, `useOthers`, `useOthersMapped`, `useOthersConnectionIds`,
`useCursors`, `useOtherCursors`, `useCursorsMapped`, `useOther`, `useSelf`,
`useSelfCursor`, `useCursor`, `useMyPresence`, `useMyPresenceSelector`,
`useSetCursor`, `useBroadcastEvent`, `useBroadcastEventWithAck`, `useStatus`,
`useRoomStatus`, `useRoomEvents`, `useDiagnostics`, `useErrorListener`,
`useLostConnectionListener`, and `useRoomReconnect`. It also exports
Liveblocks-style `Cursors`, `Cursor`, and `AvatarStack` components for apps that
want cursor tracking/rendering and collaborator stacks without building the UI
from scratch. Cursor hooks and components return typed cursor peers with
resolved `user`, `color`, and `mode` fields, and accept a `presenceKey` for apps
with multiple cursor layers in one room. Broadcast hooks accept the same string
or object-shaped events as room handles. Room hooks use shared entry tracking,
so multiple components can subscribe to the same room without one cleanup
leaving the room for the others. `initialPresence` is captured once per room
entry, so inline initial presence objects do not cause accidental leave/rejoin
churn on rerender.

```tsx
import {
  AvatarStack,
  Cursors,
  useBroadcastEventWithAck,
  useEnterRoom,
  useLostConnectionListener,
} from "@openrtc/react";

export function CanvasPresence() {
  const room = useEnterRoom("tenant-a:canvas-1", {
    initialPresence: { cursor: null, user: { id: "user-1", name: "Ada" } },
  });
  const broadcastWithAck = useBroadcastEventWithAck(room.id);

  useLostConnectionListener(room.id, (event) => {
    console.info("room connection", event);
  });

  return (
    <Cursors
      room={room.id}
      cursorOptions={{ user: { id: "user-1", name: "Ada" }, color: "#4fd1b6" }}
      mode="pointer"
    >
      <AvatarStack room={room.id} max={5} />
      <Canvas onPing={() => broadcastWithAck({ type: "canvas.ping", at: Date.now() })} />
    </Cursors>
  );
}
```

For server-side product surfaces, `OpenRTCAdminClient` wraps the admin REST APIs
used for rooms, active users, comments, notifications, subscription settings,
ephemeral presence, and broadcast.

The reference app Presence Lab includes a fan-out benchmark for production
debugging. Spawn lab clients, run the benchmark, and it stamps every synthetic
presence update with a run ID, round, sender, and sent timestamp. The UI reports
expected versus observed delivery, loss percentage, p99 latency, and duration so
integrators can verify multi-client realtime behavior before embedding OpenRTC.

```ts
import { OpenRTCAdminClient } from "@openrtc/client";

const admin = new OpenRTCAdminClient({
  url: "https://openrtc.example.com",
  token: process.env.OPENRTC_ADMIN_TOKEN!,
});

await admin.createRoom({
  id: "tenant-a:canvas-1",
  defaultAccesses: ["room:read", "room:presence:write"],
});

await admin.setPresence(
  "tenant-a:canvas-1",
  "agent-1",
  { status: "active", cursor: { x: 120, y: 240 } },
  { ttlSeconds: 60 },
);

const active = await admin.activeUsers("tenant-a:canvas-1");
await admin.createThread("tenant-a:canvas-1", {
  comment: {
    userId: "user-1",
    body: { type: "text", text: "Ready for review" },
  },
});
await admin.triggerInboxNotification({
  userId: "user-1",
  kind: "$custom",
  roomId: "tenant-a:canvas-1",
  activityData: { activeUsers: active.data.length },
});
```
