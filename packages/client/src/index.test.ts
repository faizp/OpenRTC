import assert from "node:assert/strict";
import {
  OpenRTCAdminClient,
  OpenRTCAdminError,
  OpenRTCClient,
  OPENRTC_COMMENT_EVENTS,
  OPENRTC_NOTIFICATION_EVENTS,
  OPENRTC_ROOM_PERMISSIONS,
  accessMatrixPermissions,
  getCursorPeers,
  getPresenceColor,
  getPresenceCursor,
  getPresenceUser,
  isOpenRTCCursor,
  isLiveList,
  isLiveMap,
  isLiveObject,
  isLiveStorageNode,
  liveList,
  liveMap,
  liveObject,
  liveObjectPatch,
  type OpenRTCCommentEvent,
  type OpenRTCDiagnosticEvent,
  type OpenRTCEvent,
  type OpenRTCNotificationDelta,
  type OpenRTCStorageEvent,
  type OpenRTCWebSocket,
  type PresenceState,
} from "./index.ts";

assert.deepEqual(accessMatrixPermissions({
  room: "read",
  storage: "write",
  comments: "none",
  feeds: "read",
}), [
  OPENRTC_ROOM_PERMISSIONS.roomRead,
  OPENRTC_ROOM_PERMISSIONS.storageWrite,
  OPENRTC_ROOM_PERMISSIONS.feedsRead,
]);
assert.deepEqual(accessMatrixPermissions({ comments: "write" }), [OPENRTC_ROOM_PERMISSIONS.commentsWrite]);

class FakeWebSocket implements OpenRTCWebSocket {
  static instances: FakeWebSocket[] = [];

  readonly sent: string[] = [];
  readyState = 0;
  private listeners = new Map<string, Set<(event: unknown) => void>>();

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(data: string | ArrayBufferLike | ArrayBufferView): void {
    if (typeof data !== "string") {
      throw new Error("expected JSON string payload");
    }
    this.sent.push(data);
  }

  close(): void {
    this.readyState = 3;
    this.emit("close", {});
  }

  addEventListener(type: string, listener: (event: unknown) => void): void {
    let listeners = this.listeners.get(type);
    if (!listeners) {
      listeners = new Set();
      this.listeners.set(type, listeners);
    }
    listeners.add(listener);
  }

