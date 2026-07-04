import assert from "node:assert/strict";
import {
  OpenRTCAdminClient,
  OpenRTCAdminError,
  OpenRTCClient,
  OpenRTCDevError,
  OPENRTC_COMMENT_EVENTS,
  OPENRTC_DEV_PUBLIC_KEY,
  OPENRTC_NOTIFICATION_EVENTS,
  OPENRTC_ROOM_SUBSCRIPTION_PRESETS,
  OPENRTC_ROOM_PERMISSIONS,
  accessMatrixPermissions,
  accessMatrixPolicy,
  accessMatrixRoomAccesses,
  accessMatrixScope,
  accessMatrixScopes,
  applyCommentEventToThreads,
  applyCommentEventsToThreads,
  applyNotificationDeltaToInbox,
  applyNotificationDeltasToInbox,
  removeCommentThread,
  addCommentMention,
  addCommentReaction,
  createLiveStorageMutation,
  createOpenRTCDevAdminClient,
  createOpenRTCDevClient,
  createOpenRTCDevTools,
  fetchOpenRTCDevConfig,
  fetchOpenRTCDevToken,
  getCursorPeers,
  getPresenceColor,
  getPresenceCursor,
  getPresenceUser,
  isOpenRTCCursor,
  isLiveList,
  isLiveMap,
  isLiveObject,
  isLiveStorageNode,
  liveListAppend,
  liveListInsert,
  liveListMove,
  liveListRemove,
  liveListReplace,
  liveMapDelete,
  liveMapPatch,
  liveList,
  liveMap,
  liveObjectDelete,
  liveObject,
  liveObjectPatch,
  liveStorageMutation,
  normalizeCommentMentions,
  normalizeCommentReactions,
  removeCommentMention,
  removeCommentReaction,
  roomQuery,
  roomQueryExists,
  roomSubscriptionSettingsInput,
  runOpenRTCDevProbe,
  sortCommentThreads,
  sortInboxNotifications,
  threadQuery,
  threadQueryExists,
  type OpenRTCAdminInboxNotification,
  type OpenRTCAdminThread,
  type OpenRTCCommentEvent,
  type OpenRTCDiagnosticEvent,
  type OpenRTCError,
  type OpenRTCEvent,
  type OpenRTCNotificationDelta,
  type OpenRTCStorageEvent,
  type OpenRTCStorageHistoryEvent,
  type OpenRTCStorageStatusUpdate,
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
assert.deepEqual(accessMatrixScopes({
  room: "write",
  storage: "read",
  comments: "none",
  feeds: "write",
}, "tenant-a:*"), [
  "room:write:tenant-a:*",
  "storage:read:tenant-a:*",
  "feeds:write:tenant-a:*",
]);
assert.equal(
  accessMatrixScope({ room: "read", storage: "write", comments: "write" }, "tenant-a:canvas-1"),
  "room:read:tenant-a:canvas-1 storage:write:tenant-a:canvas-1 comments:write:tenant-a:canvas-1",
);
assert.deepEqual(accessMatrixRoomAccesses({
  default: {
    room: "read",
    storage: "read",
  },
  users: {
    "user-denied": {
      room: "none",
      storage: "none",
      comments: "none",
      feeds: "none",
    },
    "user-storage": {
      storage: "write",
    },
  },
  groups: {
    editors: {
      room: "write",
      storage: "write",
      comments: "write",
    },
  },
}), {
  defaultAccesses: [OPENRTC_ROOM_PERMISSIONS.roomRead, OPENRTC_ROOM_PERMISSIONS.storageRead],
  usersAccesses: {
    "user-denied": [],
    "user-storage": [OPENRTC_ROOM_PERMISSIONS.storageWrite],
  },
  groupsAccesses: {
    editors: [
      OPENRTC_ROOM_PERMISSIONS.roomWrite,
      OPENRTC_ROOM_PERMISSIONS.storageWrite,
      OPENRTC_ROOM_PERMISSIONS.commentsWrite,
    ],
  },
});
const editorAccessPolicy = accessMatrixPolicy({
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
    },
    users: {
      "blocked-user": {
        room: "none",
        storage: "none",
        comments: "none",
      },
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
assert.deepEqual(editorAccessPolicy.subjectPermissions, [
  OPENRTC_ROOM_PERMISSIONS.roomWrite,
  OPENRTC_ROOM_PERMISSIONS.storageWrite,
  OPENRTC_ROOM_PERMISSIONS.commentsWrite,
]);
assert.deepEqual(editorAccessPolicy.subjectScopes, [
  "room:write:tenant-a:*",
  "storage:write:tenant-a:*",
  "comments:write:tenant-a:*",
]);
assert.equal(editorAccessPolicy.subjectScope, "room:write:tenant-a:* storage:write:tenant-a:* comments:write:tenant-a:*");
assert.deepEqual(editorAccessPolicy.tokenClaims, {
  scope: "room:write:tenant-a:* storage:write:tenant-a:* comments:write:tenant-a:*",
});
assert.deepEqual(editorAccessPolicy.devClientTokenOptions, { access: "grants" });
assert.deepEqual(editorAccessPolicy.roomInput({ id: "tenant-a:canvas-1", metadata: { type: "whiteboard" } }), {
  id: "tenant-a:canvas-1",
  metadata: { type: "whiteboard" },
  defaultAccesses: [OPENRTC_ROOM_PERMISSIONS.roomRead, OPENRTC_ROOM_PERMISSIONS.storageRead],
  usersAccesses: {
    "blocked-user": [],
  },
  groupsAccesses: {
    editors: [
      OPENRTC_ROOM_PERMISSIONS.roomWrite,
      OPENRTC_ROOM_PERMISSIONS.storageWrite,
      OPENRTC_ROOM_PERMISSIONS.commentsWrite,
    ],
  },
});
assert.deepEqual(editorAccessPolicy.roomUpdate({ metadata: { archived: false } }), {
  metadata: { archived: false },
  defaultAccesses: [OPENRTC_ROOM_PERMISSIONS.roomRead, OPENRTC_ROOM_PERMISSIONS.storageRead],
  usersAccesses: {
    "blocked-user": [],
  },
  groupsAccesses: {
    editors: [
      OPENRTC_ROOM_PERMISSIONS.roomWrite,
      OPENRTC_ROOM_PERMISSIONS.storageWrite,
      OPENRTC_ROOM_PERMISSIONS.commentsWrite,
    ],
  },
});
assert.throws(() => accessMatrixScopes({ room: "read" }, ""), /non-empty string/);
assert.throws(() => accessMatrixScope({ room: "read" }, "tenant-a:* storage:*"), /cannot contain whitespace/);

assert.equal(
  roomQuery({
    id: "tenant-a:canvas-1",
    "metadata.type": "whiteboard",
    "metadata.archived": false,
    "metadata.priority": 2,
    "metadata.owner": null,
    "metadata.tags": roomQueryExists,
  }),
  'id:"tenant-a:canvas-1" metadata.type:"whiteboard" metadata.archived:false metadata.priority:2 metadata.owner:null metadata.tags:*',
);
assert.equal(roomQuery({}), "");
assert.throws(() => roomQuery({ owner: "user-1" }), /fields must be id or metadata/);
assert.throws(() => roomQuery({ id: roomQueryExists }), /id field does not support exists/);
assert.throws(() => roomQuery({ id: "" }), /id value must be a non-empty string/);
assert.throws(() => roomQuery({ "metadata.bad/key": "x" }), /metadata path keys/);
assert.throws(() => roomQuery({ "metadata.score": Number.NaN }), /number values must be finite/);

assert.equal(
  threadQuery({
    resolved: false,
    unread: false,
    "metadata.status": "open",
    "metadata.priority": 2,
    "metadata.owner": threadQueryExists,
  }),
  'resolved:false unread:false metadata.status:"open" metadata.priority:2 metadata.owner:*',
);
assert.equal(threadQuery({}), "");
assert.throws(() => threadQuery({ id: "thread-1" }), /fields must be resolved, unread, or metadata/);
assert.throws(() => threadQuery({ resolved: threadQueryExists }), /resolved field does not support exists/);
assert.throws(() => threadQuery({ resolved: "false" }), /resolved value must be a boolean/);
assert.throws(() => threadQuery({ unread: threadQueryExists }), /unread field does not support exists/);
assert.throws(() => threadQuery({ unread: "false" }), /unread value must be a boolean/);
assert.throws(() => threadQuery({ "metadata.bad/key": "x" }), /metadata path keys/);
assert.throws(() => threadQuery({ "metadata.score": Number.NaN }), /number values must be finite/);

assert.deepEqual(normalizeCommentMentions([" user-2 ", "", "user-2", "user-3"]), ["user-2", "user-3"]);
assert.deepEqual(addCommentMention(["user-2"], " user-3 "), ["user-2", "user-3"]);
assert.deepEqual(removeCommentMention(["user-2", "user-3"], " user-2 "), ["user-3"]);
assert.deepEqual(
  normalizeCommentReactions([
    { emoji: " +1 ", userId: " user-2 " },
    { emoji: "+1", userId: "user-2" },
    { emoji: "", userId: "user-3" },
    { emoji: "eyes", userId: "user-3" },
  ]),
  [
    { emoji: "+1", userId: "user-2" },
    { emoji: "eyes", userId: "user-3" },
  ],
);
assert.deepEqual(addCommentReaction([{ emoji: "+1", userId: "user-2" }], { emoji: "eyes", userId: " user-3 " }), [
  { emoji: "+1", userId: "user-2" },
  { emoji: "eyes", userId: "user-3" },
]);
assert.deepEqual(
  removeCommentReaction(
    [
      { emoji: "+1", userId: "user-2" },
      { emoji: "eyes", userId: "user-3" },
    ],
    { emoji: " +1 ", userId: "user-2" },
  ),
  [{ emoji: "eyes", userId: "user-3" }],
);
assert.deepEqual(OPENRTC_ROOM_SUBSCRIPTION_PRESETS.all, { threads: "all", textMentions: "mine" });
assert.deepEqual(roomSubscriptionSettingsInput("replies_and_mentions"), {
  threads: "replies_and_mentions",
  textMentions: "mine",
});
const mutableSubscriptionInput = roomSubscriptionSettingsInput("none");
mutableSubscriptionInput.threads = "all";
assert.deepEqual(roomSubscriptionSettingsInput("none"), { threads: "none", textMentions: "none" });

const baseThread: OpenRTCAdminThread = {
  type: "thread",
  id: "thread-2",
  roomId: "tenant-a:room-1",
  comments: [
    {
      type: "comment",
      threadId: "thread-2",
      roomId: "tenant-a:room-1",
      id: "comment-2",
      userId: "user-2",
      createdAt: "2026-07-04T00:02:00.000Z",
      body: { text: "second" },
    },
  ],
  resolved: false,
  createdAt: "2026-07-04T00:02:00.000Z",
  updatedAt: "2026-07-04T00:02:00.000Z",
};
const earlierThread: OpenRTCAdminThread = {
  type: "thread",
  id: "thread-1",
  roomId: "tenant-a:room-1",
  comments: [
    {
      type: "comment",
      threadId: "thread-1",
      roomId: "tenant-a:room-1",
      id: "comment-1",
      userId: "user-1",
      createdAt: "2026-07-04T00:01:00.000Z",
      body: { text: "first" },
    },
  ],
  resolved: false,
  metadata: { status: "open" },
  createdAt: "2026-07-04T00:01:00.000Z",
  updatedAt: "2026-07-04T00:01:00.000Z",
};
const updatedEarlierThread: OpenRTCAdminThread = {
  ...earlierThread,
  comments: [
    {
      ...earlierThread.comments[0]!,
      body: { text: "first edited" },
      editedAt: "2026-07-04T00:03:00.000Z",
      reactions: [{ emoji: "+1", userId: "user-2" }],
    },
  ],
  updatedAt: "2026-07-04T00:03:00.000Z",
};
const resolvedEarlierThread: OpenRTCAdminThread = {
  ...updatedEarlierThread,
  resolved: true,
  metadata: { status: "resolved" },
  updatedAt: "2026-07-04T00:04:00.000Z",
};
const threadCreatedEvent: OpenRTCCommentEvent = {
  room: "tenant-a:room-1",
  event: OPENRTC_COMMENT_EVENTS.threadCreated,
  type: "thread-created",
  roomId: "tenant-a:room-1",
  threadId: "thread-1",
  commentId: "comment-1",
  thread: earlierThread,
  comment: earlierThread.comments[0]!,
};
const commentUpdatedEvent: OpenRTCCommentEvent = {
  room: "tenant-a:room-1",
  event: OPENRTC_COMMENT_EVENTS.commentUpdated,
  type: "comment-updated",
  roomId: "tenant-a:room-1",
  threadId: "thread-1",
  commentId: "comment-1",
  thread: updatedEarlierThread,
  comment: updatedEarlierThread.comments[0]!,
};
const threadUpdatedEvent: OpenRTCCommentEvent = {
  room: "tenant-a:room-1",
  event: OPENRTC_COMMENT_EVENTS.threadUpdated,
  type: "thread-updated",
  roomId: "tenant-a:room-1",
  threadId: "thread-1",
  thread: resolvedEarlierThread,
};
const threadDeletedEvent: OpenRTCCommentEvent = {
  room: "tenant-a:room-1",
  event: OPENRTC_COMMENT_EVENTS.threadDeleted,
  type: "thread-deleted",
  roomId: "tenant-a:room-1",
  threadId: "thread-1",
  thread: resolvedEarlierThread,
};
assert.deepEqual(sortCommentThreads([baseThread, earlierThread]).map((thread) => thread.id), ["thread-1", "thread-2"]);
const materializedThreads = applyCommentEventsToThreads([baseThread], [
  threadCreatedEvent,
  commentUpdatedEvent,
  threadUpdatedEvent,
]);
assert.deepEqual(materializedThreads.map((thread) => thread.id), ["thread-1", "thread-2"]);
assert.deepEqual(materializedThreads[0]?.comments[0]?.body, { text: "first edited" });
assert.equal(materializedThreads[0]?.resolved, true);
assert.deepEqual(applyCommentEventToThreads(materializedThreads, threadUpdatedEvent), materializedThreads);
const readAwareThreads = applyCommentEventToThreads(
  [{ ...earlierThread, readAt: "2026-07-04T00:03:00.000Z", unread: false }],
  threadUpdatedEvent,
);
assert.equal(readAwareThreads[0]?.readAt, "2026-07-04T00:03:00.000Z");
assert.equal(readAwareThreads[0]?.unread, true);
assert.deepEqual(applyCommentEventToThreads(materializedThreads, threadDeletedEvent).map((thread) => thread.id), [
  "thread-2",
]);
assert.deepEqual(removeCommentThread(materializedThreads, "thread-1").map((thread) => thread.id), ["thread-2"]);
updatedEarlierThread.comments[0]!.reactions![0]!.emoji = "mutated";
assert.deepEqual(materializedThreads[0]?.comments[0]?.reactions, [{ emoji: "+1", userId: "user-2" }]);

const olderNotification: OpenRTCAdminInboxNotification = {
  id: "in-older",
  userId: "user-1",
  kind: "thread",
  roomId: "tenant-a:room-1",
  notifiedAt: "2026-07-04T00:01:00.000Z",
  activityData: { threadId: "thread-1" },
};
const newerNotification: OpenRTCAdminInboxNotification = {
  id: "in-newer",
  userId: "user-1",
  kind: "thread",
  roomId: "tenant-a:room-1",
  notifiedAt: "2026-07-04T00:02:00.000Z",
  activityData: { threadId: "thread-2" },
};
const otherUserNotification: OpenRTCAdminInboxNotification = {
  id: "in-other",
  userId: "user-2",
  kind: "thread",
  roomId: "tenant-a:room-1",
  notifiedAt: "2026-07-04T00:03:00.000Z",
};
assert.deepEqual(
  sortInboxNotifications([olderNotification, newerNotification, otherUserNotification], { userId: "user-1" }).map(
    (notification) => notification.id,
  ),
  ["in-newer", "in-older"],
);
assert.deepEqual(
  applyNotificationDeltaToInbox(
    [olderNotification],
    {
      event: OPENRTC_NOTIFICATION_EVENTS.inboxCreated,
      type: "created",
      userId: "user-1",
      notificationId: "in-newer",
      notification: newerNotification,
    },
    { userId: "user-1", limit: 1 },
  ).map((notification) => notification.id),
  ["in-newer"],
);
const readDelta: OpenRTCNotificationDelta = {
  event: OPENRTC_NOTIFICATION_EVENTS.inboxRead,
  type: "read",
  userId: "user-1",
  notificationId: "in-newer",
  notification: { ...newerNotification, readAt: "2026-07-04T00:03:00.000Z" },
};
assert.deepEqual(applyNotificationDeltaToInbox([newerNotification], readDelta, { userId: "user-1", unreadOnly: true }), []);
assert.deepEqual(
  applyNotificationDeltasToInbox(
    [olderNotification, newerNotification],
    [
      { event: OPENRTC_NOTIFICATION_EVENTS.inboxDeleted, type: "deleted", userId: "user-1", notificationId: "in-older" },
      { event: OPENRTC_NOTIFICATION_EVENTS.inboxDeletedAll, type: "deleted-all", userId: "user-1" },
    ],
    { userId: "user-1" },
  ),
  [],
);

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

function latestSentMessage(socket: FakeWebSocket, type: string): Record<string, unknown> {
  for (let index = socket.sent.length - 1; index >= 0; index -= 1) {
    const message = JSON.parse(socket.sent[index] ?? "{}") as Record<string, unknown>;
    if (message.t === type) {
      return message;
    }
  }
  throw new Error(`Expected sent ${type} message`);
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
const errorEvents: OpenRTCError[] = [];
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
const offErrors = roomClient.on("error", (event) => {
  errorEvents.push(event);
});
const storageEvents: OpenRTCStorageEvent[] = [];
const storageStatuses: string[] = [];
const roomStorageStatusUpdates: OpenRTCStorageStatusUpdate[] = [];
const storageStatusUpdates: unknown[] = [];
const offStorage = room.subscribe("storage", (event) => {
  storageEvents.push(event);
});
const offStorageStatus = room.subscribe("storage-status", (status, update) => {
  storageStatuses.push(status);
  roomStorageStatusUpdates.push(update);
});
const offStorageStatusUpdates = roomClient.on("storage-status", (event) => {
  if (event.room === "tenant-a:room-api") {
    storageStatusUpdates.push(event);
  }
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
assert.deepEqual(room.getStoragePendingMutations(), []);
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
assert.deepEqual(room.getStoragePendingMutations(), [
  { requestId: "storage-set-6", kind: "set", opId: "op-set-1" },
]);
assert.deepEqual(roomStorageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronizing",
  pendingMutations: 1,
  pendingOpIds: ["op-set-1"],
});
assert.deepEqual(storageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronizing",
  pendingMutations: 1,
  pendingOpIds: ["op-set-1"],
});
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
  meta: { seq: 1 },
  payload: { kind: "set", op_id: "op-set-1", document: { title: "Published", version: 20 } },
});
assert.deepEqual(await setStorage, { title: "Published", version: 20 });
assert.equal(room.getStorageSequence(), 1);
assert.deepEqual(room.getStoragePendingMutations(), []);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Published", version: 20 },
  source: "ack",
  kind: "set",
  opId: "op-set-1",
  sequence: 1,
});
assert.deepEqual(storageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronized",
  pendingMutations: 0,
  pendingOpIds: [],
  sequence: 1,
});

