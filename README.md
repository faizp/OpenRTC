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
- `openrtc-runtime` owns WebSocket traffic, access-grant checks, limits, and delivery; room-centered session, presence, storage mutation, Yjs, and fan-out planning live in `server/internal/roomengine`.
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
origin proxies. It uses an embedded Redis-compatible store by default, so no
external service is required.

```bash
# Start the integrated dev server from the repo root.
go run ./server/cmd/openrtc dev

# Optional: use external Redis instead of the embedded local store.
go run ./server/cmd/openrtc dev --storage redis --redis-url redis://localhost:6379/0

# In another terminal, run a typed smoke probe against the live dev stack.
go run ./server/cmd/openrtc dev probe --reconnect --realtime --yjs-realtime
go run ./server/cmd/openrtc dev probe --json

# Fetch a local client/admin token for terminal scripts or CI.
go run ./server/cmd/openrtc dev token
go run ./server/cmd/openrtc dev token --kind admin --scope "rooms:*" --json
go run ./server/cmd/openrtc dev token --room demo:room-1 --env
```

Then open `http://127.0.0.1:3000`. The dev server exposes:

- `http://127.0.0.1:3000/jwks` for local token verification.
- `http://127.0.0.1:3000/dev/config` for local client/admin/runtime URLs and debug endpoint discovery without issuing a token.
- `http://127.0.0.1:3000/dev/token?pubkey=pk_localdev` for anonymous client JWTs plus the local config, debug endpoint URLs, and default room.
- `http://127.0.0.1:3000/dev/status` for storage backend, Redis protocol health, runtime/admin generation metadata, seeded-room, and endpoint readiness.
- `ws://127.0.0.1:8080/ws` and `ws://127.0.0.1:8080/yjs/{room}` for runtime traffic.
- `http://127.0.0.1:8090` for the admin API.
- `http://127.0.0.1:3000/dev/connections?room=demo:room-1` for active-user inspection.
- `http://127.0.0.1:3000/dev/sockets` for local runtime WebSocket/Yjs socket inspection.
- `http://127.0.0.1:3000/dev/storage?room=demo:room-1` for durable and runtime-observed room storage inspection, including storage sequence metadata when available.
- `http://127.0.0.1:3000/dev/yjs?room=demo:room-1` for durable and runtime-observed Yjs snapshot/update metadata.
- `POST http://127.0.0.1:3000/dev/crash/runtime` and `/dev/crash/admin` to restart local services and return the new service generation.
- The Ops tab includes dev status, socket/event inspection, and a runtime reconnect drill that restarts the local runtime, reconnects, and verifies the new socket/presence path.
- `openrtc dev probe` runs the same endpoint checks from a terminal or CI job, including optional runtime/admin restart drills, JSON output, `--reconnect` for a runtime crash/reconnect/rejoin drill, `--realtime` for a tokenized runtime WebSocket join plus sequenced storage patch and duplicate `op_id` retry, and `--yjs-realtime` for a live Yjs WebSocket update check.
- `openrtc dev token` fetches local client/admin JWTs from a terminal, defaulting to a token-only stdout value for command substitution, with `--json` for the full response and `--env` for shell-safe `OPENRTC_DEV_*` assignments.
- `createOpenRTCDevClient()` and `createOpenRTCDevAdminClient()` return typed `tools` helpers for fetching status, sockets, storage, Yjs metadata, event logs, restart/reconnect drills, and a reusable `tools.probe()` smoke check from the advertised dev URLs.

For a zero-config local client, let the SDK fetch the dev token response and
use its embedded config:

```ts
import { createOpenRTCDevAdminClient, createOpenRTCDevClient, fetchOpenRTCDevConfig } from "@openrtc/client";

const config = await fetchOpenRTCDevConfig();
console.log("OpenRTC runtime:", config.wsURL);
const { client, room, tools } = await createOpenRTCDevClient();
await client.connect();
client.enterRoom(room);
const probe = await tools.probe({ restart: "runtime", realtime: true });
if (!probe.ok) {
  console.table(probe.checks);
  throw new Error("OpenRTC dev probe failed");
}

const { admin } = await createOpenRTCDevAdminClient({ useProxy: true });
await admin.stats();
```