  removeEventListener(type: string, listener: (event: unknown) => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  open(): void {
    this.readyState = 1;
    this.emit("open", {});
  }

  receive(payload: unknown): void {
    this.emit("message", { data: JSON.stringify(payload) });
  }

  private emit(type: string, event: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

class FakeDocument {
  hidden = false;
  visibilityState = "visible";
  private listeners = new Map<string, Set<(event: unknown) => void>>();

  addEventListener(type: string, listener: (event: unknown) => void): void {
    let listeners = this.listeners.get(type);
    if (!listeners) {
      listeners = new Set();
      this.listeners.set(type, listeners);
    }
    listeners.add(listener);
  }

  removeEventListener(type: string, listener: (event: unknown) => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  setHidden(hidden: boolean): void {
    this.hidden = hidden;
    this.visibilityState = hidden ? "hidden" : "visible";
    for (const listener of this.listeners.get("visibilitychange") ?? []) {
      listener({});
    }
  }

  listenerCount(type: string): number {
    return this.listeners.get(type)?.size ?? 0;
  }
}

const client = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-1",
  WebSocket: FakeWebSocket,
});
const diagnostics: OpenRTCDiagnosticEvent[] = [];
client.on("diagnostic", (event) => {
  diagnostics.push(event);
});
const connected = client.connect();
await Promise.resolve();
const socket = FakeWebSocket.instances[0];
assert.ok(socket);
assert.equal(socket.url, "ws://localhost:8080/ws?token=token-1");
socket.open();
await connected;

let latestOthers = client.getOthers("tenant-a:doc");
client.on("presence", (event) => {
  latestOthers = event.others;
});

socket.receive({
  t: "HELLO",
  payload: { conn_id: "self", server: { node_id: "node-a" } },
});
assert.equal(client.connId, "self");

socket.receive({
  t: "JOINED",
  room: "tenant-a:doc",
  payload: {
    members: ["self", "peer-1"],
    presence: {
      self: { name: "A" },
      "peer-1": { name: "B" },
    },
    next_cursor: "cursor-2",
  },
});
assert.deepEqual(client.getOthers("tenant-a:doc"), [{ connId: "peer-1", state: { name: "B" } }]);
assert.deepEqual(client.getSelf("tenant-a:doc"), { connId: "self", state: { name: "A" } });
assert.deepEqual(client.getMyPresence("tenant-a:doc"), { name: "A" });
assert.deepEqual(client.getPresence("tenant-a:doc"), {
  room: "tenant-a:doc",
  members: ["self", "peer-1"],
  others: [{ connId: "peer-1", state: { name: "B" } }],
  self: { connId: "self", state: { name: "A" } },
  nextCursor: "cursor-2",
});

socket.receive({
  t: "PRESENCE",
  room: "tenant-a:doc",
  payload: { conn_id: "peer-2", state: { cursor: { x: 1, y: 2 } } },
});
assert.deepEqual(latestOthers, [
  { connId: "peer-1", state: { name: "B" } },
  { connId: "peer-2", state: { cursor: { x: 1, y: 2 } } },
]);

socket.receive({
  t: "PRESENCE",
  room: "tenant-a:doc",
  payload: { conn_id: "peer-1", offline: true },
});
assert.deepEqual(client.getOthers("tenant-a:doc"), [
  { connId: "peer-2", state: { cursor: { x: 1, y: 2 } } },
]);

client.broadcast("tenant-a:doc", "chat.message", { text: "hello" });
assert.match(socket.sent.at(-1) ?? "", /"t":"EMIT"/);

client.updatePresence("tenant-a:doc", { user: { name: "A" } });
const userPresenceSend = JSON.parse(socket.sent.at(-1) ?? "{}") as {
  payload: Record<string, unknown>;
};
socket.receive({
  t: "PRESENCE",
  room: "tenant-a:doc",
  payload: { conn_id: "self", state: userPresenceSend.payload },
});
client.patchPresence("tenant-a:doc", { cursor: { x: 1 } });
assert.equal(
  socket.sent.at(-1),
  JSON.stringify({
    t: "PRESENCE_SET",
    id: "presence-3",
    room: "tenant-a:doc",
    payload: { user: { name: "A" }, cursor: { x: 1 } },
  }),
);
socket.receive({
  t: "PRESENCE",
  room: "tenant-a:doc",
  payload: { conn_id: "self", state: { user: { name: "A" }, cursor: { x: 1 } } },
});

client.setCursor(
  "tenant-a:doc",
  { x: 25, y: 50, mode: "comment", label: "Review point" },
  {
    user: { id: "user-a", name: "A", color: "#0f766e" },
    color: "#0f766e",
    metadata: { status: "reviewing" },
  },
);
const cursorSend = JSON.parse(socket.sent.at(-1) ?? "{}") as {
  id: string;
  payload: Record<string, unknown>;
};
assert.deepEqual(cursorSend.payload, {
  user: { id: "user-a", name: "A", color: "#0f766e" },
  cursor: { x: 25, y: 50, mode: "comment", label: "Review point" },
  status: "reviewing",
  color: "#0f766e",
});
assert.equal(isOpenRTCCursor(cursorSend.payload["cursor"]), true);
assert.equal(isOpenRTCCursor({ x: 1 }), false);
assert.deepEqual(getPresenceCursor(cursorSend.payload), { x: 25, y: 50, mode: "comment", label: "Review point" });
assert.equal(getPresenceCursor({ cursor: { x: 25 } }), null);
assert.deepEqual(getPresenceUser(cursorSend.payload), { id: "user-a", name: "A", color: "#0f766e" });
assert.equal(getPresenceUser({ user: "Ada" }), undefined);
assert.equal(getPresenceColor(cursorSend.payload), "#0f766e");

socket.receive({
  t: "PRESENCE",
  room: "tenant-a:doc",
  payload: { conn_id: "self", state: cursorSend.payload },
});
assert.deepEqual(client.getMyPresence("tenant-a:doc"), cursorSend.payload);
assert.deepEqual(getCursorPeers(client.getOthers("tenant-a:doc")), [
  { connId: "peer-2", state: { cursor: { x: 1, y: 2 } }, cursor: { x: 1, y: 2 } },
]);
assert.ok(
  diagnostics.some(
    (event) =>
      event.direction === "in" &&
      event.t === "PRESENCE" &&
      event.id === cursorSend.id &&
      event.room === "tenant-a:doc" &&
      typeof event.latencyMs === "number",
  ),
);

FakeWebSocket.instances = [];
const roomClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-2",
  WebSocket: FakeWebSocket,
  autoReconnect: false,
});
const roomDiagnostics: OpenRTCDiagnosticEvent[] = [];
roomClient.on("diagnostic", (event) => {
  roomDiagnostics.push(event);
});
const roomConnected = roomClient.connect();
await Promise.resolve();
const roomSocket = FakeWebSocket.instances[0];
assert.ok(roomSocket);
roomSocket.open();
await roomConnected;
roomSocket.receive({
  t: "HELLO",
  payload: { conn_id: "room-self", server: { node_id: "node-b" } },
});

const { room, leave } = roomClient.enterRoom("tenant-a:room-api", {
  initialPresence: { cursor: null, user: { name: "Room user" } },
});
assert.deepEqual(
  roomSocket.sent.slice(-2).map((item) => JSON.parse(item) as Record<string, unknown>),
  [
    { t: "JOIN", id: "join-1", room: "tenant-a:room-api" },
    {
      t: "PRESENCE_SET",
      id: "presence-2",
      room: "tenant-a:room-api",
      payload: { cursor: null, user: { name: "Room user" } },
    },
  ],
);

const othersEvents: string[] = [];
const myPresenceValues: PresenceState[] = [];
const roomEvents: string[] = [];
const receivedRoomEvents: OpenRTCEvent[] = [];
const commentEvents: OpenRTCCommentEvent[] = [];
const notificationEvents: OpenRTCNotificationDelta[] = [];
const offOthers = room.subscribe("others", (_others, event) => {
  othersEvents.push(event.type);
});
const offMyPresence = room.subscribe("my-presence", (presence) => {
  myPresenceValues.push(presence);
});
const offEvents = room.subscribe("event", (event) => {
  roomEvents.push(event.event);
  receivedRoomEvents.push(event);
});
const offComments = room.subscribe("comments", (event) => {
  commentEvents.push(event);
});
const offNotifications = roomClient.on("notification", (event) => {
  notificationEvents.push(event);
});
const storageEvents: OpenRTCStorageEvent[] = [];
const storageStatuses: string[] = [];
const offStorage = room.subscribe("storage", (event) => {
  storageEvents.push(event);
});
const offStorageStatus = room.subscribe("storage-status", (status) => {
  storageStatuses.push(status);
});

roomSocket.receive({
  t: "JOINED",
  room: "tenant-a:room-api",
  payload: {
    members: ["room-self", "room-peer"],
    presence: {
      "room-self": { cursor: null, user: { name: "Room user" } },
      "room-peer": { cursor: { x: 2, y: 3 } },
    },
  },
});
assert.equal(room.getStatus(), "open");
assert.deepEqual(room.getSelf(), { connId: "room-self", state: { cursor: null, user: { name: "Room user" } } });
assert.deepEqual(room.getOthers(), [{ connId: "room-peer", state: { cursor: { x: 2, y: 3 } } }]);
assert.deepEqual(myPresenceValues.at(-1), { cursor: null, user: { name: "Room user" } });
assert.deepEqual(othersEvents, ["reset"]);