const patchStorage = room.patchStorage([{ op: "replace", path: "/title", value: "Patched" }], {
  opId: "op-patch-1",
  expectedSequence: 1,
});
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-7",
    room: "tenant-a:room-api",
    payload: [{ op: "replace", path: "/title", value: "Patched" }],
    meta: { op_id: "op-patch-1", expected_seq: 1 },
  }),
);
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 20 });
assert.deepEqual(room.getStoragePendingMutations(), [
  {
    requestId: "storage-patch-7",
    kind: "patch",
    opId: "op-patch-1",
    operations: [{ op: "replace", path: "/title", value: "Patched" }],
  },
]);
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
  meta: { seq: 2 },
  payload: {
    kind: "patch",
    op_id: "remote-op-1",
    origin_conn_id: "room-peer",
    operations: [{ op: "replace", path: "/version", value: 3 }],
    document: { title: "Patched", version: 3 },
  },
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 3 });
assert.equal(room.getStorageSequence(), 2);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Patched", version: 3 },
  source: "remote",
  kind: "patch",
  opId: "remote-op-1",
  sequence: 2,
  originConnId: "room-peer",
  operations: [{ op: "replace", path: "/version", value: 3 }],
});
const eventsAfterSequencedRemote = storageEvents.length;
roomSocket.receive({
  t: "STORAGE_UPDATE",
  room: "tenant-a:room-api",
  meta: { seq: 1 },
  payload: {
    kind: "set",
    op_id: "stale-op-1",
    origin_conn_id: "room-peer",
    document: { title: "Stale", version: 1 },
  },
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 3 });
assert.equal(room.getStorageSequence(), 2);
assert.equal(storageEvents.length, eventsAfterSequencedRemote);