## Client presence integration

`@openrtc/client` exposes both low-level protocol methods and a Liveblocks-style
room handle for app integrations that need ephemeral presence, live cursors,
debuggable broadcast events, and realtime room storage.

```ts
import {
  OpenRTCClient,
  liveList,
  liveListAppend,
  liveMap,
  liveObject,
  liveStorageMutation,
} from "@openrtc/client";

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
  expectedSequence: room.getStorageSequence(),
});

const typedRoot = liveObject({
  title: "Typed Draft",
  items: liveList(["intro"]),
  props: liveMap({ visible: true }),
});
await room.setLiveStorage(typedRoot, { opId: "typed-init-1" });
await room.updateLiveStorage({ title: "Typed Review" }, { opId: "typed-title-1" });
await room.mutateLiveStorage((storage) => {
  storage.list<string>("items").append("next");
  storage.map("props").set({ visible: false }).delete("staleFlag");
  storage.object().delete("draftNote");
}, { opId: "typed-nested-1" });
const reusablePatch = liveStorageMutation((storage) => {
  storage.object().set({ title: "Ready" });
  return liveListAppend("final", { basePath: "/data/items/data" });
});
await room.patchStorage(reusablePatch, { opId: "typed-reusable-1" });

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
with the authoritative server ack or roll back on failure. When a server ack or
remote update arrives while later local mutations are still pending, the SDK
uses the server document as the new base and replays pending optimistic
mutations on top. `room.getStoragePendingMutations()` plus client
`storage-status` events and room `storage-status` subscription updates expose
pending mutation count, op IDs, and the latest authoritative storage sequence
when the runtime provides one. Sequenced storage ACK/UPDATE messages are exposed
as `event.sequence` and `room.getStorageSequence()`, and `getStorage()` applies
snapshot sequence metadata before resolving. Stale sequenced updates are ignored
so late fan-out cannot roll local storage backward. Pass
`expectedSequence` to `setStorage`, `patchStorage`, or typed storage helpers to
reject stale writes with `STORAGE_CONFLICT` instead of applying them on a newer
base document. Conflict errors include the authoritative storage document and
sequence when available, and the SDK applies that repair snapshot before
rejecting the stale mutation so later pending mutations can rebase cleanly.
Storage mutations send an `op_id` automatically when one is not provided, so
optimistic, ack, and rollback events can be correlated. The runtime treats a
retry with the same `op_id` and same normalized mutation as idempotent: it
replays the original ACK document/sequence without incrementing storage sequence
or fanning out a second update. Reusing an `op_id` for a different mutation is a
`STORAGE_CONFLICT`. Typed storage helpers build Liveblocks-style `LiveObject`,
`LiveList`, and `LiveMap` envelopes and
`updateLiveStorage` patches root `LiveObject.data` fields without hand-writing
reserved envelope JSON. `room.mutateLiveStorage` and `liveStorageMutation`
compose LiveObject/LiveMap/LiveList operations into one `STORAGE_PATCH`
mutation while retaining op IDs, optimistic updates, conflict repair, and
sequence handling. `liveObjectDelete`, `liveMapPatch`, `liveMapDelete`,
`liveListAppend`, `liveListInsert`, `liveListReplace`, `liveListRemove`, and
`liveListMove` remain available when callers want raw nested typed-node JSON
Patch operations for `room.patchStorage`. Collaborative text
remains owned by the Yjs provider.
Room handles also expose `room.history` for Liveblocks-style local undo/redo of
acknowledged storage writes. `room.history.undo()` and `redo()` send focused
JSON Patch mutations built from the accepted before/after documents, and
`pause()` / `resume()` merge multiple accepted writes into one undo item. Use
`room.history.disable(() => write())` or pass `{ addToHistory: false }` for
background reconciliation writes that should not appear on the local undo stack.
For product-surface state, `@openrtc/client` exports
`applyCommentEventToThreads` and `applyNotificationDeltaToInbox` reducers so
apps can seed from REST thread/inbox lists and apply realtime deltas
immutably. `@openrtc/react` exposes `useRoomThreads`, `useRoomThread`,
`useInboxNotifications`, and `useUnreadInboxCount` for the same materialized
comment and notification state in React. Wrap an `OpenRTCAdminClient` with
`OpenRTCAdminProvider` or pass `admin` to `useRoomThreadsState` /
`useInboxNotificationsState` when the hook should fetch the durable REST list
before realtime deltas take over.
The provider requests state-vector diffs after opening, relays transient diff
responses through the runtime without persisting them, and exposes
`getSyncState()` plus `sync-status` events with state-vector and snapshot hashes
for reconnect diagnostics. It automatically reconnects by default using bounded
backoff; local root and subdoc updates made while disconnected are marked as
pending and flushed to the replacement socket before the provider requests a
fresh state-vector diff. Set `autoReconnect: false` or pass `reconnect` options
to tune that behavior. For browser offline starts, pass
`offlineStore: createIndexedDBYjsStore({ room: "tenant-a:canvas-1" })`; cached
Yjs updates are replayed before the websocket opens, and local/remote updates
are appended to the cache without changing server durability semantics.

For rich-text editors, `@openrtc/rich-text` exports Tiptap, Lexical, and
BlockNote integration helpers that wire `OpenRTCClient`, `OpenRTCYjsProvider`,
Yjs document bindings, editor selection presence, remote-selection filtering,
and cleanup without adding editor dependencies to the OpenRTC package itself.
React apps can import `@openrtc/rich-text/react` for
`useRemoteTextSelections()` and `useSelectionPresenceController()` so Tiptap,
Lexical, and BlockNote canvases can render remote selections and flush local
selection presence without custom room subscription glue.

The React package exposes the same lifecycle through `useEnterRoom`,
`usePresence`, `useOthers`, `useOthersMapped`, `useOthersConnectionIds`,
`useCursors`, `useOtherCursors`, `useCursorsMapped`, `useOther`, `useSelf`,
`useSelfCursor`, `useCursor`, `useMyPresence`, `useMyPresenceSelector`,
`useSetCursor`, `useBroadcastEvent`, `useBroadcastEventWithAck`, `useStatus`,
`useRoomStatus`, `useRoomEvents`, `useCommentListener`, `useRoomCommentEvents`,
`useNotificationListener`, `useNotificationEvents`, `useDiagnostics`, `useErrorListener`,
`useLostConnectionListener`, `useRoomReconnect`, `useStorage`,
`useStorageSelector`, `useStorageStatus`, `useStorageSequence`, `useSetStorage`, `usePatchStorage`,
`useStoragePendingMutations`, `useSetLiveStorage`, `useUpdateLiveStorage`,
`useMutateLiveStorage`, `useHistory`, `useUndo`, `useRedo`, `useCanUndo`,
`useCanRedo`, `useStorageMutation`, `useMutation`, and
`useStorageListener`. It also exports `RoomProvider`, `useCurrentRoom`, and
`createRoomContext()` for Liveblocks-style room-bound hooks where components call
`useOthers()`, `useStorage()`, and `useMutation()` without passing a room ID
through every hook. It also exports Liveblocks-style `Cursors`, `Cursor`, and
`AvatarStack` components for apps that
want cursor tracking/rendering and collaborator stacks without building the UI
from scratch. Cursor hooks and components return typed cursor peers with
resolved `user`, `color`, and `mode` fields, and accept a `presenceKey` for apps
with multiple cursor layers in one room. Broadcast hooks accept the same string
or object-shaped events as room handles. Storage hooks retain the room, request
the latest storage snapshot, subscribe to realtime updates, expose storage
status, support selector equality for avoiding unrelated storage updates, and
provide stable set/patch mutation callbacks. Room hooks use shared
entry tracking, so multiple components can subscribe to the same room without
one cleanup leaving the room for the others. `initialPresence` is captured once
per room entry, so inline initial presence objects do not cause accidental
leave/rejoin churn on rerender.

```tsx
import {
  AvatarStack,
  Cursors,
  useBroadcastEventWithAck,
  useCanUndo,
  useEnterRoom,
  useLostConnectionListener,
  usePatchStorage,
  useStorage,
  useUndo,
} from "@openrtc/react";