room.updatePresence({ cursor: { x: 8, y: 9 } });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "PRESENCE_SET",
    id: "presence-3",
    room: "tenant-a:room-api",
    payload: { cursor: { x: 8, y: 9 }, user: { name: "Room user" } },
  }),
);
roomSocket.receive({
  t: "PRESENCE",
  room: "tenant-a:room-api",
  payload: { conn_id: "room-self", state: { cursor: { x: 8, y: 9 }, user: { name: "Room user" } } },
});

roomSocket.receive({
  t: "PRESENCE",
  room: "tenant-a:room-api",
  payload: { conn_id: "room-peer-2", state: { cursor: { x: 13, y: 21 } } },
});
roomSocket.receive({
  t: "PRESENCE",
  room: "tenant-a:room-api",
  payload: { conn_id: "room-peer-2", state: { cursor: { x: 34, y: 55 } } },
});
roomSocket.receive({
  t: "PRESENCE",
  room: "tenant-a:room-api",
  payload: { conn_id: "room-peer-2", offline: true },
});
assert.deepEqual(othersEvents, ["reset", "enter", "update", "leave"]);

room.broadcastEvent({ type: "CANVAS_PING", value: 1 }, undefined, { traceId: "room-trace" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "EMIT",
    id: "emit-4",
    room: "tenant-a:room-api",
    event: "CANVAS_PING",
    payload: { type: "CANVAS_PING", value: 1 },
    meta: { trace_id: "room-trace" },
  }),
);
roomSocket.receive({
  t: "EVENT",
  room: "tenant-a:room-api",
  event: "CANVAS_PING",
  payload: { type: "CANVAS_PING", value: 1 },
  meta: { trace_id: "room-trace", seq: 12 },
});
assert.deepEqual(roomEvents, ["CANVAS_PING"]);
assert.equal(receivedRoomEvents.at(-1)?.sequence, 12);
assert.equal(roomDiagnostics.at(-1)?.sequence, 12);

roomSocket.receive({
  t: "EVENT",
  room: "tenant-a:room-api",
  event: OPENRTC_COMMENT_EVENTS.commentUpdated,
  payload: {
    type: "comment-updated",
    roomId: "tenant-a:room-api",
    threadId: "thread-1",
    commentId: "comment-1",
    thread: { type: "thread", id: "thread-1", roomId: "tenant-a:room-api", comments: [{ id: "comment-1" }], resolved: false },
    comment: { type: "comment", id: "comment-1", threadId: "thread-1", roomId: "tenant-a:room-api", userId: "user-1" },
  },
});
assert.deepEqual(roomEvents, ["CANVAS_PING", OPENRTC_COMMENT_EVENTS.commentUpdated]);
assert.equal(commentEvents.length, 1);
assert.equal(commentEvents[0]?.type, "comment-updated");
assert.equal(commentEvents[0]?.commentId, "comment-1");
assert.equal(commentEvents[0]?.thread.id, "thread-1");

roomSocket.receive({
  t: "NOTIFICATION",
  event: OPENRTC_NOTIFICATION_EVENTS.inboxCreated,
  payload: {
    type: "created",
    userId: "user-1",
    notificationId: "in_1",
    notification: {
      id: "in_1",
      userId: "user-1",
      kind: "thread",
      roomId: "tenant-a:room-api",
      notifiedAt: "2026-07-03T00:00:00Z",
    },
  },
});
assert.equal(notificationEvents.length, 1);
assert.equal(notificationEvents[0]?.event, OPENRTC_NOTIFICATION_EVENTS.inboxCreated);
assert.equal(notificationEvents[0]?.type, "created");
assert.equal(notificationEvents[0]?.notificationId, "in_1");
assert.equal(notificationEvents[0]?.notification?.roomId, "tenant-a:room-api");

const loadedStorage = room.getStorage<{ title: string; version: number }>();
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_GET",
    id: "storage-get-5",
    room: "tenant-a:room-api",
  }),
);
assert.equal(room.getStorageStatus(), "loading");
roomSocket.receive({
  t: "STORAGE_SNAPSHOT",
  id: "storage-get-5",
  room: "tenant-a:room-api",
  payload: { document: { title: "Draft", version: 1 } },
});
assert.deepEqual(await loadedStorage, { title: "Draft", version: 1 });
assert.deepEqual(room.getStorageSnapshot(), { title: "Draft", version: 1 });
assert.deepEqual(storageStatuses.slice(-2), ["loading", "synchronized"]);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Draft", version: 1 },
  source: "snapshot",
});

const setStorage = room.setStorage({ title: "Published", version: 2 }, { opId: "op-set-1" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_SET",
    id: "storage-set-6",
    room: "tenant-a:room-api",
    payload: { title: "Published", version: 2 },
    meta: { op_id: "op-set-1" },
  }),
);
assert.equal(room.getStorageStatus(), "synchronizing");
assert.deepEqual(room.getStorageSnapshot(), { title: "Published", version: 2 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Published", version: 2 },
  source: "optimistic",
  kind: "set",
  opId: "op-set-1",
});
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-set-6",
  room: "tenant-a:room-api",
  payload: { kind: "set", op_id: "op-set-1", document: { title: "Published", version: 20 } },
});
assert.deepEqual(await setStorage, { title: "Published", version: 20 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Published", version: 20 },
  source: "ack",
  kind: "set",
  opId: "op-set-1",
});