const failedPatch = room.patchStorage([{ op: "replace", path: "/version", value: 4 }], {
  opId: "op-fail-1",
  expectedSequence: 1,
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 4 });
roomSocket.receive({
  t: "ERROR",
  id: "storage-patch-8",
  room: "tenant-a:room-api",
  payload: {
    code: "STORAGE_CONFLICT",
    message: "storage conflict",
    request_id: "storage-patch-8",
    room: "tenant-a:room-api",
    sequence: 2,
    document: { title: "Patched", version: 30 },
  },
});
await assert.rejects(failedPatch, /storage conflict/);
assert.equal(room.getStorageStatus(), "synchronized");
assert.deepEqual(room.getStorageSnapshot(), { title: "Patched", version: 30 });
assert.equal(room.getStorageSequence(), 2);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Patched", version: 30 },
  source: "repair",
  sequence: 2,
});
assert.deepEqual(errorEvents.at(-1), {
  code: "STORAGE_CONFLICT",
  message: "storage conflict",
  requestId: "storage-patch-8",
  storageRepair: {
    room: "tenant-a:room-api",
    document: { title: "Patched", version: 30 },
    sequence: 2,
  },
});
await assert.rejects(
  room.patchStorage([{ op: "replace", path: "/version", value: 5 }], { expectedSequence: 1.5 }),
  /expectedSequence/,
);

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
assert.deepEqual(liveObjectDelete("a/b", { basePath: "/data/props/data" }), [
  { op: "remove", path: "/data/props/data/a~1b" },
]);
assert.deepEqual(liveMapPatch({ "a/b": false }, { basePath: "/data/props/data" }), [
  { op: "add", path: "/data/props/data/a~1b", value: false },
]);
assert.deepEqual(liveMapDelete(["visible", "a/b"], { basePath: "/data/props/data" }), [
  { op: "remove", path: "/data/props/data/visible" },
  { op: "remove", path: "/data/props/data/a~1b" },
]);
assert.deepEqual(liveListAppend("b", { basePath: "/data/items/data" }), [
  { op: "add", path: "/data/items/data/-", value: "b" },
]);
assert.deepEqual(liveListInsert(0, "z", { basePath: "/data/items/data" }), [
  { op: "add", path: "/data/items/data/0", value: "z" },
]);
assert.deepEqual(liveListReplace(1, "B", { basePath: "/data/items/data" }), [
  { op: "replace", path: "/data/items/data/1", value: "B" },
]);
assert.deepEqual(liveListRemove(2, { basePath: "/data/items/data" }), [
  { op: "remove", path: "/data/items/data/2" },
]);
assert.deepEqual(liveListMove(0, 1, { basePath: "/data/items/data" }), [
  { op: "move", from: "/data/items/data/0", path: "/data/items/data/1" },
]);
const typedMutationBuilder = createLiveStorageMutation();
typedMutationBuilder.object().set({ "a/b": 1 }).delete("draftNote");
typedMutationBuilder.list<string>("items").append("b");
typedMutationBuilder.map("props").set({ "a/b": false }).delete("visible");
const typedMutationOperations = [
  { op: "add", path: "/data/a~1b", value: 1 },
  { op: "remove", path: "/data/draftNote" },
  { op: "add", path: "/data/items/data/-", value: "b" },
  { op: "add", path: "/data/props/data/a~1b", value: false },
  { op: "remove", path: "/data/props/data/visible" },
];
assert.deepEqual(typedMutationBuilder.toJSONPatch(), typedMutationOperations);
assert.deepEqual(liveStorageMutation(typedMutationBuilder), typedMutationOperations);
assert.deepEqual(
  liveStorageMutation((storage) => {
    storage.object().set({ title: "Next" });
    return liveListAppend("c", { basePath: "/data/items/data" });
  }),
  [
    { op: "add", path: "/data/title", value: "Next" },
    { op: "add", path: "/data/items/data/-", value: "c" },
  ],
);
assert.throws(() => liveStorageMutation(() => undefined), /at least one operation/);
assert.throws(() => liveMapPatch({}, { basePath: "/data/props/data" }), /LiveMap patch must contain/);
assert.throws(() => liveObjectDelete([], { basePath: "/data/props/data" }), /delete must include/);
assert.throws(() => liveMapDelete("", { basePath: "/data/props/data" }), /non-empty strings/);
assert.throws(() => liveListInsert(-1, "x", { basePath: "/data/items/data" }), /non-negative integer/);

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

const typedCollectionUpdate = room.mutateLiveStorage((storage) => {
  storage.list<string>("items").append("b");
  storage.map<{ visible: boolean }>("props").set({ visible: false });
}, { opId: "typed-collections-1" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-10",
    room: "tenant-a:room-api",
    payload: [
      { op: "add", path: "/data/items/data/-", value: "b" },
      { op: "add", path: "/data/props/data/visible", value: false },
    ],
    meta: { op_id: "typed-collections-1" },
  }),
);
const typedCollections = liveObject({
  title: "Typed Draft",
  items: liveList(["a", "b"]),
  props: liveMap({ visible: false }),
});
assert.deepEqual(room.getStorageSnapshot(), typedCollections);
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-10",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "typed-collections-1", document: typedCollections },
});
assert.deepEqual(await typedCollectionUpdate, typedCollections);

const typedUpdate = room.updateLiveStorage<{ title: string; items: unknown; props: unknown }>(
  { title: "Typed Published" },
  { opId: "typed-update-1" },
);
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-11",
    room: "tenant-a:room-api",
    payload: [{ op: "add", path: "/data/title", value: "Typed Published" }],
    meta: { op_id: "typed-update-1" },
  }),
);
const typedPublished = liveObject({
  title: "Typed Published",
  items: liveList(["a", "b"]),
  props: liveMap({ visible: false }),
});
assert.deepEqual(room.getStorageSnapshot(), typedPublished);
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-11",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "typed-update-1", document: typedPublished },
});
assert.deepEqual(await typedUpdate, typedPublished);

const autoOpSet = room.setStorage({ title: "Auto Op", version: 30 });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_SET",
    id: "storage-set-12",
    room: "tenant-a:room-api",
    payload: { title: "Auto Op", version: 30 },
    meta: { op_id: "storage-set-12" },
  }),
);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Auto Op", version: 30 },
  source: "optimistic",
  kind: "set",
  opId: "storage-set-12",
});
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-set-12",
  room: "tenant-a:room-api",
  payload: { kind: "set", op_id: "storage-set-12", document: { title: "Auto Op", version: 31 } },
});
assert.deepEqual(await autoOpSet, { title: "Auto Op", version: 31 });

