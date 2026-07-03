# OpenRTC

OpenRTC is a self-hosted OSS realtime layer for SaaS teams.

## Monorepo layout

- `server/`: Go core backend module. It builds `openrtc`, `openrtc-runtime`, and `openrtc-admin`.
- `packages/client/`: TypeScript WebSocket client for rooms, events, and presence.
- `packages/react/`: React hooks for room state, presence, and broadcast events.
- `packages/rich-text/`: Yjs binding helpers plus presence adapters for Tiptap, Lexical, and BlockNote selection/cursor state.
- `packages/yjs/`: Yjs provider for binary update/snapshot sync, state-vector diff sync, optional IndexedDB offline caching, sync diagnostics, and an awareness bridge over OpenRTC presence.
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
- `http://127.0.0.1:3000/dev/sockets` for local runtime WebSocket/Yjs socket inspection.
- `POST http://127.0.0.1:3000/dev/crash/runtime` and `/dev/crash/admin` to restart local services.
- The Ops tab includes a runtime reconnect drill that restarts the local runtime, reconnects, and verifies the new socket/presence path.

## Client presence integration

`@openrtc/client` exposes both low-level protocol methods and a Liveblocks-style
room handle for app integrations that need ephemeral presence, live cursors,
debuggable broadcast events, and realtime room storage.

```ts
import { OpenRTCClient, liveList, liveMap, liveObject } from "@openrtc/client";

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
room.subscribe("comments", (event) => {
  console.log(event.type, event.threadId, event.commentId);
});
const unsubscribeNotifications = client.on("notification", (event) => {
  console.log(event.type, event.notificationId, event.notification?.roomId);
});
await room.patchStorage([{ op: "replace", path: "/title", value: "Review" }], {
  opId: "title-edit-1",
});

const typedRoot = liveObject({
  title: "Typed Draft",
  items: liveList(["intro"]),
  props: liveMap({ visible: true }),
});
await room.setLiveStorage(typedRoot, { opId: "typed-init-1" });
await room.updateLiveStorage({ title: "Typed Review" }, { opId: "typed-title-1" });

unsubscribe();
unsubscribeLostConnection();
unsubscribeNotifications();
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
fresh snapshot when an active room reconnects. `setStorage` and loaded
`patchStorage` calls apply optimistic local updates, then replace local state
with the authoritative server ack or roll back on failure. Typed storage helpers
build Liveblocks-style `LiveObject`, `LiveList`, and `LiveMap` envelopes and
`updateLiveStorage` patches root `LiveObject.data` fields without hand-writing
reserved envelope JSON. Collaborative text remains owned by the Yjs provider.
The provider requests state-vector diffs after opening, relays transient diff
responses through the runtime without persisting them, and exposes
`getSyncState()` plus `sync-status` events with state-vector and snapshot hashes
for reconnect diagnostics. For browser offline starts, pass
`offlineStore: createIndexedDBYjsStore({ room: "tenant-a:canvas-1" })`; cached
Yjs updates are replayed before the websocket opens, and local/remote updates
are appended to the cache without changing server durability semantics.

For rich-text editors, `packages/rich-text/README.md` has Tiptap, Lexical, and
BlockNote recipes that wire `OpenRTCClient`, `OpenRTCYjsProvider`, Yjs document
bindings, editor selection presence, and cleanup without adding editor
dependencies to the OpenRTC package itself.

The React package exposes the same lifecycle through `useEnterRoom`,
`usePresence`, `useOthers`, `useOthersMapped`, `useOthersConnectionIds`,
`useCursors`, `useOtherCursors`, `useCursorsMapped`, `useOther`, `useSelf`,
`useSelfCursor`, `useCursor`, `useMyPresence`, `useMyPresenceSelector`,
`useSetCursor`, `useBroadcastEvent`, `useBroadcastEventWithAck`, `useStatus`,
`useRoomStatus`, `useRoomEvents`, `useCommentListener`, `useRoomCommentEvents`,
`useNotificationListener`, `useNotificationEvents`, `useDiagnostics`, `useErrorListener`,
`useLostConnectionListener`, `useRoomReconnect`, `useStorage`,
`useStorageSelector`, `useStorageStatus`, `useSetStorage`, `usePatchStorage`,
`useStorageMutation`, and `useStorageListener`. It also exports
Liveblocks-style `Cursors`, `Cursor`, and `AvatarStack` components for apps that
want cursor tracking/rendering and collaborator stacks without building the UI
from scratch. Cursor hooks and components return typed cursor peers with
resolved `user`, `color`, and `mode` fields, and accept a `presenceKey` for apps
with multiple cursor layers in one room. Broadcast hooks accept the same string
or object-shaped events as room handles. Storage hooks retain the room, request
the latest storage snapshot, subscribe to realtime updates, expose storage
status, and provide stable set/patch mutation callbacks. Room hooks use shared
entry tracking, so multiple components can subscribe to the same room without
one cleanup leaving the room for the others. `initialPresence` is captured once
per room entry, so inline initial presence objects do not cause accidental
leave/rejoin churn on rerender.

```tsx
import {
  AvatarStack,
  Cursors,
  useBroadcastEventWithAck,
  useEnterRoom,
  useLostConnectionListener,
  usePatchStorage,
  useStorage,
} from "@openrtc/react";