const patchStorage = room.patchStorage([{ op: "replace", path: "/title", value: "Patched" }], { opId: "op-patch-1" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-7",
    room: "tenant-a:room-api",
    payload: [{ op: "replace", path: "/title", value: "Patched" }],
    meta: { op_id: "op-patch-1" },
  }),
);
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 20 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Patched", version: 20 },
  source: "optimistic",
  kind: "patch",
  opId: "op-patch-1",
  operations: [{ op: "replace", path: "/title", value: "Patched" }],
});
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-7",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "op-patch-1", document: { title: "Patched", version: 20 } },
});
assert.deepEqual(await patchStorage, { title: "Patched", version: 20 });

roomSocket.receive({
  t: "STORAGE_UPDATE",
  room: "tenant-a:room-api",
  payload: {
    kind: "patch",
    op_id: "remote-op-1",
    origin_conn_id: "room-peer",
    operations: [{ op: "replace", path: "/version", value: 3 }],
    document: { title: "Patched", version: 3 },
  },
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 3 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Patched", version: 3 },
  source: "remote",
  kind: "patch",
  opId: "remote-op-1",
  originConnId: "room-peer",
  operations: [{ op: "replace", path: "/version", value: 3 }],
});

const failedPatch = room.patchStorage([{ op: "replace", path: "/version", value: 4 }], { opId: "op-fail-1" });
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 4 });
roomSocket.receive({
  t: "ERROR",
  id: "storage-patch-8",
  payload: {
    code: "PATCH_FAILED",
    message: "patch failed",
    request_id: "storage-patch-8",
  },
});
await assert.rejects(failedPatch, /patch failed/);
assert.equal(room.getStorageStatus(), "error");
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 3 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Patched", version: 3 },
  source: "rollback",
});

const typedItems = liveList(["a"]);
const typedProps = liveMap({ visible: true });
const typedRoot = liveObject({ title: "Typed Draft", items: typedItems, props: typedProps });
assert.equal(isLiveList(typedItems), true);
assert.equal(isLiveMap(typedProps), true);
assert.equal(isLiveObject(typedRoot), true);
assert.equal(isLiveStorageNode(typedRoot, "LiveObject"), true);
assert.deepEqual(liveObjectPatch({ "a/b": 1 }, { basePath: "/data/props/data" }), [
  { op: "add", path: "/data/props/data/a~1b", value: 1 },
]);

const typedSet = room.setLiveStorage(
  { title: "Typed Draft", items: typedItems, props: typedProps },
  { opId: "typed-set-1" },
);
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_SET",
    id: "storage-set-9",
    room: "tenant-a:room-api",
    payload: typedRoot,
    meta: { op_id: "typed-set-1" },
  }),
);
assert.deepEqual(room.getStorageSnapshot(), typedRoot);
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-set-9",
  room: "tenant-a:room-api",
  payload: { kind: "set", op_id: "typed-set-1", document: typedRoot },
});
assert.deepEqual(await typedSet, typedRoot);

const typedUpdate = room.updateLiveStorage<{ title: string; items: unknown; props: unknown }>(
  { title: "Typed Published" },
  { opId: "typed-update-1" },
);
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-10",
    room: "tenant-a:room-api",
    payload: [{ op: "add", path: "/data/title", value: "Typed Published" }],
    meta: { op_id: "typed-update-1" },
  }),
);
const typedPublished = liveObject({ title: "Typed Published", items: typedItems, props: typedProps });
assert.deepEqual(room.getStorageSnapshot(), typedPublished);
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-10",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "typed-update-1", document: typedPublished },
});
assert.deepEqual(await typedUpdate, typedPublished);

offOthers();
offMyPresence();
offEvents();
offComments();
offNotifications();
offStorage();
offStorageStatus();
let sawClosedRoomReset = false;
const offRoomReset = roomClient.on("room", (state) => {
  if (state.room === "tenant-a:room-api" && state.members.length === 0 && state.others.length === 0) {
    sawClosedRoomReset = true;
  }
});
leave();
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({ t: "LEAVE", id: "leave-11", room: "tenant-a:room-api" }),
);
assert.equal(sawClosedRoomReset, true);
roomSocket.close();
assert.equal(roomClient.status, "closed");
assert.deepEqual(room.getOthers(), []);
offRoomReset();