const concurrentPatchA = room.patchStorage([{ op: "replace", path: "/version", value: 32 }], { opId: "concurrent-a" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-13",
    room: "tenant-a:room-api",
    payload: [{ op: "replace", path: "/version", value: 32 }],
    meta: { op_id: "concurrent-a" },
  }),
);
const concurrentPatchB = room.patchStorage([{ op: "replace", path: "/title", value: "Still Local" }], { opId: "concurrent-b" });
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({
    t: "STORAGE_PATCH",
    id: "storage-patch-14",
    room: "tenant-a:room-api",
    payload: [{ op: "replace", path: "/title", value: "Still Local" }],
    meta: { op_id: "concurrent-b" },
  }),
);
assert.deepEqual(room.getStoragePendingMutations(), [
  {
    requestId: "storage-patch-13",
    kind: "patch",
    opId: "concurrent-a",
    operations: [{ op: "replace", path: "/version", value: 32 }],
  },
  {
    requestId: "storage-patch-14",
    kind: "patch",
    opId: "concurrent-b",
    operations: [{ op: "replace", path: "/title", value: "Still Local" }],
  },
]);
assert.deepEqual(room.getStorageSnapshot(), { title: "Still Local", version: 32 });
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-13",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "concurrent-a", document: { title: "Server Fixed", version: 32 } },
});
assert.deepEqual(await concurrentPatchA, { title: "Server Fixed", version: 32 });
assert.equal(room.getStorageStatus(), "synchronizing");
assert.deepEqual(room.getStoragePendingMutations(), [
  {
    requestId: "storage-patch-14",
    kind: "patch",
    opId: "concurrent-b",
    operations: [{ op: "replace", path: "/title", value: "Still Local" }],
  },
]);
assert.deepEqual(roomStorageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronizing",
  pendingMutations: 1,
  pendingOpIds: ["concurrent-b"],
  sequence: 2,
});
assert.deepEqual(storageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronizing",
  pendingMutations: 1,
  pendingOpIds: ["concurrent-b"],
  sequence: 2,
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Still Local", version: 32 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Still Local", version: 32 },
  source: "optimistic",
  kind: "patch",
  opId: "concurrent-b",
  operations: [{ op: "replace", path: "/title", value: "Still Local" }],
});
roomSocket.receive({
  t: "STORAGE_ACK",
  id: "storage-patch-14",
  room: "tenant-a:room-api",
  payload: { kind: "patch", op_id: "concurrent-b", document: { title: "Still Local", version: 33 } },
});
assert.deepEqual(await concurrentPatchB, { title: "Still Local", version: 33 });
assert.equal(room.getStorageStatus(), "synchronized");
assert.deepEqual(room.getStoragePendingMutations(), []);
assert.deepEqual(storageStatusUpdates.at(-1), {
  room: "tenant-a:room-api",
  status: "synchronized",
  pendingMutations: 0,
  pendingOpIds: [],
  sequence: 2,
});
assert.deepEqual(room.getStorageSnapshot(), { title: "Still Local", version: 33 });

roomSocket.receive({
  t: "STORAGE_SNAPSHOT",
  room: "tenant-a:room-api",
  meta: { seq: 3 },
  payload: { document: { title: "Snapshot", version: 34 } },
});
assert.equal(room.getStorageSequence(), 3);
assert.deepEqual(room.getStorageSnapshot(), { title: "Snapshot", version: 34 });
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: { title: "Snapshot", version: 34 },
  source: "snapshot",
  sequence: 3,
});

roomSocket.receive({
  t: "STORAGE_UPDATE",
  room: "tenant-a:room-api",
  meta: { seq: 4 },
  payload: {
    kind: "delete",
    origin_conn_id: "admin:node-a",
  },
});
assert.equal(room.getStorageSequence(), 4);
assert.equal(room.getStorageSnapshot(), undefined);
assert.deepEqual(storageEvents.at(-1), {
  room: "tenant-a:room-api",
  document: undefined,
  source: "remote",
  kind: "delete",
  sequence: 4,
  originConnId: "admin:node-a",
});

offOthers();
offMyPresence();
offEvents();
offComments();
offNotifications();
offErrors();
offStorage();
offStorageStatus();
offStorageStatusUpdates();
let sawClosedRoomReset = false;
const offRoomReset = roomClient.on("room", (state) => {
  if (state.room === "tenant-a:room-api" && state.members.length === 0 && state.others.length === 0) {
    sawClosedRoomReset = true;
  }
});
leave();
assert.equal(
  roomSocket.sent.at(-1),
  JSON.stringify({ t: "LEAVE", id: "leave-15", room: "tenant-a:room-api" }),
);
assert.equal(sawClosedRoomReset, true);
roomSocket.close();
assert.equal(roomClient.status, "closed");
assert.deepEqual(room.getOthers(), []);
offRoomReset();

FakeWebSocket.instances = [];
const historyClient = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-history",
  WebSocket: FakeWebSocket,
  autoReconnect: false,
});
const historyConnected = historyClient.connect();
await Promise.resolve();
const historySocket = FakeWebSocket.instances[0];
assert.ok(historySocket);
historySocket.open();
await historyConnected;
historySocket.receive({
  t: "HELLO",
  payload: { conn_id: "history-self", server: { node_id: "node-history" } },
});
const { room: historyRoom } = historyClient.enterRoom("tenant-a:history");
historySocket.receive({
  t: "JOINED",
  room: "tenant-a:history",
  payload: { members: ["history-self"], presence: { "history-self": {} } },
});
const historyEvents: OpenRTCStorageHistoryEvent[] = [];
const offHistoryEvents = historyRoom.subscribe("history", (event) => {
  historyEvents.push(event);
});
const historySnapshot = historyRoom.getStorage<{ title: string; count: number }>();
let historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
historySocket.receive({
  t: "STORAGE_SNAPSHOT",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { document: { title: "Draft", count: 0 } },
});
assert.deepEqual(await historySnapshot, { title: "Draft", count: 0 });
assert.equal(historyRoom.history.canUndo(), false);
assert.equal(historyRoom.history.canRedo(), false);

const historySet = historyRoom.setStorage({ title: "A", count: 1 }, { opId: "history-set-1" });
assert.equal(historyRoom.history.canUndo(), false);
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "set", op_id: "history-set-1", document: { title: "A", count: 1 } },
});
assert.deepEqual(await historySet, { title: "A", count: 1 });
assert.equal(historyRoom.history.canUndo(), true);
assert.deepEqual(historyEvents.at(-1), {
  room: "tenant-a:history",
  canUndo: true,
  canRedo: false,
  undoDepth: 1,
  redoDepth: 0,
  paused: false,
});

const historyUndo = historyRoom.history.undo<{ title: string; count: number }>({ opId: "history-undo-1" });
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
assert.deepEqual(historyMessage, {
  t: "STORAGE_PATCH",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: [
    { op: "replace", path: "/count", value: 0 },
    { op: "replace", path: "/title", value: "Draft" },
  ],
  meta: { op_id: "history-undo-1" },
});
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-undo-1", document: { title: "Draft", count: 0 } },
});
assert.deepEqual(await historyUndo, { title: "Draft", count: 0 });
assert.equal(historyRoom.history.canUndo(), false);
assert.equal(historyRoom.history.canRedo(), true);

const historyRedo = historyRoom.history.redo<{ title: string; count: number }>({ opId: "history-redo-1" });
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
assert.deepEqual(historyMessage, {
  t: "STORAGE_PATCH",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: [
    { op: "replace", path: "/count", value: 1 },
    { op: "replace", path: "/title", value: "A" },
  ],
  meta: { op_id: "history-redo-1" },
});
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-redo-1", document: { title: "A", count: 1 } },
});
assert.deepEqual(await historyRedo, { title: "A", count: 1 });
assert.equal(historyRoom.history.canUndo(), true);
assert.equal(historyRoom.history.canRedo(), false);

historyRoom.history.clear();
assert.equal(historyRoom.history.canUndo(), false);
historyRoom.history.pause();
assert.equal(historyEvents.at(-1)?.paused, true);
const pausedCount = historyRoom.patchStorage([{ op: "replace", path: "/count", value: 2 }], {
  opId: "history-pause-count",
});
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-pause-count", document: { title: "A", count: 2 } },
});
await pausedCount;
const pausedTitle = historyRoom.patchStorage([{ op: "replace", path: "/title", value: "B" }], {
  opId: "history-pause-title",
});
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-pause-title", document: { title: "B", count: 2 } },
});
await pausedTitle;
assert.equal(historyRoom.history.canUndo(), false);
historyRoom.history.resume();
assert.deepEqual(historyEvents.at(-1), {
  room: "tenant-a:history",
  canUndo: true,
  canRedo: false,
  undoDepth: 1,
  redoDepth: 0,
  paused: false,
});
const groupedUndo = historyRoom.history.undo<{ title: string; count: number }>({ opId: "history-group-undo" });
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
assert.deepEqual(historyMessage["payload"], [
  { op: "replace", path: "/count", value: 1 },
  { op: "replace", path: "/title", value: "A" },
]);
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-group-undo", document: { title: "A", count: 1 } },
});
assert.deepEqual(await groupedUndo, { title: "A", count: 1 });