export function CanvasPresence() {
  const room = useEnterRoom("tenant-a:canvas-1", {
    initialPresence: { cursor: null, user: { id: "user-1", name: "Ada" } },
  });
  const broadcastWithAck = useBroadcastEventWithAck(room.id);
  const storage = useStorage<{ title?: string }>(room.id);
  const patchStorage = usePatchStorage(room.id);
  const canUndo = useCanUndo(room.id);
  const undo = useUndo(room.id);

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
      <button type="button" disabled={!canUndo} onClick={() => undo()}>
        Undo
      </button>
      <Canvas onPing={() => broadcastWithAck({ type: "canvas.ping", at: Date.now() })} />
    </Cursors>
  );
}
```

For a room-context integration style, create bound hooks once and use them below
the provider:

```tsx
import { createRoomContext } from "@openrtc/react";

const {
  RoomProvider,
  useMutation,
  useOthers,
  useStorage,
} = createRoomContext();

export function CanvasRoom() {
  return (
    <RoomProvider
      id="tenant-a:canvas-1"
      initialPresence={{ cursor: null, user: { id: "user-1", name: "Ada" } }}
    >
      <CanvasToolbar />
    </RoomProvider>
  );
}

function CanvasToolbar() {
  const others = useOthers();
  const storage = useStorage<{ title?: string }>();
  const rename = useMutation<{ title?: string }, [title: string], Promise<void>>(
    async ({ updateMyPresence, setStorage, broadcastEvent }, title) => {
      updateMyPresence({ editingTitle: title });
      await setStorage({ title }, { opId: `rename:${title}` });
      broadcastEvent({ type: "canvas.renamed", title });
    },
    [],
  );

  return (
    <button type="button" onClick={() => rename("Review")}>
      {storage?.title ?? "Untitled"} ({others.length})
    </button>
  );
}
```

For server-side product surfaces, `OpenRTCAdminClient` wraps the admin REST APIs
used for rooms, active users, comments, comment metadata/reaction/mention
updates, notifications, subscription settings, ephemeral presence, and
broadcast.

Admin storage PUT and JSON Patch mutations also emit realtime `storage` client
updates for connected room subscribers.

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
import {
  OpenRTCAdminClient,
  accessMatrixPolicy,
  addCommentMention,
  addCommentReaction,
  roomQuery,
} from "@openrtc/client";

const admin = new OpenRTCAdminClient({
  url: "https://openrtc.example.com",
  token: process.env.OPENRTC_ADMIN_TOKEN!,
});

const canvasPolicy = accessMatrixPolicy({
  subject: {
    room: "write",
    storage: "write",
    comments: "write",
  },
  roomPattern: "tenant-a:*",
  roomAccesses: {
    default: {
      room: "read",
      storage: "read",
      comments: "read",
    },
    users: {
      "blocked-user": { room: "none", storage: "none", comments: "none" },
    },
    groups: {
      editors: {
        room: "write",
        storage: "write",
        comments: "write",
      },
    },
  },
});

await admin.createRoom(canvasPolicy.roomInput({
  id: "tenant-a:canvas-1",
  metadata: { type: "whiteboard", archived: false },
}));

const editorTokenClaims = {
  sub: "user-1",
  groupIds: ["editors"],
  ...canvasPolicy.tokenClaims,
};

const editableWhiteboards = await admin.listRooms({
  prefix: "tenant-a:",
  query: roomQuery({ "metadata.type": "whiteboard", "metadata.archived": false }),
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
  mentions: addCommentMention(["user-1"], "user-2"),
  reactions: addCommentReaction([], { emoji: "+1", userId: "user-2" }),
});
await admin.triggerInboxNotification({
  userId: "user-1",
  kind: "$custom",
  roomId: "tenant-a:canvas-1",
  activityData: { activeUsers: active.data.length },
});
```