FakeWebSocket.instances = [];
let reconnectToken = 0;
const reconnectClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: () => `token-reconnect-${++reconnectToken}`,
  WebSocket: FakeWebSocket,
  lostConnectionTimeout: 1000,
  reconnect: { initialDelayMs: 1, maxDelayMs: 1, jitterRatio: 0 },
});
const reconnectConnected = reconnectClient.connect();
await Promise.resolve();
const reconnectSocketA = FakeWebSocket.instances[0];
assert.ok(reconnectSocketA);
reconnectSocketA.open();
await reconnectConnected;
reconnectSocketA.receive({
  t: "HELLO",
  payload: { conn_id: "reconnect-self-a" },
});
const { room: reconnectRoom, leave: leaveReconnectRoom } = reconnectClient.enterRoom("tenant-a:reconnect", {
  initialPresence: {
    user: { id: "user-reconnect", name: "Reconnect User" },
    cursor: { x: 10, y: 20, mode: "pointer" },
  },
});
reconnectSocketA.receive({
  t: "JOINED",
  room: "tenant-a:reconnect",
  payload: {
    members: ["reconnect-self-a", "peer-a"],
    presence: {
      "reconnect-self-a": {
        user: { id: "user-reconnect", name: "Reconnect User" },
        cursor: { x: 10, y: 20 },
      },
      "peer-a": { cursor: { x: 30, y: 40 } },
    },
  },
});
assert.deepEqual(reconnectRoom.getOthers(), [{ connId: "peer-a", state: { cursor: { x: 30, y: 40 } } }]);
reconnectRoom.updatePresence({ cursor: { x: 42, y: 84, mode: "comment" } });
const reconnectStorage = reconnectRoom.getStorage<{ count: number }>();
assert.equal(
  reconnectSocketA.sent.at(-1),
  JSON.stringify({ t: "STORAGE_GET", id: "storage-get-4", room: "tenant-a:reconnect" }),
);
reconnectSocketA.receive({
  t: "STORAGE_SNAPSHOT",
  id: "storage-get-4",
  room: "tenant-a:reconnect",
  payload: { document: { count: 1 } },
});
assert.deepEqual(await reconnectStorage, { count: 1 });
const fastLostEvents: string[] = [];
const offFastLost = reconnectRoom.subscribe("lost-connection", (event) => {
  fastLostEvents.push(event);
});
const reconnectEvents: OpenRTCEvent[] = [];
const offReconnectEvents = reconnectRoom.subscribe("event", (event) => {
  reconnectEvents.push(event);
});
reconnectSocketA.receive({
  t: "EVENT",
  room: "tenant-a:reconnect",
  event: "before-close",
  payload: { ok: true },
  meta: { seq: 9 },
});
assert.equal(reconnectEvents.at(-1)?.sequence, 9);
reconnectSocketA.close();
assert.equal(reconnectClient.status, "reconnecting");
await waitFor(() => FakeWebSocket.instances.length === 2, "expected reconnect socket");
const reconnectSocketB = FakeWebSocket.instances[1];
assert.ok(reconnectSocketB);
reconnectSocketB.open();
reconnectSocketB.receive({
  t: "HELLO",
  payload: { conn_id: "reconnect-self-b" },
});
assert.deepEqual(
  reconnectSocketB.sent.map((item) => JSON.parse(item) as Record<string, unknown>),
  [
    { t: "JOIN", id: "join-5", room: "tenant-a:reconnect", meta: { after_seq: 9 } },
    {
      t: "PRESENCE_SET",
      id: "presence-6",
      room: "tenant-a:reconnect",
      payload: {
        cursor: { x: 42, y: 84, mode: "comment" },
        user: { id: "user-reconnect", name: "Reconnect User" },
      },
    },
    { t: "STORAGE_GET", id: "storage-get-7", room: "tenant-a:reconnect" },
  ],
);
assert.deepEqual(reconnectRoom.getOthers(), [{ connId: "peer-a", state: { cursor: { x: 30, y: 40 } } }]);
reconnectSocketB.receive({
  t: "JOINED",
  room: "tenant-a:reconnect",
  payload: {
    members: ["reconnect-self-b", "peer-b"],
    presence: {
      "reconnect-self-b": {
        user: { id: "user-reconnect", name: "Reconnect User" },
        cursor: { x: 42, y: 84, mode: "comment" },
      },
      "peer-b": { cursor: { x: 55, y: 89 } },
    },
  },
});
assert.equal(reconnectClient.status, "open");
reconnectSocketB.receive({
  t: "EVENT",
  room: "tenant-a:reconnect",
  event: "before-close",
  payload: { duplicate: true },
  meta: { seq: 9 },
});
reconnectSocketB.receive({
  t: "EVENT",
  room: "tenant-a:reconnect",
  event: "after-reconnect",
  payload: { ok: true },
  meta: { seq: 10 },
});
assert.deepEqual(
  reconnectEvents.map((event) => event.event),
  ["before-close", "after-reconnect"],
);
reconnectSocketB.receive({
  t: "STORAGE_SNAPSHOT",
  id: "storage-get-7",
  room: "tenant-a:reconnect",
  payload: { document: { count: 2 } },
});
assert.deepEqual(reconnectRoom.getStorageSnapshot(), { count: 2 });
assert.deepEqual(fastLostEvents, []);
assert.deepEqual(reconnectRoom.getOthers(), [{ connId: "peer-b", state: { cursor: { x: 55, y: 89 } } }]);
await wait(1050);
assert.deepEqual(fastLostEvents, []);
offFastLost();
offReconnectEvents();
leaveReconnectRoom();
reconnectClient.close();

FakeWebSocket.instances = [];
const lostClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-lost",
  WebSocket: FakeWebSocket,
  lostConnectionTimeout: 5,
  reconnect: { initialDelayMs: 1200, maxDelayMs: 1200, jitterRatio: 0 },
});
const lostConnected = lostClient.connect();
await Promise.resolve();
const lostSocketA = FakeWebSocket.instances[0];
assert.ok(lostSocketA);
lostSocketA.open();
await lostConnected;
lostSocketA.receive({
  t: "HELLO",
  payload: { conn_id: "lost-self-a" },
});
const { room: lostRoom, leave: leaveLostRoom } = lostClient.enterRoom("tenant-a:lost", {
  initialPresence: { user: { name: "Lost User" }, cursor: { x: 1, y: 2 } },
});
const lostEvents: string[] = [];
lostRoom.subscribe("lost-connection", (event) => {
  lostEvents.push(event);
});
lostSocketA.receive({
  t: "JOINED",
  room: "tenant-a:lost",
  payload: {
    members: ["lost-self-a", "lost-peer"],
    presence: {
      "lost-self-a": { user: { name: "Lost User" }, cursor: { x: 1, y: 2 } },
      "lost-peer": { cursor: { x: 3, y: 4 } },
    },
  },
});
assert.deepEqual(lostRoom.getOthers(), [{ connId: "lost-peer", state: { cursor: { x: 3, y: 4 } } }]);
lostSocketA.close();
await wait(100);
assert.equal(lostEvents.length, 0);
assert.deepEqual(lostRoom.getOthers(), [{ connId: "lost-peer", state: { cursor: { x: 3, y: 4 } } }]);
await waitFor(() => lostEvents.includes("lost"), "expected lost connection event", 1500);
assert.deepEqual(lostRoom.getOthers(), []);
assert.deepEqual(lostRoom.getMyPresence(), { user: { name: "Lost User" }, cursor: { x: 1, y: 2 } });
await waitFor(() => FakeWebSocket.instances.length === 2, "expected delayed reconnect socket", 1500);
const lostSocketB = FakeWebSocket.instances[1];
assert.ok(lostSocketB);
lostSocketB.open();
lostSocketB.receive({
  t: "HELLO",
  payload: { conn_id: "lost-self-b" },
});
assert.deepEqual(
  lostSocketB.sent.map((item) => JSON.parse(item) as Record<string, unknown>),
  [
    { t: "JOIN", id: "join-3", room: "tenant-a:lost" },
    {
      t: "PRESENCE_SET",
      id: "presence-4",
      room: "tenant-a:lost",
      payload: { user: { name: "Lost User" }, cursor: { x: 1, y: 2 } },
    },
  ],
);
lostSocketB.receive({
  t: "JOINED",
  room: "tenant-a:lost",
  payload: {
    members: ["lost-self-b"],
    presence: {
      "lost-self-b": { user: { name: "Lost User" }, cursor: { x: 1, y: 2 } },
    },
  },
});
assert.deepEqual(lostEvents, ["lost", "restored"]);
leaveLostRoom();
lostClient.close();