historyRoom.history.clear();
const disabledPatch = historyRoom.history.disable(() =>
  historyRoom.patchStorage([{ op: "replace", path: "/count", value: 9 }], { opId: "history-disabled" }),
);
historyMessage = JSON.parse(historySocket.sent.at(-1) ?? "{}") as Record<string, unknown>;
historySocket.receive({
  t: "STORAGE_ACK",
  id: historyMessage.id,
  room: "tenant-a:history",
  payload: { kind: "patch", op_id: "history-disabled", document: { title: "A", count: 9 } },
});
assert.deepEqual(await disabledPatch, { title: "A", count: 9 });
assert.equal(historyRoom.history.canUndo(), false);
offHistoryEvents();
historySocket.close();

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

const devConfig = {
  publicKey: OPENRTC_DEV_PUBLIC_KEY,
  tokenURL: "/dev/token",
  jwksURL: "http://127.0.0.1:3000/jwks",
  wsURL: "ws://127.0.0.1:8080/ws",
  yjsURL: "ws://127.0.0.1:8080/yjs",
  adminURL: "http://127.0.0.1:8090",
  adminProxyURL: "/admin",
  runtimeURL: "http://127.0.0.1:8080",
  runtimeProxyURL: "/runtime",
  statusURL: "http://127.0.0.1:3000/dev/status",
  connectionsURL: "http://127.0.0.1:3000/dev/connections?room=demo:room-1",
  socketsURL: "http://127.0.0.1:3000/dev/sockets",
  storageURL: "http://127.0.0.1:3000/dev/storage?room=demo:room-1",
  yjsInspectionURL: "http://127.0.0.1:3000/dev/yjs?room=demo:room-1",
  eventsURL: "http://127.0.0.1:3000/dev/events?room=demo:room-1",
  crashRuntimeURL: "http://127.0.0.1:3000/dev/crash/runtime",
  crashAdminURL: "http://127.0.0.1:3000/dev/crash/admin",
  seedRooms: ["demo:room-1", "demo:canvas-1"],
};

const devConfigCalls: string[] = [];
const fetchedDevConfig = await fetchOpenRTCDevConfig({
  baseURL: "http://127.0.0.1:3000",
  fetch: async (input) => {
    devConfigCalls.push(input);
    return fakeResponse(200, JSON.stringify(devConfig));
  },
});
assert.equal(new URL(devConfigCalls[0] ?? "").pathname, "/dev/config");
assert.equal(fetchedDevConfig.wsURL, "ws://127.0.0.1:8080/ws");
assert.equal(fetchedDevConfig.seedRooms[1], "demo:canvas-1");

const devConfigAliasCalls: string[] = [];
await fetchOpenRTCDevConfig({
  baseURL: "http://127.0.0.1:3000",
  configURL: "/config",
  fetch: async (input) => {
    devConfigAliasCalls.push(input);
    return fakeResponse(200, JSON.stringify(devConfig));
  },
});
assert.equal(new URL(devConfigAliasCalls[0] ?? "").pathname, "/config");

const devTokenCalls: string[] = [];
const devToken = await fetchOpenRTCDevToken({
  baseURL: "http://127.0.0.1:3000",
  username: "ada",
  tenant: "acme",
  room: "acme:room-1",
  groups: ["editors", "reviewers"],
  fetch: async (input) => {
    devTokenCalls.push(input);
    return fakeResponse(
      200,
      JSON.stringify({
        token: "dev-token-1",
        kind: "client",
        username: "ada",
        tenant: "acme",
        groups: ["editors", "reviewers"],
        expiresAt: "2026-07-04T00:00:00Z",
        room: "acme:room-1",
        config: devConfig,
      }),
    );
  },
});
const devTokenURL = new URL(devTokenCalls[0] ?? "");
assert.equal(devTokenURL.pathname, "/dev/token");
assert.equal(devTokenURL.searchParams.get("pubkey"), OPENRTC_DEV_PUBLIC_KEY);
assert.equal(devTokenURL.searchParams.get("username"), "ada");
assert.equal(devTokenURL.searchParams.get("tenant"), "acme");
assert.equal(devTokenURL.searchParams.get("room"), "acme:room-1");
assert.equal(devTokenURL.searchParams.get("groups"), "editors,reviewers");
assert.equal(devToken.token, "dev-token-1");
assert.equal(devToken.room, "acme:room-1");
assert.deepEqual(devToken.config.seedRooms, ["demo:room-1", "demo:canvas-1"]);
assert.equal(devToken.config.statusURL, "http://127.0.0.1:3000/dev/status");
assert.equal(devToken.config.connectionsURL, "http://127.0.0.1:3000/dev/connections?room=demo:room-1");
assert.equal(devToken.config.socketsURL, "http://127.0.0.1:3000/dev/sockets");
assert.equal(devToken.config.storageURL, "http://127.0.0.1:3000/dev/storage?room=demo:room-1");
assert.equal(devToken.config.yjsInspectionURL, "http://127.0.0.1:3000/dev/yjs?room=demo:room-1");
assert.equal(devToken.config.eventsURL, "http://127.0.0.1:3000/dev/events?room=demo:room-1");
assert.equal(devToken.config.crashRuntimeURL, "http://127.0.0.1:3000/dev/crash/runtime");
assert.equal(devToken.config.crashAdminURL, "http://127.0.0.1:3000/dev/crash/admin");

const devToolCalls: Array<{ input: string; init?: { method?: string; headers?: Record<string, string>; body?: string } }> =
  [];