export function CanvasPresence() {
  const room = useEnterRoom("tenant-a:canvas-1", {
    initialPresence: { cursor: null, user: { id: "user-1", name: "Ada" } },
  });
  const broadcastWithAck = useBroadcastEventWithAck(room.id);
  const storage = useStorage<{ title?: string }>(room.id);
  const patchStorage = usePatchStorage(room.id);

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
      <button
        type="button"
        onClick={() => patchStorage([{ op: "replace", path: "/title", value: "Review" }])}
      >
        {storage?.title ?? "Untitled"}
      </button>
      <Canvas onPing={() => broadcastWithAck({ type: "canvas.ping", at: Date.now() })} />
    </Cursors>
  );
}
```

For server-side product surfaces, `OpenRTCAdminClient` wraps the admin REST APIs
used for rooms, active users, comments, comment metadata/reaction/mention
updates, notifications, subscription settings, ephemeral presence, and
broadcast.

Inbox notification create/read/delete/delete-all mutations also emit
user-targeted realtime `notification` client events and React notification
hooks for connected users. These deltas complement the durable inbox REST APIs;
refresh the list after reconnects.

Admin room, comment, and notification mutations can also fan out signed
best-effort webhooks when `OPENRTC_WEBHOOK_URL` or `OPENRTC_WEBHOOK_URLS` and
`OPENRTC_WEBHOOK_SECRET` are configured. Webhook failures are logged and do not
roll back the admin mutation; see `docs/protocol/v1.md` for event names,
headers, and payload envelopes.

The reference app Presence Lab includes a fan-out benchmark for production
debugging. Spawn lab clients, run the benchmark, and it stamps every synthetic
presence update with a run ID, round, sender, and sent timestamp. The UI reports
expected versus observed delivery, loss percentage, p99 latency, and duration so
integrators can verify multi-client realtime behavior before embedding OpenRTC.

```ts
import { OpenRTCAdminClient, accessMatrixPermissions } from "@openrtc/client";

const admin = new OpenRTCAdminClient({
  url: "https://openrtc.example.com",
  token: process.env.OPENRTC_ADMIN_TOKEN!,
});

await admin.createRoom({
  id: "tenant-a:canvas-1",
  metadata: { type: "whiteboard", archived: false },
  defaultAccesses: accessMatrixPermissions({
    room: "read",
    storage: "read",
    comments: "read",
  }),
  groupsAccesses: {
    editors: accessMatrixPermissions({
      room: "write",
      storage: "write",
      comments: "write",
    }),
  },
});

const editableWhiteboards = await admin.listRooms({
  prefix: "tenant-a:",
  query: `metadata.type:whiteboard metadata.archived:false`,
  limit: 50,
});

await admin.setPresence(
  "tenant-a:canvas-1",
  "agent-1",
  { status: "active", cursor: { x: 120, y: 240 } },
  { ttlSeconds: 60 },
);

const active = await admin.activeUsers("tenant-a:canvas-1");
await admin.createThread("tenant-a:canvas-1", {
  id: "thread-1",
  comment: {
    id: "comment-1",
    userId: "user-1",
    body: { type: "text", text: "Ready for review" },
    mentions: ["user-2"],
  },
});
await admin.updateComment("tenant-a:canvas-1", "thread-1", "comment-1", {
  metadata: { status: "resolved" },
  reactions: [{ emoji: "+1", userId: "user-2" }],
});
await admin.triggerInboxNotification({
  userId: "user-1",
  kind: "$custom",
  roomId: "tenant-a:canvas-1",
  activityData: { activeUsers: active.data.length },
});
```