FakeWebSocket.instances = [];
const retainedClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-retained",
  WebSocket: FakeWebSocket,
  autoReconnect: false,
});
const retainedConnected = retainedClient.connect();
await Promise.resolve();
const retainedSocket = FakeWebSocket.instances[0];
assert.ok(retainedSocket);
retainedSocket.open();
await retainedConnected;
const firstEntry = retainedClient.enterRoom("tenant-a:retained");
const secondEntry = retainedClient.enterRoom("tenant-a:retained");
assert.deepEqual(
  retainedSocket.sent.map((item) => JSON.parse(item) as Record<string, unknown>),
  [{ t: "JOIN", id: "join-1", room: "tenant-a:retained" }],
);
firstEntry.leave();
assert.equal(retainedSocket.sent.length, 1);
secondEntry.leave();
assert.deepEqual(JSON.parse(retainedSocket.sent.at(-1) ?? "{}"), {
  t: "LEAVE",
  id: "leave-2",
  room: "tenant-a:retained",
});
retainedClient.close();

FakeWebSocket.instances = [];
const preconnectClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-preconnect",
  WebSocket: FakeWebSocket,
  autoReconnect: false,
});
const preconnectEntry = preconnectClient.enterRoom("tenant-a:preconnect", {
  initialPresence: { cursor: null, user: { name: "Preconnect User" } },
});
const preconnectStorage = preconnectEntry.room.getStorage<{ title: string }>();
const preconnectStarted = preconnectClient.connect();
await Promise.resolve();
const preconnectSocket = FakeWebSocket.instances[0];
assert.ok(preconnectSocket);
preconnectSocket.open();
await preconnectStarted;
assert.deepEqual(preconnectSocket.sent, []);
preconnectSocket.receive({
  t: "HELLO",
  payload: { conn_id: "preconnect-self" },
});
assert.deepEqual(
  preconnectSocket.sent.map((item) => JSON.parse(item) as Record<string, unknown>),
  [
    { t: "JOIN", id: "join-3", room: "tenant-a:preconnect" },
    {
      t: "PRESENCE_SET",
      id: "presence-4",
      room: "tenant-a:preconnect",
      payload: { cursor: null, user: { name: "Preconnect User" } },
    },
    { t: "STORAGE_GET", id: "storage-get-5", room: "tenant-a:preconnect" },
  ],
);
preconnectSocket.receive({
  t: "STORAGE_SNAPSHOT",
  id: "storage-get-5",
  room: "tenant-a:preconnect",
  payload: { document: { title: "Preconnect Draft" } },
});
assert.deepEqual(await preconnectStorage, { title: "Preconnect Draft" });
preconnectEntry.leave();
preconnectClient.close();