let devRuntimeSocketConnId = "conn-dev-1";
let devRuntimeSocketRooms = ["demo:canvas-1"];
let devRuntimeCrashSocketToClose: FakeWebSocket | undefined;
const socketCountBeforeDevClient = FakeWebSocket.instances.length;
const devClient = await createOpenRTCDevClient({
  baseURL: "http://127.0.0.1:3000",
  room: "demo:canvas-1",
  WebSocket: FakeWebSocket,
  reconnect: { initialDelayMs: 1, maxDelayMs: 1, jitterRatio: 0 },
  fetch: async (input, init) => {
    const url = new URL(input);
    if (url.pathname !== "/dev/token") {
      devToolCalls.push(init ? { input, init } : { input });
      if (url.pathname === "/dev/status") {
        return fakeResponse(
          200,
          JSON.stringify({
            status: "ok",
            storage_backend: "memory",
            redis: { healthy: true },
            runtime: {
              running: true,
              url: "http://127.0.0.1:8080",
              healthz: "http://127.0.0.1:8080/healthz",
              readyz: "http://127.0.0.1:8080/readyz",
              generation: 2,
            },
            admin: {
              running: true,
              url: "http://127.0.0.1:8090",
              healthz: "http://127.0.0.1:8090/healthz",
              readyz: "http://127.0.0.1:8090/readyz",
              generation: 1,
            },
            seed_rooms: [{ room: "demo:room-1", exists: true, storage_found: true }],
            endpoints: { sockets: "http://127.0.0.1:3000/dev/sockets" },
          }),
        );
      }
      if (url.pathname === "/dev/connections") {
        return fakeResponse(
          200,
          JSON.stringify({
            room: url.searchParams.get("room"),
            connections: [
              {
                type: "json",
                connection_id: "conn-dev-1",
                id: "ada",
                tenant: "demo",
                presence: { cursor: { x: 1, y: 2 } },
              },
            ],
          }),
        );
      }
      if (url.pathname === "/dev/sockets") {
        return fakeResponse(
          200,
          JSON.stringify({
            node_id: "openrtc-dev-runtime",
            connections: [
              {
                connection_id: devRuntimeSocketConnId,
                subject: "ada",
                tenant: "demo",
                rooms: devRuntimeSocketRooms,
              },
            ],
            yjs_connections: [{ connection_id: "yjs-dev-1", subject: "ada", tenant: "demo", room: "demo:canvas-1" }],
            active_sockets: 2,
            active_room_count: 1,
          }),
        );
      }
      if (url.pathname === "/dev/storage") {
        return fakeResponse(
          200,
          JSON.stringify({
            room: url.searchParams.get("room"),
            durable: { found: true, document: { title: "Draft" } },
            runtime: {
              node_id: "openrtc-dev-runtime",
              room: url.searchParams.get("room"),
              found: true,
              store_backed: true,
              document: { title: "Runtime" },
            },
          }),
        );
      }
      if (url.pathname === "/dev/yjs") {
        return fakeResponse(
          200,
          JSON.stringify({
            room: url.searchParams.get("room"),
            durable: {
              found: true,
              snapshot_found: true,
              snapshot_bytes: 12,
              snapshot_hash: "fnv1a64:abc",
              snapshot_checkpoint: 7,
              update_count: 2,
              update_bytes: 30,
              update_sequences: [8, 9],
              update_kinds: ["update", "subdoc-update"],
            },
          }),
        );
      }
      if (url.pathname === "/dev/events") {
        return fakeResponse(
          200,
          JSON.stringify({
            room: url.searchParams.get("room"),
            after_seq: Number(url.searchParams.get("after_seq")),
            limit: Number(url.searchParams.get("limit")),
            events: [{ room: "demo:canvas-1", event: "demo.event", payload: { ok: true }, seq: 8, origin_node: "node-a" }],
          }),
        );
      }
      if (url.pathname === "/dev/crash/runtime") {
        assert.equal(init?.method, "POST");
        const crashSocket = devRuntimeCrashSocketToClose;
        devRuntimeCrashSocketToClose = undefined;
        crashSocket?.close();
        return fakeResponse(
          200,
          JSON.stringify({
            status: "restarted",
            service: "runtime",
            service_status: {
              running: true,
              url: "http://127.0.0.1:8080",
              healthz: "http://127.0.0.1:8080/healthz",
              readyz: "http://127.0.0.1:8080/readyz",
              generation: 3,
            },
          }),
        );
      }
      if (url.pathname === "/dev/crash/admin") {
        assert.equal(init?.method, "POST");
        return fakeResponse(
          200,
          JSON.stringify({
            status: "restarted",
            service: "admin",
            service_status: {
              running: true,
              url: "http://127.0.0.1:8090",
              healthz: "http://127.0.0.1:8090/healthz",
              readyz: "http://127.0.0.1:8090/readyz",
              generation: 2,
            },
          }),
        );
      }
      throw new Error(`unexpected dev tool URL: ${input}`);
    }
    assert.equal(url.searchParams.get("room"), "demo:canvas-1");
    return fakeResponse(
      200,
      JSON.stringify({
        token: "dev-token-2",
        kind: "client",
        username: "anon-local",
        tenant: "demo",
        groups: [],
        expiresAt: "2026-07-04T00:00:00Z",
        room: "demo:canvas-1",
        config: devConfig,
      }),
    );
  },
});
assert.equal(devClient.room, "demo:canvas-1");
assert.equal(devClient.config.wsURL, "ws://127.0.0.1:8080/ws");
assert.equal(devClient.tools.room, "demo:canvas-1");
assert.equal(devClient.tools.config, devClient.config);
const devStatus = await devClient.tools.fetchStatus();
assert.equal(devStatus.status, "ok");
assert.equal(devStatus.storage_backend, "memory");
assert.equal(devStatus.redis.healthy, true);
assert.equal(devStatus.runtime.generation, 2);
assert.equal(devStatus.seed_rooms[0]?.storage_found, true);
assert.equal(devStatus.endpoints.sockets, "http://127.0.0.1:3000/dev/sockets");
const devConnections = await devClient.tools.fetchConnections();
assert.equal(devConnections.room, "demo:canvas-1");
assert.equal(devConnections.connections[0]?.connection_id, "conn-dev-1");
assert.deepEqual(devConnections.connections[0]?.presence, { cursor: { x: 1, y: 2 } });
const devSockets = await devClient.tools.fetchSockets({ room: "demo:canvas-1" });
assert.equal(devSockets.node_id, "openrtc-dev-runtime");
assert.equal(devSockets.active_sockets, 2);
assert.equal(devSockets.connections[0]?.rooms[0], "demo:canvas-1");
assert.equal(devSockets.yjs_connections[0]?.room, "demo:canvas-1");
const devStorage = await devClient.tools.fetchStorage();
assert.equal(devStorage.room, "demo:canvas-1");
assert.deepEqual(devStorage.durable.document, { title: "Draft" });
assert.equal(devStorage.runtime?.store_backed, true);
const devYJS = await devClient.tools.fetchYJS({ room: "demo:canvas-yjs" });
assert.equal(devYJS.room, "demo:canvas-yjs");
assert.equal(devYJS.durable.snapshot_hash, "fnv1a64:abc");
assert.deepEqual(devYJS.durable.update_kinds, ["update", "subdoc-update"]);
const devEvents = await devClient.tools.fetchEvents({ afterSequence: 7, limit: 3 });
assert.equal(devEvents.room, "demo:canvas-1");
assert.equal(devEvents.after_seq, 7);
assert.equal(devEvents.limit, 3);
assert.equal(devEvents.events[0]?.event, "demo.event");
assert.equal(devEvents.events[0]?.seq, 8);
const runtimeRestart = await devClient.tools.restartRuntime();
assert.equal(runtimeRestart.service, "runtime");
assert.equal(runtimeRestart.service_status.generation, 3);
const adminRestart = await devClient.tools.restartAdmin();
assert.equal(adminRestart.service, "admin");
assert.equal(adminRestart.service_status.generation, 2);
assert.deepEqual(
  devToolCalls.map((call) => [new URL(call.input).pathname, call.init?.method ?? "GET"]),
  [
    ["/dev/status", "GET"],
    ["/dev/connections", "GET"],
    ["/dev/sockets", "GET"],
    ["/dev/storage", "GET"],
    ["/dev/yjs", "GET"],
    ["/dev/events", "GET"],
    ["/dev/crash/runtime", "POST"],
    ["/dev/crash/admin", "POST"],
  ],
);
const devProbe = await devClient.tools.probe({ afterSequence: 8, limit: 2, restart: "runtime" });
assert.equal(devProbe.ok, true);
assert.equal(devProbe.room, "demo:canvas-1");
assert.deepEqual(
  devProbe.checks.map((check) => [check.name, check.ok]),
  [
    ["config", true],
    ["seed-room", true],
    ["status", true],
    ["connections", true],
    ["sockets", true],
    ["storage", true],
    ["yjs", true],
    ["events", true],
    ["restart-runtime", true],
  ],
);
assert.equal(devProbe.snapshots.status?.storage_backend, "memory");
assert.equal(devProbe.snapshots.events?.after_seq, 8);
assert.equal(devProbe.snapshots.events?.limit, 2);
assert.equal(devProbe.snapshots.runtimeRestart?.service_status.generation, 3);
assert.deepEqual(
  devToolCalls.slice(8).map((call) => [new URL(call.input).pathname, call.init?.method ?? "GET"]),
  [
    ["/dev/status", "GET"],
    ["/dev/connections", "GET"],
    ["/dev/sockets", "GET"],
    ["/dev/storage", "GET"],
    ["/dev/yjs", "GET"],
    ["/dev/events", "GET"],
    ["/dev/crash/runtime", "POST"],
  ],
);
const standaloneProbe = await runOpenRTCDevProbe(devClient.tools, {
  room: "demo:other",
  expectSeedRoom: false,
  expectSeedStorage: false,
});
assert.equal(standaloneProbe.ok, true);
assert.equal(standaloneProbe.room, "demo:other");
assert.equal(standaloneProbe.checks.find((check) => check.name === "seed-room")?.ok, true);
const legacyDevTools = createOpenRTCDevTools(
  {
    publicKey: devConfig.publicKey,
    tokenURL: devConfig.tokenURL,
    jwksURL: devConfig.jwksURL,
    wsURL: devConfig.wsURL,
    yjsURL: devConfig.yjsURL,
    adminURL: devConfig.adminURL,
    adminProxyURL: devConfig.adminProxyURL,
    runtimeURL: devConfig.runtimeURL,
    runtimeProxyURL: devConfig.runtimeProxyURL,
    seedRooms: devConfig.seedRooms,
  },
  {
    baseURL: "http://127.0.0.1:3000",
    room: "demo:legacy",
    fetch: async (input, init) =>
      fakeResponse(200, JSON.stringify({ url: input, method: init?.method ?? "GET" })),
  },
);
assert.deepEqual(await legacyDevTools.fetchEvents({ limit: 5 }), {
  url: "http://127.0.0.1:3000/dev/events?room=demo%3Alegacy&limit=5",
  method: "GET",
});
assert.deepEqual(await legacyDevTools.restartRuntime(), {
  url: "http://127.0.0.1:3000/dev/crash/runtime",
  method: "POST",
});
const degradedDevTools = createOpenRTCDevTools(devConfig, {
  room: "demo:room-1",
  fetch: async (input) => {
    const url = new URL(input);
    if (url.pathname === "/dev/status") {
      return fakeResponse(
        200,
        JSON.stringify({
          status: "degraded",
          storage_backend: "memory",
          redis: { healthy: false, error: "redis ping failed" },
          runtime: {
            running: false,
            url: "http://127.0.0.1:8080",
            healthz: "http://127.0.0.1:8080/healthz",
            readyz: "http://127.0.0.1:8080/readyz",
            generation: 4,
          },
          admin: {
            running: true,
            url: "http://127.0.0.1:8090",
            healthz: "http://127.0.0.1:8090/healthz",
            readyz: "http://127.0.0.1:8090/readyz",
            generation: 2,
          },
          seed_rooms: [{ room: "demo:room-1", exists: true, storage_found: false }],
          endpoints: { sockets: "http://127.0.0.1:3000/dev/sockets" },
        }),
      );
    }
    if (url.pathname === "/dev/storage") {
      return fakeResponse(
        200,
        JSON.stringify({
          room: url.searchParams.get("room"),
          durable: { found: false },
          runtime: { node_id: "node-a", room: url.searchParams.get("room"), found: false, store_backed: true },
        }),
      );
    }
    if (url.pathname === "/dev/connections") {
      return fakeResponse(200, JSON.stringify({ room: url.searchParams.get("room"), connections: [] }));
    }
    if (url.pathname === "/dev/sockets") {
      return fakeResponse(
        200,
        JSON.stringify({
          node_id: "node-a",
          connections: [],
          yjs_connections: [],
          active_sockets: 0,
          active_room_count: 0,
        }),
      );
    }
    if (url.pathname === "/dev/yjs") {
      return fakeResponse(
        200,
        JSON.stringify({
          room: url.searchParams.get("room"),
          durable: {
            found: false,
            snapshot_found: false,
            snapshot_bytes: 0,
            snapshot_checkpoint: 0,
            update_count: 0,
            update_bytes: 0,
          },
        }),
      );
    }
    if (url.pathname === "/dev/events") {
      return fakeResponse(
        200,
        JSON.stringify({ room: url.searchParams.get("room"), after_seq: 0, limit: 20, events: [] }),
      );
    }
    throw new Error(`unexpected degraded dev tool URL: ${input}`);
  },
});
const degradedProbe = await degradedDevTools.probe();
assert.equal(degradedProbe.ok, false);
assert.equal(degradedProbe.checks.find((check) => check.name === "status")?.ok, false);
assert.equal(degradedProbe.checks.find((check) => check.name === "storage")?.ok, false);
assert.equal(degradedProbe.checks.find((check) => check.name === "events")?.ok, true);
const realtimeProbePromise = devClient.tools.probe({ realtime: true });
await waitFor(() => FakeWebSocket.instances.length > socketCountBeforeDevClient, "expected realtime probe socket");
const devSocket = FakeWebSocket.instances[socketCountBeforeDevClient];
assert.ok(devSocket);
assert.equal(devSocket.url, "ws://127.0.0.1:8080/ws?token=dev-token-2");
devSocket.open();
devSocket.receive({
  t: "HELLO",
  payload: { conn_id: "dev-probe-conn", server: { name: "openrtc", node_id: "openrtc-dev-runtime" } },
});
await waitFor(() => devSocket.sent.length >= 1, "expected realtime probe join");
const realtimeJoin = latestSentMessage(devSocket, "JOIN");
assert.equal(realtimeJoin.t, "JOIN");
assert.equal(realtimeJoin.room, "demo:canvas-1");
devSocket.receive({
  t: "JOINED",
  id: realtimeJoin.id,
  room: "demo:canvas-1",
  payload: { members: ["dev-probe-conn"], presence: {} },
});
await waitFor(() => devSocket.sent.some((item) => (JSON.parse(item) as Record<string, unknown>).t === "STORAGE_GET"), "expected realtime storage get");
const realtimeStorageGet = latestSentMessage(devSocket, "STORAGE_GET");
assert.equal(realtimeStorageGet.t, "STORAGE_GET");
devSocket.receive({
  t: "STORAGE_SNAPSHOT",
  id: realtimeStorageGet.id,
  room: "demo:canvas-1",
  meta: { seq: 1 },
  payload: {
    document: {
      liveblocksType: "LiveObject",
      data: { title: "OpenRTC dev room" },
    },
  },
});
await waitFor(() => devSocket.sent.some((item) => (JSON.parse(item) as Record<string, unknown>).t === "STORAGE_PATCH"), "expected realtime storage patch");
const realtimePatch = latestSentMessage(devSocket, "STORAGE_PATCH");
assert.equal(realtimePatch.t, "STORAGE_PATCH");
assert.deepEqual(realtimePatch.meta, { op_id: "dev-probe-storage-patch", expected_seq: 1 });
const realtimePatchPayload = realtimePatch.payload as Array<{ op: string; path: string; value?: { checked_at?: string } }>;
assert.equal(realtimePatchPayload[0]?.op, "add");
assert.equal(realtimePatchPayload[0]?.path, "/data/__openrtc_probe");
assert.equal(typeof realtimePatchPayload[0]?.value?.checked_at, "string");
devSocket.receive({
  t: "STORAGE_ACK",
  id: realtimePatch.id,
  room: "demo:canvas-1",
  meta: { seq: 2 },
  payload: {
    kind: "patch",
    op_id: "dev-probe-storage-patch",
    document: {
      liveblocksType: "LiveObject",
      data: { title: "OpenRTC dev room", __openrtc_probe: { checked_at: "2026-07-04T00:00:00.000Z" } },
    },
  },
});
await waitFor(
  () =>
    devSocket.sent.filter((item) => (JSON.parse(item) as Record<string, unknown>).t === "STORAGE_PATCH").length >= 2,
  "expected realtime storage patch retry",
);
const realtimeRetryPatch = latestSentMessage(devSocket, "STORAGE_PATCH");
assert.equal(realtimeRetryPatch.t, "STORAGE_PATCH");
assert.deepEqual(realtimeRetryPatch.meta, { op_id: "dev-probe-storage-patch", expected_seq: 1 });
assert.deepEqual(realtimeRetryPatch.payload, realtimePatch.payload);
devSocket.receive({
  t: "STORAGE_ACK",
  id: realtimeRetryPatch.id,
  room: "demo:canvas-1",
  meta: { seq: 2 },
  payload: {
    kind: "patch",
    op_id: "dev-probe-storage-patch",
    document: {
      liveblocksType: "LiveObject",
      data: { title: "OpenRTC dev room", __openrtc_probe: { checked_at: "2026-07-04T00:00:00.000Z" } },
    },
  },
});
const realtimeProbe = await realtimeProbePromise;
assert.equal(realtimeProbe.ok, true);
assert.deepEqual(realtimeProbe.checks.map((check) => [check.name, check.ok]).at(-1), ["realtime", true]);
assert.equal(realtimeProbe.snapshots.realtime?.connection_id, "dev-probe-conn");
assert.equal(realtimeProbe.snapshots.realtime?.snapshot_sequence, 1);
assert.equal(realtimeProbe.snapshots.realtime?.ack_sequence, 2);
assert.equal(realtimeProbe.snapshots.realtime?.retry_ack_sequence, 2);
assert.equal(realtimeProbe.snapshots.realtime?.idempotent_retry_acked, true);
assert.equal(realtimeProbe.snapshots.realtime?.probe_path, "/data/__openrtc_probe");

devClient.client.close();
const socketCountBeforeReconnectProbe = FakeWebSocket.instances.length;
const reconnectProbePromise = devClient.tools.probe({ reconnect: true });
await waitFor(
  () => FakeWebSocket.instances.length > socketCountBeforeReconnectProbe,
  "expected reconnect probe initial socket",
);
const reconnectProbeSocketA = FakeWebSocket.instances[socketCountBeforeReconnectProbe];
assert.ok(reconnectProbeSocketA);
devRuntimeCrashSocketToClose = reconnectProbeSocketA;
reconnectProbeSocketA.open();
reconnectProbeSocketA.receive({
  t: "HELLO",
  payload: { conn_id: "dev-reconnect-a", server: { name: "openrtc", node_id: "openrtc-dev-runtime" } },
});
await waitFor(
  () => reconnectProbeSocketA.sent.some((item) => (JSON.parse(item) as Record<string, unknown>).t === "JOIN"),
  "expected reconnect probe initial join",
);
const reconnectJoinA = latestSentMessage(reconnectProbeSocketA, "JOIN");
assert.equal(reconnectJoinA.room, "demo:canvas-1");
devRuntimeSocketConnId = "dev-reconnect-a";
devRuntimeSocketRooms = ["demo:canvas-1"];
reconnectProbeSocketA.receive({
  t: "JOINED",
  id: reconnectJoinA.id,
  room: "demo:canvas-1",
  payload: { members: ["dev-reconnect-a"], presence: {} },
});
await waitFor(
  () => FakeWebSocket.instances.length > socketCountBeforeReconnectProbe + 1,
  "expected reconnect probe replacement socket",
  1500,
);
const reconnectProbeSocketB = FakeWebSocket.instances[socketCountBeforeReconnectProbe + 1];
assert.ok(reconnectProbeSocketB);
reconnectProbeSocketB.open();
reconnectProbeSocketB.receive({
  t: "HELLO",
  payload: { conn_id: "dev-reconnect-b", server: { name: "openrtc", node_id: "openrtc-dev-runtime" } },
});
await waitFor(
  () => reconnectProbeSocketB.sent.some((item) => (JSON.parse(item) as Record<string, unknown>).t === "JOIN"),
  "expected reconnect probe rejoin",
);
const reconnectJoinB = latestSentMessage(reconnectProbeSocketB, "JOIN");
assert.equal(reconnectJoinB.room, "demo:canvas-1");
devRuntimeSocketConnId = "dev-reconnect-b";
devRuntimeSocketRooms = ["demo:canvas-1"];
reconnectProbeSocketB.receive({
  t: "JOINED",
  id: reconnectJoinB.id,
  room: "demo:canvas-1",
  payload: { members: ["dev-reconnect-b"], presence: {} },
});
const reconnectProbe = await reconnectProbePromise;
assert.equal(reconnectProbe.ok, true);
assert.deepEqual(reconnectProbe.checks.map((check) => [check.name, check.ok]).at(-1), [
  "runtime-reconnect",
  true,
]);
assert.equal(reconnectProbe.snapshots.runtimeReconnect?.initial_connection_id, "dev-reconnect-a");
assert.equal(reconnectProbe.snapshots.runtimeReconnect?.reconnect_connection_id, "dev-reconnect-b");
assert.equal(reconnectProbe.snapshots.runtimeReconnect?.close_observed, true);
assert.equal(reconnectProbe.snapshots.runtimeReconnect?.rejoined, true);
assert.equal(reconnectProbe.snapshots.runtimeReconnect?.connection_id_changed, true);
assert.deepEqual(
  reconnectProbe.snapshots.runtimeReconnect?.status_history,
  ["connecting", "open", "reconnecting", "open"],
);
devClient.client.close();