FakeWebSocket.instances = [];
const fakeDocument = new FakeDocument();
const originalDocumentDescriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
Object.defineProperty(globalThis, "document", { configurable: true, value: fakeDocument });
try {
  const backgroundClient = new OpenRTCClient({
    url: "http://localhost:8080/ws",
    token: "token-background",
    WebSocket: FakeWebSocket,
    backgroundKeepAliveTimeout: 5,
    lostConnectionTimeout: 1000,
    reconnect: { initialDelayMs: 1, maxDelayMs: 1, jitterRatio: 0 },
  });
  assert.equal(fakeDocument.listenerCount("visibilitychange"), 1);
  const backgroundConnected = backgroundClient.connect();
  await Promise.resolve();
  const backgroundSocketA = FakeWebSocket.instances[0];
  assert.ok(backgroundSocketA);
  backgroundSocketA.open();
  await backgroundConnected;
  backgroundSocketA.receive({
    t: "HELLO",
    payload: { conn_id: "background-self-a" },
  });
  const { room: backgroundRoom } = backgroundClient.enterRoom("tenant-a:background", {
    initialPresence: { user: { name: "Background User" }, cursor: { x: 5, y: 10 } },
  });
  backgroundSocketA.receive({
    t: "JOINED",
    room: "tenant-a:background",
    payload: {
      members: ["background-self-a", "background-peer"],
      presence: {
        "background-self-a": { user: { name: "Background User" }, cursor: { x: 5, y: 10 } },
        "background-peer": { cursor: { x: 20, y: 30 } },
      },
    },
  });
  assert.deepEqual(backgroundRoom.getOthers(), [{ connId: "background-peer", state: { cursor: { x: 20, y: 30 } } }]);

  fakeDocument.setHidden(true);
  await waitFor(() => backgroundSocketA.readyState === 3, "expected background keep-alive close");
  assert.equal(backgroundClient.status, "closed");
  assert.deepEqual(backgroundRoom.getMyPresence(), { user: { name: "Background User" }, cursor: { x: 5, y: 10 } });
  assert.deepEqual(backgroundRoom.getOthers(), [{ connId: "background-peer", state: { cursor: { x: 20, y: 30 } } }]);

  fakeDocument.setHidden(false);
  await waitFor(() => FakeWebSocket.instances.length === 2, "expected foreground reconnect");
  const backgroundSocketB = FakeWebSocket.instances[1];
  assert.ok(backgroundSocketB);
  backgroundSocketB.open();
  backgroundSocketB.receive({
    t: "HELLO",
    payload: { conn_id: "background-self-b" },
  });
  assert.deepEqual(
    backgroundSocketB.sent.map((item) => JSON.parse(item) as Record<string, unknown>),
    [
      { t: "JOIN", id: "join-3", room: "tenant-a:background" },
      {
        t: "PRESENCE_SET",
        id: "presence-4",
        room: "tenant-a:background",
        payload: { user: { name: "Background User" }, cursor: { x: 5, y: 10 } },
      },
    ],
  );
  backgroundSocketB.receive({
    t: "JOINED",
    room: "tenant-a:background",
    payload: {
      members: ["background-self-b"],
      presence: {
        "background-self-b": { user: { name: "Background User" }, cursor: { x: 5, y: 10 } },
      },
    },
  });
  assert.equal(backgroundClient.status, "open");
  backgroundClient.destroy();
  assert.equal(fakeDocument.listenerCount("visibilitychange"), 0);
} finally {
  if (originalDocumentDescriptor) {
    Object.defineProperty(globalThis, "document", originalDocumentDescriptor);
  } else {
    delete (globalThis as { document?: unknown }).document;
  }
}

const adminCalls: Array<{ input: string; init?: { method?: string; headers?: Record<string, string>; body?: string } }> = [];
const adminClient = new OpenRTCAdminClient({
  url: "http://localhost:8090/admin/",
  token: () => "admin-token",
  fetch: async (input, init) => {
    adminCalls.push(init ? { input, init } : { input });
    if (input.endsWith("/v1/rooms/tenant-a%3Aroom-1") && init?.method === "DELETE") {
      return fakeResponse(204, "");
    }
    if (input.endsWith("/v1/publish")) {
      return fakeResponse(202, "");
    }
    if (input.endsWith("/v1/presence")) {
      return fakeResponse(202, "");
    }
    if (input.includes("/active_users")) {
      return fakeResponse(200, JSON.stringify({ data: [{ type: "user", connection_id: "conn-1", id: "user-1" }] }));
    }
    if (input.endsWith("/v1/rooms") && init?.method === "POST") {
      return fakeResponse(201, JSON.stringify({ id: "tenant-a:room-1", metadata: { title: "Room" } }));
    }
    if (input.includes("/v1/rooms?")) {
      return fakeResponse(200, JSON.stringify({ rooms: [{ id: "tenant-a:room-1" }], next_cursor: "1" }));
    }
    if (input.endsWith("/threads") && init?.method === "POST") {
      return fakeResponse(
        201,
        JSON.stringify({ type: "thread", id: "thread-1", roomId: "tenant-a:room-1", comments: [], resolved: false }),
      );
    }
    if (input.endsWith("/comments")) {
      return fakeResponse(
        201,
        JSON.stringify({ type: "thread", id: "thread-1", roomId: "tenant-a:room-1", comments: [{ id: "comment-2" }], resolved: false }),
      );
    }
    if (input.endsWith("/comments/comment-1") && init?.method === "PATCH") {
      return fakeResponse(
        200,
        JSON.stringify({
          type: "thread",
          id: "thread-1",
          roomId: "tenant-a:room-1",
          comments: [{ id: "comment-1", mentions: ["user-2"], reactions: [{ emoji: "+1", userId: "user-2" }] }],
          resolved: false,
        }),
      );
    }
    if (input.endsWith("/inbox-notifications/trigger")) {
      return fakeResponse(201, JSON.stringify({ id: "in_1", userId: "user-1", kind: "$custom", notifiedAt: "now" }));
    }
    if (input.endsWith("/notification-settings") && init?.method === "POST") {
      return fakeResponse(200, init.body ?? "{}");
    }
    if (input.endsWith("/subscription-settings") && init?.method === "POST") {
      return fakeResponse(
        200,
        JSON.stringify({ roomId: "tenant-a:room-1", userId: "user-1", threads: "none", textMentions: "none" }),
      );
    }
    if (input.endsWith("/v1/stats")) {
      return fakeResponse(200, JSON.stringify({ activeConnections: 1 }));
    }
    if (input.endsWith("/broken")) {
      return fakeResponse(403, JSON.stringify({ code: "ROOM_FORBIDDEN", message: "no access" }), false);
    }
    return fakeResponse(200, JSON.stringify({ ok: true }));
  },
});

await adminClient.publish("tenant-a:room-1", "debug.ping", { ok: true }, { traceId: "trace-1" });
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  room: "tenant-a:room-1",
  event: "debug.ping",
  payload: { ok: true },
  trace_id: "trace-1",
});
assert.equal(adminCalls.at(-1)?.init?.headers?.Authorization, "Bearer admin-token");

await adminClient.setPresence("tenant-a:room-1", "agent-1", { cursor: { x: 1, y: 2 } }, { ttlSeconds: 30 });
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  room: "tenant-a:room-1",
  conn_id: "agent-1",
  state: { cursor: { x: 1, y: 2 } },
  ttl_seconds: 30,
});

const createdRoom = await adminClient.createRoom({ id: "tenant-a:room-1", metadata: { title: "Room" } });
assert.deepEqual(createdRoom, { id: "tenant-a:room-1", metadata: { title: "Room" } });

const rooms = await adminClient.listRooms({
  prefix: "tenant-a:",
  limit: 10,
  cursor: "0",
  query: `metadata.title:"Room" metadata.public:true`,
});
assert.deepEqual(rooms, { rooms: [{ id: "tenant-a:room-1" }], next_cursor: "1" });
assert.equal(
  adminCalls.at(-1)?.input,
  "http://localhost:8090/admin/v1/rooms?prefix=tenant-a%3A&limit=10&cursor=0&query=metadata.title%3A%22Room%22+metadata.public%3Atrue",
);

const activeUsers = await adminClient.activeUsers("tenant-a:room-1");
assert.deepEqual(activeUsers.data, [{ type: "user", connection_id: "conn-1", id: "user-1" }]);

const thread = await adminClient.createThread("tenant-a:room-1", {
  comment: { userId: "user-1", body: { type: "text", text: "hello" }, mentions: ["user-2"] },
});
assert.equal(thread.id, "thread-1");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  comment: { userId: "user-1", body: { type: "text", text: "hello" }, mentions: ["user-2"] },
});

const updatedThread = await adminClient.addComment("tenant-a:room-1", "thread-1", {
  userId: "user-2",
  body: { type: "text", text: "reply" },
  reactions: [{ emoji: "+1", userId: "user-1" }],
});
assert.deepEqual(updatedThread.comments, [{ id: "comment-2" }]);

const patchedThread = await adminClient.updateComment("tenant-a:room-1", "thread-1", "comment-1", {
  body: { type: "text", text: "edited" },
  metadata: { status: "resolved" },
  mentions: ["user-2"],
  reactions: [{ emoji: "+1", userId: "user-2" }],
});
const patchedComment = patchedThread.comments[0];
assert.ok(patchedComment);
assert.deepEqual(patchedComment.reactions, [{ emoji: "+1", userId: "user-2" }]);
assert.equal(adminCalls.at(-1)?.input, "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/comments/comment-1");
assert.equal(adminCalls.at(-1)?.init?.method, "PATCH");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  body: { type: "text", text: "edited" },
  metadata: { status: "resolved" },
  mentions: ["user-2"],
  reactions: [{ emoji: "+1", userId: "user-2" }],
});

const notification = await adminClient.triggerInboxNotification({
  userId: "user-1",
  kind: "$custom",
  roomId: "tenant-a:room-1",
});
assert.equal(notification.id, "in_1");

assert.deepEqual(await adminClient.setNotificationSettings("user-1", { email: true }), { email: true });
assert.deepEqual(await adminClient.setRoomSubscriptionSettings("tenant-a:room-1", "user-1", { threads: "none", textMentions: "none" }), {
  roomId: "tenant-a:room-1",
  userId: "user-1",
  threads: "none",
  textMentions: "none",
});
assert.deepEqual(await adminClient.stats(), { activeConnections: 1 });
await adminClient.deleteRoom("tenant-a:room-1");

await assert.rejects(
  async () => {
    await (adminClient as unknown as { request<T>(path: string): Promise<T> }).request("/broken");
  },
  (error) => error instanceof OpenRTCAdminError && error.status === 403 && error.message === "ROOM_FORBIDDEN: no access",
);

function fakeResponse(status: number, text: string, ok = status >= 200 && status < 300) {
  return {
    ok,
    status,
    statusText: ok ? "OK" : "Forbidden",
    async text(): Promise<string> {
      return text;
    },
  };
}

async function wait(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitFor(condition: () => boolean, message: string, timeoutMs = 500): Promise<void> {
  const started = Date.now();
  while (Date.now() - started < timeoutMs) {
    if (condition()) {
      return;
    }
    await wait(5);
  }
  throw new Error(message);
}

const acked = client.broadcastWithAck(
  "tenant-a:doc",
  "presence.ping",
  { ok: true },
  { traceId: "trace-test", timeoutMs: 1000 },
);
assert.match(socket.sent.at(-1) ?? "", /"trace_id":"trace-test"/);
socket.receive({
  t: "EVENT",
  room: "tenant-a:doc",
  event: "presence.ping",
  payload: { ok: true },
  meta: { trace_id: "trace-test" },
});
assert.deepEqual(await acked, {
  room: "tenant-a:doc",
  event: "presence.ping",
  payload: { ok: true },
  traceId: "trace-test",
});
assert.ok(
  diagnostics.some(
    (event) =>
      event.direction === "in" &&
      event.t === "EVENT" &&
      event.traceId === "trace-test" &&
      typeof event.latencyMs === "number",
  ),
);

const timedOutAck = client.broadcastWithAck(
  "tenant-a:doc",
  "presence.timeout",
  { ok: false },
  { traceId: "trace-timeout", timeoutMs: 5 },
);
assert.match(socket.sent.at(-1) ?? "", /"trace_id":"trace-timeout"/);
await assert.rejects(timedOutAck, /Timed out waiting for presence\.timeout ack in tenant-a:doc/);

const diagnosticsBeforeLateAck = diagnostics.length;
socket.receive({
  t: "EVENT",
  room: "tenant-a:doc",
  event: "presence.timeout",
  payload: { late: true },
  meta: { trace_id: "trace-timeout" },
});
const lateAckDiagnostics = diagnostics
  .slice(diagnosticsBeforeLateAck)
  .filter((event) => event.direction === "in" && event.t === "EVENT" && event.traceId === "trace-timeout");
assert.equal(lateAckDiagnostics.length, 1);
assert.equal(lateAckDiagnostics[0]?.latencyMs, undefined);