const devAdmin = await createOpenRTCDevAdminClient({
  baseURL: "http://127.0.0.1:3000",
  scope: "rooms:* storage:*",
  fetch: async (input, init) => {
    if (input.includes("/dev/token")) {
      const url = new URL(input);
      assert.equal(url.searchParams.get("kind"), "admin");
      assert.equal(url.searchParams.get("scope"), "rooms:* storage:*");
      return fakeResponse(
        200,
        JSON.stringify({
          token: "dev-admin-token",
          kind: "admin",
          username: "anon-admin",
          tenant: "demo",
          groups: [],
          expiresAt: "2026-07-04T00:00:00Z",
          config: devConfig,
        }),
      );
    }
    assert.equal(input, "http://127.0.0.1:8090/v1/stats");
    assert.equal(init?.headers?.Authorization, "Bearer dev-admin-token");
    return fakeResponse(200, JSON.stringify({ activeConnections: 2 }));
  },
});
assert.equal(devAdmin.auth.kind, "admin");
assert.equal(devAdmin.config.adminURL, "http://127.0.0.1:8090");
assert.deepEqual(await devAdmin.admin.stats(), { activeConnections: 2 });

const devAdminProxy = await createOpenRTCDevAdminClient({
  baseURL: "http://127.0.0.1:3000",
  useProxy: true,
  fetch: async (input, init) => {
    if (input.includes("/dev/token")) {
      return fakeResponse(
        200,
        JSON.stringify({
          token: "dev-admin-proxy-token",
          kind: "admin",
          username: "anon-admin",
          tenant: "demo",
          groups: [],
          expiresAt: "2026-07-04T00:00:00Z",
          config: devConfig,
        }),
      );
    }
    assert.equal(input, "/admin/v1/stats");
    assert.equal(init?.headers?.Authorization, "Bearer dev-admin-proxy-token");
    return fakeResponse(200, JSON.stringify({ activeConnections: 3 }));
  },
});
assert.deepEqual(await devAdminProxy.admin.stats(), { activeConnections: 3 });

await assert.rejects(
  async () => {
    await fetchOpenRTCDevToken({
      fetch: async () => fakeResponse(403, JSON.stringify({ code: "ROOM_FORBIDDEN", message: "no access" })),
    });
  },
  (error) => error instanceof OpenRTCDevError && error.status === 403 && error.message === "ROOM_FORBIDDEN: no access",
);

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
    if (input.includes("/threads?")) {
      return fakeResponse(
        200,
        JSON.stringify({
          data: [{ type: "thread", id: "thread-1", roomId: "tenant-a:room-1", comments: [], resolved: false, unread: false }],
          next_cursor: "2",
        }),
      );
    }
    if (input.includes("/threads/thread-1/read-state?")) {
      return fakeResponse(
        200,
        JSON.stringify({
          roomId: "tenant-a:room-1",
          threadId: "thread-1",
          userId: "user-1",
          readAt: "2026-07-04T00:05:00.000Z",
          threadUpdatedAt: "2026-07-04T00:04:00.000Z",
          unread: false,
        }),
      );
    }
    if (input.endsWith("/threads/thread-1/read") && init?.method === "POST") {
      return fakeResponse(
        200,
        JSON.stringify({
          roomId: "tenant-a:room-1",
          threadId: "thread-1",
          userId: "user-1",
          readAt: "2026-07-04T00:05:00.000Z",
          threadUpdatedAt: "2026-07-04T00:04:00.000Z",
          unread: false,
        }),
      );
    }
    if (input.endsWith("/threads/thread-1/unread") && init?.method === "POST") {
      return fakeResponse(
        200,
        JSON.stringify({
          roomId: "tenant-a:room-1",
          threadId: "thread-1",
          userId: "user-1",
          threadUpdatedAt: "2026-07-04T00:04:00.000Z",
          unread: true,
        }),
      );
    }
    if (input.endsWith("/threads/thread-1") && (!init?.method || init.method === "GET")) {
      return fakeResponse(
        200,
        JSON.stringify({ type: "thread", id: "thread-1", roomId: "tenant-a:room-1", comments: [], resolved: false }),
      );
    }
    if (input.endsWith("/threads/thread-1") && init?.method === "PATCH") {
      return fakeResponse(
        200,
        JSON.stringify({
          type: "thread",
          id: "thread-1",
          roomId: "tenant-a:room-1",
          comments: [],
          resolved: JSON.parse(init.body ?? "{}").resolved ?? false,
          metadata: JSON.parse(init.body ?? "{}").metadata ?? {},
        }),
      );
    }
    if (input.endsWith("/threads/thread-1") && init?.method === "DELETE") {
      return fakeResponse(204, "");
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
      const body = JSON.parse(init.body ?? "{}");
      return fakeResponse(
        200,
        JSON.stringify({
          roomId: "tenant-a:room-1",
          userId: "user-1",
          threads: body.threads ?? "all",
          textMentions: body.textMentions ?? "mine",
        }),
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
  query: roomQuery({ "metadata.title": "Room", "metadata.public": true }),
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

const queriedThreads = await adminClient.listThreads("tenant-a:room-1", {
  limit: 20,
  cursor: "1",
  query: threadQuery({ resolved: false, unread: false, "metadata.status": "open" }),
  userId: "user-1",
});
assert.equal(queriedThreads.data[0]?.id, "thread-1");
assert.equal(queriedThreads.data[0]?.unread, false);
assert.equal(queriedThreads.next_cursor, "2");
assert.equal(
  adminCalls.at(-1)?.input,
  "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads?limit=20&cursor=1&query=resolved%3Afalse+unread%3Afalse+metadata.status%3A%22open%22&userId=user-1",
);

const fetchedThread = await adminClient.getThread("tenant-a:room-1", "thread-1");
assert.equal(fetchedThread.id, "thread-1");
assert.equal(adminCalls.at(-1)?.input, "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1");

const lifecycleThread = await adminClient.updateThread("tenant-a:room-1", "thread-1", {
  metadata: { status: "resolved" },
  resolved: true,
});
assert.equal(lifecycleThread.resolved, true);
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  metadata: { status: "resolved" },
  resolved: true,
});

await adminClient.editThreadMetadata("tenant-a:room-1", "thread-1", { status: "open" });
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { metadata: { status: "open" } });

await adminClient.markThreadResolved("tenant-a:room-1", "thread-1");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { resolved: true });

await adminClient.markThreadUnresolved("tenant-a:room-1", "thread-1");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { resolved: false });

const threadReadState = await adminClient.getThreadReadState("tenant-a:room-1", "thread-1", "user-1");
assert.equal(threadReadState.unread, false);
assert.equal(
  adminCalls.at(-1)?.input,
  "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/read-state?userId=user-1",
);

const markedThreadRead = await adminClient.markThreadRead("tenant-a:room-1", "thread-1", "user-1");
assert.equal(markedThreadRead.unread, false);
assert.equal(adminCalls.at(-1)?.input, "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/read");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { userId: "user-1" });

const markedThreadUnread = await adminClient.markThreadUnread("tenant-a:room-1", "thread-1", "user-1");
assert.equal(markedThreadUnread.unread, true);
assert.equal(adminCalls.at(-1)?.input, "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1/unread");
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { userId: "user-1" });

await adminClient.deleteThread("tenant-a:room-1", "thread-1");
assert.equal(adminCalls.at(-1)?.input, "http://localhost:8090/admin/v1/rooms/tenant-a%3Aroom-1/threads/thread-1");
assert.equal(adminCalls.at(-1)?.init?.method, "DELETE");

const updatedThread = await adminClient.addComment("tenant-a:room-1", "thread-1", {
  userId: "user-2",
  body: { type: "text", text: "reply" },
  reactions: [{ emoji: "+1", userId: "user-1" }],
});
assert.deepEqual(updatedThread.comments, [{ id: "comment-2" }]);

const patchedThread = await adminClient.updateComment("tenant-a:room-1", "thread-1", "comment-1", {
  body: { type: "text", text: "edited" },
  metadata: { status: "resolved" },
  mentions: addCommentMention([], "user-2"),
  reactions: addCommentReaction([], { emoji: "+1", userId: "user-2" }),
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
assert.deepEqual(await adminClient.subscribeRoomThreads("tenant-a:room-1", "user-1"), {
  roomId: "tenant-a:room-1",
  userId: "user-1",
  threads: "all",
  textMentions: "mine",
});
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { threads: "all", textMentions: "mine" });
assert.deepEqual(await adminClient.subscribeRoomRepliesAndMentions("tenant-a:room-1", "user-1"), {
  roomId: "tenant-a:room-1",
  userId: "user-1",
  threads: "replies_and_mentions",
  textMentions: "mine",
});
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), {
  threads: "replies_and_mentions",
  textMentions: "mine",
});
assert.deepEqual(await adminClient.muteRoomThreads("tenant-a:room-1", "user-1"), {
  roomId: "tenant-a:room-1",
  userId: "user-1",
  threads: "none",
  textMentions: "none",
});
assert.deepEqual(JSON.parse(adminCalls.at(-1)?.init?.body ?? "{}"), { threads: "none", textMentions: "none" });
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
