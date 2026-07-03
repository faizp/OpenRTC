export type ConnectionStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed" | "error";

export type JSONValue =
  | null
  | boolean
  | number
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };

export type PresenceState = Record<string, unknown>;

export interface OpenRTCCursor {
  x: number;
  y: number;
  mode?: "pointer" | "text" | "draw" | "comment" | "grab" | string;
  label?: string;
}

export interface OpenRTCUserInfo {
  id?: string;
  name?: string;
  color?: string;
  avatar?: string;
  [key: string]: unknown;
}

export interface OpenRTCCursorOptions {
  user?: OpenRTCUserInfo;
  color?: string;
  mode?: OpenRTCCursor["mode"];
  metadata?: PresenceState;
}

export interface OpenRTCCursorPeer extends PresencePeer {
  cursor: OpenRTCCursor;
  user?: OpenRTCUserInfo;
  color?: string;
  mode?: OpenRTCCursor["mode"];
}

export interface OpenRTCWebSocket {
  readonly readyState: number;
  send(data: string | ArrayBufferLike | ArrayBufferView): void;
  close(code?: number, reason?: string): void;
  addEventListener(type: string, listener: (event: unknown) => void): void;
  removeEventListener(type: string, listener: (event: unknown) => void): void;
}

export type OpenRTCWebSocketConstructor = new (url: string) => OpenRTCWebSocket;

export interface OpenRTCClientOptions {
  url: string;
  token: string | (() => string | Promise<string>);
  WebSocket?: OpenRTCWebSocketConstructor;
  autoReconnect?: boolean;
  lostConnectionTimeout?: number;
  backgroundKeepAliveTimeout?: number;
  reconnect?: OpenRTCReconnectOptions;
}

export interface OpenRTCReconnectOptions {
  initialDelayMs?: number;
  maxDelayMs?: number;
  maxAttempts?: number;
  jitterRatio?: number;
}

export interface OpenRTCFetchResponse {
  readonly ok: boolean;
  readonly status: number;
  readonly statusText: string;
  text(): Promise<string>;
}

export type OpenRTCFetch = (
  input: string,
  init?: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
  },
) => Promise<OpenRTCFetchResponse>;

export interface OpenRTCAdminClientOptions {
  url: string;
  token: string | (() => string | Promise<string>);
  fetch?: OpenRTCFetch;
}

export interface JoinOptions {
  limit?: number;
  cursor?: string;
}

export interface EnterRoomOptions extends JoinOptions {
  initialPresence?: PresenceState;
}

export interface PresencePeer {
  connId: string;
  state: PresenceState;
}

export interface OpenRTCRoomPresence {
  room: string;
  members: string[];
  others: PresencePeer[];
  self?: PresencePeer;
  nextCursor?: string;
}

export interface OpenRTCEvent {
  room: string;
  event: string;
  payload: unknown;
  traceId?: string;
}

export const OPENRTC_COMMENT_EVENTS = {
  threadCreated: "openrtc.comments.thread.created",
  commentCreated: "openrtc.comments.comment.created",
  commentUpdated: "openrtc.comments.comment.updated",
} as const;

export type OpenRTCCommentEventName = (typeof OPENRTC_COMMENT_EVENTS)[keyof typeof OPENRTC_COMMENT_EVENTS];
export type OpenRTCCommentEventType = "thread-created" | "comment-created" | "comment-updated";

export const OPENRTC_NOTIFICATION_EVENTS = {
  inboxCreated: "openrtc.notifications.inbox.created",
  inboxRead: "openrtc.notifications.inbox.read",
  inboxDeleted: "openrtc.notifications.inbox.deleted",
  inboxDeletedAll: "openrtc.notifications.inbox.deleted_all",
} as const;

export type OpenRTCNotificationEventName =
  (typeof OPENRTC_NOTIFICATION_EVENTS)[keyof typeof OPENRTC_NOTIFICATION_EVENTS];
export type OpenRTCNotificationDeltaType = "created" | "read" | "deleted" | "deleted-all";

export interface OpenRTCCommentEvent {
  room: string;
  event: OpenRTCCommentEventName;
  type: OpenRTCCommentEventType;
  roomId: string;
  threadId: string;
  commentId?: string;
  thread: OpenRTCAdminThread;
  comment?: OpenRTCAdminComment;
  traceId?: string;
}

export interface OpenRTCNotificationDelta {
  event: OpenRTCNotificationEventName;
  type: OpenRTCNotificationDeltaType;
  userId: string;
  notificationId?: string;
  notification?: OpenRTCAdminInboxNotification;
}

export interface OpenRTCError {
  code: string;
  message: string;
  requestId?: string;
}

export interface OpenRTCRoomState {
  room: string;
  members: string[];
  others: PresencePeer[];
  nextCursor?: string;
}

export const OPENRTC_ROOM_PERMISSIONS = {
  roomRead: "room:read",
  roomWrite: "room:write",
  roomPresenceWrite: "room:presence:write",
  storageRead: "storage:read",
  storageWrite: "storage:write",
  commentsRead: "comments:read",
  commentsWrite: "comments:write",
  feedsRead: "feeds:read",
  feedsWrite: "feeds:write",
} as const;

export type OpenRTCRoomPermission = (typeof OPENRTC_ROOM_PERMISSIONS)[keyof typeof OPENRTC_ROOM_PERMISSIONS];
export type OpenRTCAccessLevel = "none" | "read" | "write";

export interface OpenRTCPermissionMatrix {
  room?: OpenRTCAccessLevel;
  storage?: OpenRTCAccessLevel;
  comments?: OpenRTCAccessLevel;
  feeds?: OpenRTCAccessLevel;
}

export interface OpenRTCPresenceUpdate {
  room: string;
  connId: string;
  offline: boolean;
  others: PresencePeer[];
  self?: PresencePeer;
  state?: PresenceState;
  entered?: boolean;
}

export interface OpenRTCDiagnosticEvent {
  direction: "in" | "out";
  t: string;
  timestamp: number;
  id?: string;
  room?: string;
  event?: string;
  traceId?: string;
  payloadBytes?: number;
  latencyMs?: number;
}

export type OpenRTCOthersEvent =
  | { type: "enter"; user: PresencePeer }
  | { type: "update"; user: PresencePeer; updates: PresenceState }
  | { type: "leave"; user: PresencePeer }
  | { type: "reset" };

export type OpenRTCLostConnectionEvent = "lost" | "restored" | "failed";

export interface OpenRTCLostConnectionUpdate {
  room: string;
  event: OpenRTCLostConnectionEvent;
  attempts: number;
  since: number;
  error?: string;
}

export interface BroadcastOptions {
  traceId?: string;
  timeoutMs?: number;
}

export type RoomBroadcastInput = string | ({ type: string } & Record<string, unknown>);

export type JSONPatchOperationType = "add" | "remove" | "replace" | "move" | "copy" | "test";

export interface JSONPatchOperation {
  op: JSONPatchOperationType;
  path: string;
  from?: string;
  value?: unknown;
}

export type OpenRTCLiveStorageType = "LiveObject" | "LiveList" | "LiveMap";

export interface OpenRTCLiveStorageNode<TType extends OpenRTCLiveStorageType, TData> {
  liveblocksType: TType;
  data: TData;
}

export type OpenRTCLiveObject<TData extends Record<string, unknown> = Record<string, unknown>> =
  OpenRTCLiveStorageNode<"LiveObject", TData>;
export type OpenRTCLiveList<TItem = unknown> = OpenRTCLiveStorageNode<"LiveList", TItem[]>;
export type OpenRTCLiveMap<TData extends Record<string, unknown> = Record<string, unknown>> =
  OpenRTCLiveStorageNode<"LiveMap", TData>;
export type OpenRTCLiveStorageNodeValue = OpenRTCLiveObject | OpenRTCLiveList | OpenRTCLiveMap;

export type OpenRTCStorageStatus = "not-loaded" | "loading" | "synchronizing" | "synchronized" | "error";
export type OpenRTCStorageMutationKind = "set" | "patch";
export type OpenRTCStorageEventSource = "snapshot" | "optimistic" | "ack" | "remote" | "rollback";

export interface OpenRTCStorageMutationOptions {
  opId?: string;
}

export interface OpenRTCStorageEvent<TDocument = unknown> {
  room: string;
  document: TDocument | undefined;
  source: OpenRTCStorageEventSource;
  kind?: OpenRTCStorageMutationKind;
  opId?: string;
  originConnId?: string;
  operations?: JSONPatchOperation[];
}

export interface OpenRTCStorageStatusUpdate {
  room: string;
  status: OpenRTCStorageStatus;
}

export interface OpenRTCRoom {
  readonly id: string;
  getStatus(): ConnectionStatus;
  getPresence(): OpenRTCRoomPresence;
  getSelf(): PresencePeer | undefined;
  getOthers(): PresencePeer[];
  getMyPresence(): PresenceState;
  updatePresence(patch: PresenceState): string;
  setPresence(state: PresenceState): string;
  setCursor(cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions): string;
  clearCursor(): string;
  broadcastEvent(event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions): string;
  broadcastEventWithAck(event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions): Promise<OpenRTCEvent>;
  getStorage<TDocument = unknown>(): Promise<TDocument>;
  getStorageSnapshot<TDocument = unknown>(): TDocument | undefined;
  getStorageStatus(): OpenRTCStorageStatus;
  setStorage<TDocument = unknown>(
    document: TDocument,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<TDocument>;
  setLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    data: TData | OpenRTCLiveObject<TData>,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<OpenRTCLiveObject<TData>>;
  updateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    patch: Partial<TData>,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<OpenRTCLiveObject<TData>>;
  patchStorage<TDocument = unknown>(
    operations: JSONPatchOperation[],
    options?: OpenRTCStorageMutationOptions,
  ): Promise<TDocument>;
  subscribe(type: "others", callback: (others: PresencePeer[], event: OpenRTCOthersEvent) => void): () => void;
  subscribe(type: "my-presence", callback: (presence: PresenceState) => void): () => void;
  subscribe(type: "event", callback: (event: OpenRTCEvent) => void): () => void;
  subscribe(type: "comments", callback: (event: OpenRTCCommentEvent) => void): () => void;
  subscribe(type: "storage", callback: (event: OpenRTCStorageEvent) => void): () => void;
  subscribe(type: "storage-status", callback: (status: OpenRTCStorageStatus) => void): () => void;
  subscribe(type: "status", callback: (status: ConnectionStatus) => void): () => void;
  subscribe(type: "error", callback: (error: OpenRTCError) => void): () => void;
  subscribe(type: "lost-connection", callback: (event: OpenRTCLostConnectionEvent) => void): () => void;
  reconnect(): Promise<void>;
  leave(): void;
}

export interface EnterRoomResult {
  room: OpenRTCRoom;
  leave: () => void;
}

export interface OpenRTCAdminRoomRecord {
  id: string;
  metadata?: unknown;
  defaultAccesses?: string[];
  usersAccesses?: Record<string, string[]>;
  groupsAccesses?: Record<string, string[]>;
  created_at?: string;
  updated_at?: string;
}

export function accessMatrixPermissions(matrix: OpenRTCPermissionMatrix): OpenRTCRoomPermission[] {
  const permissions: OpenRTCRoomPermission[] = [];
  addMatrixPermission(permissions, matrix.room, OPENRTC_ROOM_PERMISSIONS.roomRead, OPENRTC_ROOM_PERMISSIONS.roomWrite);
  addMatrixPermission(permissions, matrix.storage, OPENRTC_ROOM_PERMISSIONS.storageRead, OPENRTC_ROOM_PERMISSIONS.storageWrite);
  addMatrixPermission(permissions, matrix.comments, OPENRTC_ROOM_PERMISSIONS.commentsRead, OPENRTC_ROOM_PERMISSIONS.commentsWrite);
  addMatrixPermission(permissions, matrix.feeds, OPENRTC_ROOM_PERMISSIONS.feedsRead, OPENRTC_ROOM_PERMISSIONS.feedsWrite);
  return permissions;
}

function addMatrixPermission(
  permissions: OpenRTCRoomPermission[],
  level: OpenRTCAccessLevel | undefined,
  readPermission: OpenRTCRoomPermission,
  writePermission: OpenRTCRoomPermission,
): void {
  if (level === "read") {
    permissions.push(readPermission);
  } else if (level === "write") {
    permissions.push(writePermission);
  }
}

export interface OpenRTCAdminRoomInput {
  id: string;
  metadata?: unknown;
  defaultAccesses?: string[];
  usersAccesses?: Record<string, string[]>;
  groupsAccesses?: Record<string, string[]>;
}

export interface OpenRTCAdminRoomUpdate {
  metadata?: unknown;
  defaultAccesses?: string[];
  usersAccesses?: Record<string, string[]>;
  groupsAccesses?: Record<string, string[]>;
}

export interface OpenRTCAdminRoomList {
  rooms: OpenRTCAdminRoomRecord[];
  next_cursor?: string;
}

export interface OpenRTCAdminActiveUser {
  type: string;
  connection_id: string;
  id: string;
  tenant?: string;
  node_id?: string;
  connected_at?: string;
  presence?: unknown;
}

export interface OpenRTCAdminThread {
  type: string;
  id: string;
  roomId: string;
  comments: OpenRTCAdminComment[];
  resolved: boolean;
  metadata?: unknown;
  createdAt: string;
  updatedAt: string;
}

export interface OpenRTCAdminComment {
  type: string;
  threadId: string;
  roomId: string;
  id: string;
  userId: string;
  createdAt: string;
  editedAt?: string;
  deletedAt?: string;
  body?: unknown;
  metadata?: unknown;
  mentions?: string[];
  reactions?: OpenRTCAdminCommentReaction[];
}

export interface OpenRTCAdminCommentReaction {
  emoji: string;
  userId: string;
}

export interface OpenRTCAdminThreadInput {
  id?: string;
  metadata?: unknown;
  comment: OpenRTCAdminCommentInput;
}

export interface OpenRTCAdminCommentInput {
  id?: string;
  userId: string;
  body: unknown;
  metadata?: unknown;
  mentions?: string[];
  reactions?: OpenRTCAdminCommentReaction[];
}

export interface OpenRTCAdminCommentUpdate {
  body?: unknown;
  metadata?: unknown;
  mentions?: string[];
  reactions?: OpenRTCAdminCommentReaction[];
}

export interface OpenRTCAdminInboxNotification {
  id: string;
  userId: string;
  kind: string;
  subjectId?: string;
  threadId?: string;
  roomId?: string;
  readAt?: string;
  notifiedAt: string;
  activityData?: unknown;
}

export interface OpenRTCAdminInboxNotificationInput {
  id?: string;
  userId: string;
  kind: string;
  subjectId?: string;
  threadId?: string;
  roomId?: string;
  activityData?: unknown;
}

export interface OpenRTCAdminRoomSubscriptionSettings {
  roomId: string;
  userId: string;
  threads: "all" | "replies_and_mentions" | "none";
  textMentions: "mine" | "none";
  updatedAt?: string;
}

export interface OpenRTCAdminRoomSubscriptionSettingsInput {
  threads?: "all" | "replies_and_mentions" | "none";
  textMentions?: "mine" | "none";
}

export interface OpenRTCListResponse<T> {
  data: T[];
  next_cursor?: string;
}

export interface OpenRTCAdminPublishOptions {
  excludeSenderConnId?: string;
  traceId?: string;
}

export interface OpenRTCAdminPresenceOptions {
  ttlSeconds?: number;
}

export interface OpenRTCEventMap {
  status: ConnectionStatus;
  hello: { connId: string; nodeId?: string };
  room: OpenRTCRoomState;
  presence: OpenRTCPresenceUpdate;
  event: OpenRTCEvent;
  comment: OpenRTCCommentEvent;
  notification: OpenRTCNotificationDelta;
  storage: OpenRTCStorageEvent;
  "storage-status": OpenRTCStorageStatusUpdate;
  error: OpenRTCError;
  diagnostic: OpenRTCDiagnosticEvent;
  "lost-connection": OpenRTCLostConnectionUpdate;
  message: unknown;
}

type Handler<T> = (event: T) => void;

interface PendingSend {
  sentAt: number;
  t: string;
  room?: string;
  event?: string;
  traceId?: string;
}

interface PendingPresenceSend extends PendingSend {
  id: string;
}

interface PendingStorageGet {
  resolve(document: unknown): void;
  reject(error: Error): void;
}

interface PendingStorageMutation extends PendingStorageGet {
  room: string;
  hadPreviousDocument: boolean;
  previousDocument?: unknown;
}

interface ActiveRoomEntry {
  options: JoinOptions;
  lost: boolean;
  failed: boolean;
}

interface NormalizedReconnectOptions {
  initialDelayMs: number;
  maxDelayMs: number;
  maxAttempts: number;
  jitterRatio: number;
}

interface OpenRTCBrowserDocument {
  readonly hidden?: boolean;
  readonly visibilityState?: string;
  addEventListener(type: string, listener: (event: unknown) => void): void;
  removeEventListener(type: string, listener: (event: unknown) => void): void;
}

const WS_OPEN = 1;
const DEFAULT_LOST_CONNECTION_TIMEOUT_MS = 5000;
const MIN_LOST_CONNECTION_TIMEOUT_MS = 1000;
const MAX_LOST_CONNECTION_TIMEOUT_MS = 30000;

export class OpenRTCClient {
  readonly url: string;

  private readonly token: OpenRTCClientOptions["token"];
  private readonly WebSocketCtor: OpenRTCWebSocketConstructor;
  private readonly autoReconnect: boolean;
  private readonly lostConnectionTimeoutMs: number;
  private readonly backgroundKeepAliveTimeoutMs: number | undefined;
  private readonly reconnectOptions: NormalizedReconnectOptions;
  private readonly browserDocument: OpenRTCBrowserDocument | undefined;
  private socket: OpenRTCWebSocket | undefined;
  private requestCounter = 0;
  private statusValue: ConnectionStatus = "idle";
  private connIdValue: string | undefined;
  private handlers = new Map<keyof OpenRTCEventMap, Set<Handler<OpenRTCEventMap[keyof OpenRTCEventMap]>>>();
  private presenceByRoom = new Map<string, Map<string, PresenceState>>();
  private membersByRoom = new Map<string, Set<string>>();
  private localPresenceByRoom = new Map<string, PresenceState>();
  private nextCursorByRoom = new Map<string, string>();
  private storageByRoom = new Map<string, unknown>();
  private storageStatusByRoom = new Map<string, OpenRTCStorageStatus>();
  private storageRequestedRooms = new Set<string>();
  private pendingRequests = new Map<string, PendingSend>();
  private pendingTraces = new Map<string, PendingSend>();
  private pendingPresenceByRoom = new Map<string, PendingPresenceSend[]>();
  private pendingStorageGets = new Map<string, string>();
  private storageGetWaitersByRoom = new Map<string, PendingStorageGet[]>();
  private pendingStorageMutations = new Map<string, PendingStorageMutation>();
  private roomHandles = new Map<string, OpenRTCRoomHandle>();
  private activeRooms = new Map<string, ActiveRoomEntry>();
  private roomRetainCounts = new Map<string, number>();
  private manualClose = false;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private lostConnectionTimer: ReturnType<typeof setTimeout> | undefined;
  private backgroundKeepAliveTimer: ReturnType<typeof setTimeout> | undefined;
  private lostConnectionSince = 0;
  private socketGeneration = 0;
  private connectPromise: Promise<void> | undefined;
  private needsRoomReplay = false;
  private backgroundSuspended = false;
  private readonly visibilityChangeHandler = (): void => {
    this.handleVisibilityChange();
  };

  constructor(options: OpenRTCClientOptions) {
    this.url = options.url;
    this.token = options.token;
    const defaultCtor = globalThis.WebSocket as unknown as OpenRTCWebSocketConstructor | undefined;
    if (!options.WebSocket && !defaultCtor) {
      throw new Error("A WebSocket constructor is required in this environment");
    }
    this.WebSocketCtor = options.WebSocket ?? defaultCtor!;
    this.autoReconnect = options.autoReconnect ?? true;
    this.lostConnectionTimeoutMs = normalizeLostConnectionTimeout(options.lostConnectionTimeout);
    this.backgroundKeepAliveTimeoutMs = normalizeBackgroundKeepAliveTimeout(options.backgroundKeepAliveTimeout);
    this.reconnectOptions = normalizeReconnectOptions(options.reconnect);
    this.browserDocument = this.backgroundKeepAliveTimeoutMs === undefined ? undefined : getBrowserDocument();
    if (this.browserDocument) {
      this.browserDocument.addEventListener("visibilitychange", this.visibilityChangeHandler);
      this.handleVisibilityChange();
    }
  }

  get status(): ConnectionStatus {
    return this.statusValue;
  }

  get connId(): string | undefined {
    return this.connIdValue;
  }

  async connect(): Promise<void> {
    if (this.statusValue === "open") {
      return;
    }
    if (this.connectPromise) {
      return this.connectPromise;
    }
    this.manualClose = false;
    this.backgroundSuspended = false;
    this.clearReconnectTimer();
    this.setStatus(this.statusValue === "reconnecting" ? "reconnecting" : "connecting");
    this.connectPromise = this.openSocket({ reconnecting: this.statusValue === "reconnecting" }).finally(() => {
      this.connectPromise = undefined;
    });
    return this.connectPromise;
  }

  async reconnect(): Promise<void> {
    this.manualClose = false;
    this.backgroundSuspended = false;
    this.clearBackgroundKeepAliveTimer();
    this.clearReconnectTimer();
    this.reconnectAttempts = 0;
    if (this.socket?.readyState === WS_OPEN) {
      this.replayActiveRooms();
      this.setStatus("open");
      return;
    }
    this.setStatus(this.statusValue === "idle" || this.statusValue === "closed" ? "connecting" : "reconnecting");
    await this.connect();
  }

  close(): void {
    this.manualClose = true;
    this.backgroundSuspended = false;
    this.clearReconnectTimer();
    this.clearLostConnectionTimer();
    this.clearBackgroundKeepAliveTimer();
    this.socket?.close();
    this.socket = undefined;
    this.pendingRequests.clear();
    this.pendingTraces.clear();
    this.pendingPresenceByRoom.clear();
    this.rejectPendingStorage(new Error("OpenRTC client closed before storage request completed"));
    this.connIdValue = undefined;
    this.activeRooms.clear();
    this.roomRetainCounts.clear();
    this.needsRoomReplay = false;
    this.resetRooms({ preserveLocal: false });
    this.setStatus("closed");
  }

  destroy(): void {
    this.close();
    this.browserDocument?.removeEventListener("visibilitychange", this.visibilityChangeHandler);
  }

  enterRoom(room: string, options: EnterRoomOptions = {}): EnterRoomResult {
    const handle = this.getOrCreateRoomHandle(room);
    this.retainRoom(room, options);
    if (options.initialPresence) {
      this.updatePresence(room, options.initialPresence);
    }
    let left = false;
    return {
      room: handle,
      leave: () => {
        if (!left) {
          left = true;
          this.releaseRoom(room);
        }
      },
    };
  }

  join(room: string, options: JoinOptions = {}): string {
    this.activeRooms.set(room, { options: compactJoinOptions(options), lost: false, failed: false });
    return this.sendJoin(room, options);
  }

  leave(room: string): string {
    this.activeRooms.delete(room);
    this.roomRetainCounts.delete(room);
    this.resetRooms({ rooms: [room], preserveLocal: false });
    const id = this.nextID("leave");
    if (this.canSend()) {
      this.send({ t: "LEAVE", id, room });
    }
    return id;
  }

  private sendJoin(room: string, options: JoinOptions = {}): string {
    const id = this.nextID("join");
    const meta: Record<string, unknown> = {};
    if (options.limit !== undefined) {
      meta["limit"] = options.limit;
    }
    if (options.cursor !== undefined) {
      meta["cursor"] = options.cursor;
    }
    if (this.canSend()) {
      this.send({
        t: "JOIN",
        id,
        room,
        ...(Object.keys(meta).length > 0 ? { meta } : {}),
      });
    } else {
      this.needsRoomReplay = true;
    }
    return id;
  }

  broadcast(room: string, event: string, payload: unknown, traceId?: string): string {
    const id = this.nextID("emit");
    try {
      this.send({
        t: "EMIT",
        id,
        room,
        event,
        payload,
        ...(traceId ? { meta: { trace_id: traceId } } : {}),
      });
    } finally {
      this.pendingRequests.delete(id);
    }
    return id;
  }

  updatePresence(room: string, state: PresenceState): string {
    this.localPresenceByRoom.set(room, state);
    const id = this.nextID("presence");
    if (this.canSend()) {
      this.send({ t: "PRESENCE_SET", id, room, payload: state });
    } else if (this.activeRooms.has(room)) {
      this.needsRoomReplay = true;
    }
    return id;
  }

  patchPresence(room: string, patch: PresenceState): string {
    const current = this.localPresenceByRoom.get(room) ?? {};
    return this.updatePresence(room, { ...current, ...patch });
  }

  setUser(room: string, user: OpenRTCUserInfo): string {
    return this.patchPresence(room, { user });
  }

  setCursor(room: string, cursor: OpenRTCCursor | null, options: OpenRTCCursorOptions = {}): string {
    const patch: PresenceState = {
      ...(options.metadata ?? {}),
      cursor,
    };
    if (options.user !== undefined) {
      patch["user"] = options.user;
    }
    if (options.color !== undefined) {
      patch["color"] = options.color;
    }
    if (options.mode !== undefined) {
      patch["mode"] = options.mode;
    }
    return this.patchPresence(room, patch);
  }

  clearCursor(room: string): string {
    return this.patchPresence(room, { cursor: null });
  }

  broadcastWithAck(
    room: string,
    event: string,
    payload: unknown,
    options: { traceId?: string; timeoutMs?: number } = {},
  ): Promise<OpenRTCEvent> {
    const traceId = options.traceId ?? this.nextID("trace");
    const timeoutMs = options.timeoutMs ?? 5000;

    return new Promise<OpenRTCEvent>((resolve, reject) => {
      let settled = false;
      let timeout: ReturnType<typeof setTimeout> | undefined;
      let off = (): void => {};
      const cleanup = (cancelTrace = false): void => {
        settled = true;
        off();
        if (cancelTrace) {
          this.cancelTrace(traceId);
        }
        if (timeout) {
          clearTimeout(timeout);
        }
      };
      off = this.on("event", (incoming) => {
        if (incoming.room === room && incoming.event === event && incoming.traceId === traceId) {
          cleanup();
          resolve(incoming);
        }
      });

      timeout = setTimeout(() => {
        if (!settled) {
          cleanup(true);
          reject(new Error(`Timed out waiting for ${event} ack in ${room}`));
        }
      }, timeoutMs);

      try {
        this.broadcast(room, event, payload, traceId);
      } catch (error) {
        cleanup(true);
        reject(error instanceof Error ? error : new Error(String(error)));
      }
    });
  }

  getStorage<TDocument = unknown>(room: string): Promise<TDocument> {
    if (this.storageByRoom.has(room) && this.getStorageStatus(room) === "synchronized") {
      return Promise.resolve(this.storageByRoom.get(room) as TDocument);
    }

    return new Promise<TDocument>((resolve, reject) => {
      const waiters = this.storageGetWaitersByRoom.get(room) ?? [];
      waiters.push({
        resolve: (document) => {
          resolve(document as TDocument);
        },
        reject,
      });
      this.storageGetWaitersByRoom.set(room, waiters);
      this.requestStorageSnapshot(room);
    });
  }

  getStorageSnapshot<TDocument = unknown>(room: string): TDocument | undefined {
    return this.storageByRoom.get(room) as TDocument | undefined;
  }

  getStorageStatus(room: string): OpenRTCStorageStatus {
    return this.storageStatusByRoom.get(room) ?? "not-loaded";
  }

  setStorage<TDocument = unknown>(
    room: string,
    document: TDocument,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<TDocument> {
    return this.mutateStorage<TDocument>("set", room, document, options);
  }

  setLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    room: string,
    data: TData | OpenRTCLiveObject<TData>,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<OpenRTCLiveObject<TData>> {
    return this.setStorage<OpenRTCLiveObject<TData>>(room, normalizeLiveObjectRoot(data), options);
  }

  updateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    room: string,
    patch: Partial<TData>,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<OpenRTCLiveObject<TData>> {
    return this.patchStorage<OpenRTCLiveObject<TData>>(room, liveObjectPatch(patch), options);
  }

  patchStorage<TDocument = unknown>(
    room: string,
    operations: JSONPatchOperation[],
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<TDocument> {
    return this.mutateStorage<TDocument>("patch", room, operations, options);
  }

  getRoomState(room: string): OpenRTCRoomState {
    return {
      room,
      members: [...(this.membersByRoom.get(room) ?? new Set<string>())],
      others: this.getOthers(room),
      ...(this.nextCursorByRoom.has(room) ? { nextCursor: this.nextCursorByRoom.get(room)! } : {}),
    };
  }

  getPresence(room: string): OpenRTCRoomPresence {
    return {
      ...this.getRoomState(room),
      ...(this.getSelf(room) ? { self: this.getSelf(room)! } : {}),
    };
  }

  getMyPresence(room: string): PresenceState {
    return this.localPresenceByRoom.get(room) ?? {};
  }

  getSelf(room: string): PresencePeer | undefined {
    if (!this.connIdValue) {
      return undefined;
    }
    const state = this.presenceByRoom.get(room)?.get(this.connIdValue) ?? this.localPresenceByRoom.get(room);
    if (!state) {
      return undefined;
    }
    return { connId: this.connIdValue, state };
  }

  getOthers(room: string): PresencePeer[] {
    const roomPresence = this.presenceByRoom.get(room);
    if (!roomPresence) {
      return [];
    }
    return [...roomPresence.entries()]
      .filter(([connId]) => connId !== this.connIdValue)
      .map(([connId, state]) => ({ connId, state }));
  }

  room(room: string): OpenRTCRoom {
    return this.getOrCreateRoomHandle(room);
  }

  on<K extends keyof OpenRTCEventMap>(type: K, handler: Handler<OpenRTCEventMap[K]>): () => void {
    let handlers = this.handlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.handlers.set(type, handlers);
    }
    handlers.add(handler as Handler<OpenRTCEventMap[keyof OpenRTCEventMap]>);
    return () => {
      handlers?.delete(handler as Handler<OpenRTCEventMap[keyof OpenRTCEventMap]>);
    };
  }

  private async openSocket(options: { reconnecting: boolean }): Promise<void> {
    const generation = ++this.socketGeneration;
    let token: string;
    try {
      token = await this.resolveToken();
    } catch (error) {
      if (!options.reconnecting && !this.manualClose) {
        this.setStatus("error");
      }
      throw error;
    }
    if (this.manualClose || generation !== this.socketGeneration) {
      return;
    }

    const socket = new this.WebSocketCtor(withToken(toWebSocketURL(this.url), token));
    this.socket = socket;

    await new Promise<void>((resolve, reject) => {
      let opened = false;
      const cleanupOpenListeners = (): void => {
        socket.removeEventListener("open", onOpen as (event: unknown) => void);
      };
      const cleanupAll = (): void => {
        cleanupOpenListeners();
        socket.removeEventListener("error", onError as (event: unknown) => void);
        socket.removeEventListener("close", onClose as (event: unknown) => void);
        socket.removeEventListener("message", onMessage as (event: unknown) => void);
      };
      const onOpen = (): void => {
        opened = true;
        cleanupOpenListeners();
        this.clearReconnectTimer();
        this.reconnectAttempts = 0;
        this.setStatus("open");
        resolve();
      };
      const onError = (event: unknown): void => {
        const error = event instanceof Error ? event : new Error("WebSocket connection failed");
        if (!opened) {
          cleanupAll();
          if (this.socket === socket) {
            this.socket = undefined;
          }
          if (!options.reconnecting && !this.manualClose) {
            this.setStatus("error");
          }
          reject(error);
          return;
        }
        this.emit("error", { code: "SOCKET_ERROR", message: error.message });
      };
      const onClose = (): void => {
        cleanupAll();
        if (this.socket === socket) {
          this.socket = undefined;
          this.handleSocketClosed();
        }
      };
      const onMessage = (event: unknown): void => {
        if (generation !== this.socketGeneration || this.socket !== socket) {
          return;
        }
        const data = readMessageData(event);
        if (typeof data === "string") {
          try {
            this.handleMessage(JSON.parse(data) as unknown, byteLength(data));
          } catch (error) {
            this.emit("error", {
              code: "BAD_RESPONSE",
              message: error instanceof Error ? error.message : "WebSocket message was not valid JSON",
            });
          }
        }
      };
      socket.addEventListener("open", onOpen as (event: unknown) => void);
      socket.addEventListener("error", onError as (event: unknown) => void);
      socket.addEventListener("close", onClose as (event: unknown) => void);
      socket.addEventListener("message", onMessage as (event: unknown) => void);
    });
  }

  private handleSocketClosed(): void {
    const previousConnId = this.connIdValue;
    this.pendingRequests.clear();
    this.pendingTraces.clear();
    this.pendingPresenceByRoom.clear();
    this.rejectPendingStorage(new Error("OpenRTC socket closed before storage request completed"));
    if (previousConnId) {
      for (const presence of this.presenceByRoom.values()) {
        presence.delete(previousConnId);
      }
      for (const members of this.membersByRoom.values()) {
        members.delete(previousConnId);
      }
    }
    this.connIdValue = undefined;

    if (this.manualClose) {
      this.resetRooms({ preserveLocal: false });
      this.setStatus("closed");
      return;
    }

    if (this.backgroundSuspended) {
      this.needsRoomReplay = true;
      this.startLostConnectionTimer();
      this.setStatus("closed");
      return;
    }

    if (!this.autoReconnect) {
      this.resetRooms({ preserveLocal: false });
      this.setStatus("closed");
      return;
    }

    this.needsRoomReplay = true;
    this.setStatus("reconnecting");
    this.startLostConnectionTimer();
    this.scheduleReconnect();
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer || this.manualClose || !this.autoReconnect) {
      return;
    }
    if (this.reconnectAttempts >= this.reconnectOptions.maxAttempts) {
      this.failLostConnections(new Error("Reconnect attempts exhausted"));
      this.setStatus("error");
      return;
    }

    this.reconnectAttempts += 1;
    const attempt = this.reconnectAttempts;
    const delay = reconnectDelayMs(attempt, this.reconnectOptions);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = undefined;
      if (this.manualClose) {
        return;
      }
      void this.openSocket({ reconnecting: true }).catch(() => {
        this.scheduleReconnect();
      });
    }, delay);
  }

  private startLostConnectionTimer(): void {
    if (this.lostConnectionTimer || this.activeRooms.size === 0) {
      return;
    }
    this.lostConnectionSince = Date.now();
    this.lostConnectionTimer = setTimeout(() => {
      this.lostConnectionTimer = undefined;
      const rooms = [...this.activeRooms.keys()];
      for (const room of rooms) {
        const active = this.activeRooms.get(room);
        if (active && !active.lost && !active.failed) {
          active.lost = true;
          this.emitLostConnection(room, "lost");
        }
      }
      this.resetRooms({ rooms, preserveLocal: true });
    }, this.lostConnectionTimeoutMs);
  }

  private clearLostConnectionTimer(): void {
    if (this.lostConnectionTimer) {
      clearTimeout(this.lostConnectionTimer);
      this.lostConnectionTimer = undefined;
    }
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
  }

  private startBackgroundKeepAliveTimer(): void {
    if (this.backgroundKeepAliveTimeoutMs === undefined || this.backgroundKeepAliveTimer || this.manualClose) {
      return;
    }
    this.backgroundKeepAliveTimer = setTimeout(() => {
      this.backgroundKeepAliveTimer = undefined;
      if (this.isDocumentHidden()) {
        this.suspendForBackground();
      }
    }, this.backgroundKeepAliveTimeoutMs);
  }

  private clearBackgroundKeepAliveTimer(): void {
    if (this.backgroundKeepAliveTimer) {
      clearTimeout(this.backgroundKeepAliveTimer);
      this.backgroundKeepAliveTimer = undefined;
    }
  }

  private suspendForBackground(): void {
    if (this.manualClose || this.backgroundSuspended) {
      return;
    }
    this.backgroundSuspended = true;
    this.needsRoomReplay = this.activeRooms.size > 0;
    this.clearReconnectTimer();
    if (this.socket) {
      this.socket.close(1000, "OpenRTC background keep-alive timeout");
      return;
    }
    this.startLostConnectionTimer();
    this.setStatus("closed");
  }

  private handleVisibilityChange(): void {
    if (!this.browserDocument) {
      return;
    }
    if (this.isDocumentHidden()) {
      this.startBackgroundKeepAliveTimer();
      return;
    }
    this.clearBackgroundKeepAliveTimer();
    if (this.backgroundSuspended && !this.manualClose) {
      this.backgroundSuspended = false;
      void this.reconnect();
    }
  }

  private isDocumentHidden(): boolean {
    return this.browserDocument?.hidden === true || this.browserDocument?.visibilityState === "hidden";
  }

  private replayActiveRooms(): void {
    if (!this.canSend()) {
      return;
    }
    for (const [room, active] of this.activeRooms) {
      active.failed = false;
      this.sendJoin(room, active.options);
      if (this.localPresenceByRoom.has(room)) {
        const id = this.nextID("presence");
        this.send({ t: "PRESENCE_SET", id, room, payload: this.localPresenceByRoom.get(room) ?? {} });
      }
      if (this.storageRequestedRooms.has(room)) {
        this.requestStorageSnapshot(room);
      }
    }
  }

  private retainRoom(room: string, options: JoinOptions): void {
    const count = this.roomRetainCounts.get(room) ?? 0;
    this.roomRetainCounts.set(room, count + 1);
    if (count === 0) {
      this.join(room, options);
      return;
    }

    const active = this.activeRooms.get(room);
    if (active) {
      active.options = { ...active.options, ...compactJoinOptions(options) };
    }
  }

  private releaseRoom(room: string): void {
    const count = this.roomRetainCounts.get(room) ?? 0;
    if (count > 1) {
      this.roomRetainCounts.set(room, count - 1);
      return;
    }
    this.leave(room);
  }

  private failLostConnections(error: Error): void {
    this.clearLostConnectionTimer();
    for (const [room, active] of this.activeRooms) {
      if (!active.failed) {
        active.failed = true;
        this.emitLostConnection(room, "failed", error.message);
      }
    }
    this.resetRooms({ rooms: this.activeRooms.keys(), preserveLocal: true });
  }

  private failRoomJoin(room: string, error: string): void {
    const active = this.activeRooms.get(room);
    if (!active) {
      return;
    }
    active.failed = true;
    active.lost = false;
    this.emitLostConnection(room, "failed", error || "Room join failed");
    this.resetRooms({ rooms: [room], preserveLocal: true });
  }

  private restoreLostConnection(room: string): void {
    this.clearLostConnectionTimer();
    const active = this.activeRooms.get(room);
    if (!active) {
      return;
    }
    active.failed = false;
    if (active.lost) {
      active.lost = false;
      this.emitLostConnection(room, "restored");
    }
  }

  private emitLostConnection(room: string, event: OpenRTCLostConnectionEvent, error?: string): void {
    this.emit("lost-connection", {
      room,
      event,
      attempts: this.reconnectAttempts,
      since: this.lostConnectionSince || Date.now(),
      ...(error ? { error } : {}),
    });
  }

  private canSend(): boolean {
    return this.socket?.readyState === WS_OPEN;
  }

  private requestStorageSnapshot(room: string): void {
    this.storageRequestedRooms.add(room);
    this.setStorageStatus(room, "loading");
    if (this.hasPendingStorageGet(room)) {
      return;
    }
    if (!this.canSend()) {
      if (this.activeRooms.has(room)) {
        this.needsRoomReplay = true;
        return;
      }
      this.rejectStorageGetWaiters(room, new Error("OpenRTC socket is not open"));
      this.setStorageStatus(room, "error");
      return;
    }
    const id = this.nextID("storage-get");
    this.pendingStorageGets.set(id, room);
    try {
      this.send({ t: "STORAGE_GET", id, room });
    } catch (error) {
      this.pendingStorageGets.delete(id);
      this.rejectStorageGetWaiters(room, error instanceof Error ? error : new Error(String(error)));
      this.setStorageStatus(room, "error");
    }
  }

  private mutateStorage<TDocument>(
    kind: OpenRTCStorageMutationKind,
    room: string,
    payload: unknown,
    options: OpenRTCStorageMutationOptions,
  ): Promise<TDocument> {
    const id = this.nextID(kind === "set" ? "storage-set" : "storage-patch");
    const opId = options.opId;
    const operations = kind === "patch" ? asJSONPatchOperations(payload) : undefined;
    if (kind === "patch" && !operations) {
      return Promise.reject(new Error("Storage patch must be a valid JSON Patch operation array"));
    }
    const hadPreviousDocument = this.storageByRoom.has(room);
    const previousDocument = hadPreviousDocument ? cloneStorageDocument(this.storageByRoom.get(room)) : undefined;
    let optimisticDocument: unknown | undefined;
    let hasOptimisticDocument = false;

    try {
      if (kind === "set") {
        optimisticDocument = cloneStorageDocument(payload);
        hasOptimisticDocument = true;
      } else if (hadPreviousDocument) {
        optimisticDocument = applyJSONPatchOptimistic(previousDocument, operations ?? []);
        hasOptimisticDocument = true;
      }
    } catch (error) {
      return Promise.reject(error instanceof Error ? error : new Error(String(error)));
    }

    const message: Record<string, unknown> = {
      t: kind === "set" ? "STORAGE_SET" : "STORAGE_PATCH",
      id,
      room,
      payload: kind === "patch" ? operations : payload,
      ...(opId ? { meta: { op_id: opId } } : {}),
    };

    return new Promise<TDocument>((resolve, reject) => {
      this.pendingStorageMutations.set(id, {
        room,
        hadPreviousDocument,
        ...(hadPreviousDocument ? { previousDocument } : {}),
        resolve: (document) => {
          resolve(document as TDocument);
        },
        reject,
      });
      this.storageRequestedRooms.add(room);
      this.setStorageStatus(room, "synchronizing");
      if (hasOptimisticDocument) {
        this.applyStorageMessage(room, optimisticDocument, {
          source: "optimistic",
          kind,
          ...(opId ? { opId } : {}),
          ...(kind === "patch" && operations ? { operations } : {}),
        });
      }
      try {
        this.send(message);
      } catch (error) {
        const pending = this.pendingStorageMutations.get(id);
        this.pendingStorageMutations.delete(id);
        const normalized = error instanceof Error ? error : new Error(String(error));
        if (pending) {
          this.rollbackStorageMutation(pending);
        } else {
          this.setStorageStatus(room, "error");
        }
        reject(normalized);
      }
    });
  }

  private async resolveToken(): Promise<string> {
    return typeof this.token === "function" ? this.token() : this.token;
  }

  private send(payload: Record<string, unknown>): void {
    if (!this.socket || this.socket.readyState !== WS_OPEN) {
      throw new Error("OpenRTC socket is not open");
    }
    const encoded = JSON.stringify(payload);
    const timestamp = Date.now();
    const t = asString(payload["t"]);
    const id = optionalString(payload["id"]);
    const room = optionalString(payload["room"]);
    const event = optionalString(payload["event"]);
    const meta = asRecord(payload["meta"]);
    const traceId = optionalString(meta["trace_id"]);
    const pending: PendingSend = {
      sentAt: timestamp,
      t,
      ...(room ? { room } : {}),
      ...(event ? { event } : {}),
      ...(traceId ? { traceId } : {}),
    };
    if (id) {
      this.pendingRequests.set(id, pending);
    }
    if (traceId) {
      this.pendingTraces.set(traceId, pending);
    }
    if (t === "PRESENCE_SET" && id && room) {
      const queue = this.pendingPresenceByRoom.get(room) ?? [];
      queue.push({ ...pending, id });
      this.pendingPresenceByRoom.set(room, queue);
    }

    this.emitDiagnostic({
      direction: "out",
      t,
      timestamp,
      ...(id ? { id } : {}),
      ...(room ? { room } : {}),
      ...(event ? { event } : {}),
      ...(traceId ? { traceId } : {}),
      payloadBytes: byteLength(encoded),
    });
    this.socket.send(encoded);
  }

  private nextID(prefix: string): string {
    this.requestCounter += 1;
    return `${prefix}-${this.requestCounter}`;
  }

  private handleMessage(message: unknown, payloadBytes?: number): void {
    this.emit("message", message);
    if (!isObject(message)) {
      return;
    }

    const type = asString(message["t"]);
    const timestamp = Date.now();
    const room = optionalString(message["room"]);
    const eventName = optionalString(message["event"]);
    const meta = asRecord(message["meta"]);
    const traceId = optionalString(meta["trace_id"]);
    const payload = asRecord(message["payload"]);
    let requestId = optionalString(message["id"]);
    if (type === "ERROR") {
      requestId = requestId ?? optionalString(payload["request_id"]);
    }
    const pendingRequest = requestId ? this.pendingRequests.get(requestId) : undefined;
    let latencyMs = requestId ? this.completeRequest(requestId, timestamp) : undefined;
    if (type === "EVENT" && traceId) {
      latencyMs = this.completeTrace(traceId, timestamp) ?? latencyMs;
    }
    if (type === "PRESENCE" && room) {
      const connId = optionalString(payload["conn_id"]);
      if (connId && connId === this.connIdValue) {
        const pendingPresence = this.shiftPendingPresence(room);
        if (pendingPresence) {
          latencyMs = timestamp - pendingPresence.sentAt;
          requestId = pendingPresence.id;
          this.pendingRequests.delete(pendingPresence.id);
        }
      }
    }
    this.emitDiagnostic({
      direction: "in",
      t: type,
      timestamp,
      ...(requestId ? { id: requestId } : {}),
      ...(room ? { room } : {}),
      ...(eventName ? { event: eventName } : {}),
      ...(traceId ? { traceId } : {}),
      ...(payloadBytes !== undefined ? { payloadBytes } : {}),
      ...(latencyMs !== undefined ? { latencyMs } : {}),
    });

    if (type === "HELLO") {
      const connId = asString(payload["conn_id"]);
      this.connIdValue = connId;
      const server = asRecord(payload["server"]);
      const nodeId = optionalString(server["node_id"]);
      this.emit("hello", {
        connId,
        ...(nodeId ? { nodeId } : {}),
      });
      if (this.needsRoomReplay) {
        this.needsRoomReplay = false;
        this.replayActiveRooms();
      }
      return;
    }

    if (type === "JOINED") {
      const room = asString(message["room"]);
      const members = asStringArray(payload["members"]);
      const presence = asPresenceMap(payload["presence"]);
      this.membersByRoom.set(room, new Set(members));
      this.presenceByRoom.set(room, presence);
      const nextCursor = optionalString(payload["next_cursor"]);
      if (nextCursor) {
        this.nextCursorByRoom.set(room, nextCursor);
      } else {
        this.nextCursorByRoom.delete(room);
      }
      const self = this.connIdValue ? presence.get(this.connIdValue) : undefined;
      if (self) {
        this.localPresenceByRoom.set(room, self);
      }
      this.emit("room", {
        room,
        members,
        others: this.getOthers(room),
        ...(nextCursor ? { nextCursor } : {}),
      });
      this.restoreLostConnection(room);
      return;
    }

    if (type === "LEFT") {
      const room = asString(message["room"]);
      this.membersByRoom.delete(room);
      this.presenceByRoom.delete(room);
      this.localPresenceByRoom.delete(room);
      this.nextCursorByRoom.delete(room);
      this.emit("room", { room, members: [], others: [] });
      return;
    }

    if (type === "PRESENCE") {
      const room = asString(message["room"]);
      const connId = asString(payload["conn_id"]);
      const offline = payload["offline"] === true;
      const roomPresence = this.getOrCreatePresence(room);
      const members = this.getOrCreateMembers(room);
      const existingState = roomPresence.get(connId);
      const entered = !roomPresence.has(connId);
      if (offline) {
        roomPresence.delete(connId);
        members.delete(connId);
        if (connId === this.connIdValue) {
          this.localPresenceByRoom.delete(room);
        }
        this.emit("presence", {
          room,
          connId,
          offline,
          others: this.getOthers(room),
          ...(existingState ? { state: existingState } : {}),
          ...(this.getSelf(room) ? { self: this.getSelf(room)! } : {}),
        });
      } else {
        const state = asPresenceState(payload["state"]);
        roomPresence.set(connId, state);
        members.add(connId);
        if (connId === this.connIdValue) {
          this.localPresenceByRoom.set(room, state);
        }
        this.emit("presence", {
          room,
          connId,
          offline,
          state,
          others: this.getOthers(room),
          ...(entered ? { entered } : {}),
          ...(this.getSelf(room) ? { self: this.getSelf(room)! } : {}),
        });
      }
      return;
    }

    if (type === "EVENT") {
      const meta = asRecord(message["meta"]);
      const traceId = optionalString(meta["trace_id"]);
      const event: OpenRTCEvent = {
        room: asString(message["room"]),
        event: asString(message["event"]),
        payload: message["payload"],
        ...(traceId ? { traceId } : {}),
      };
      this.emit("event", event);
      const commentEvent = asOpenRTCCommentEvent(event);
      if (commentEvent) {
        this.emit("comment", commentEvent);
      }
      return;
    }

    if (type === "NOTIFICATION") {
      const notification = asOpenRTCNotificationDelta(asString(message["event"]), message["payload"]);
      if (notification) {
        this.emit("notification", notification);
      }
      return;
    }

    if (type === "STORAGE_SNAPSHOT") {
      const room = asString(message["room"]);
      const document = payload["document"];
      if (requestId) {
        this.pendingStorageGets.delete(requestId);
      }
      this.applyStorageMessage(room, document, { source: "snapshot" });
      this.resolveStorageGetWaiters(room, document);
      return;
    }

    if (type === "STORAGE_ACK") {
      const room = asString(message["room"]);
      const document = payload["document"];
      const kind = asStorageMutationKind(payload["kind"]);
      const opId = optionalString(payload["op_id"]);
      this.applyStorageMessage(room, document, {
        source: "ack",
        ...(kind ? { kind } : {}),
        ...(opId ? { opId } : {}),
      });
      if (requestId) {
        const pending = this.pendingStorageMutations.get(requestId);
        if (pending) {
          this.pendingStorageMutations.delete(requestId);
          pending.resolve(document);
        }
      }
      return;
    }

    if (type === "STORAGE_UPDATE") {
      const room = asString(message["room"]);
      const document = payload["document"];
      const kind = asStorageMutationKind(payload["kind"]);
      const opId = optionalString(payload["op_id"]);
      const originConnId = optionalString(payload["origin_conn_id"]);
      const operations = asJSONPatchOperations(payload["operations"]);
      this.storageRequestedRooms.add(room);
      this.applyStorageMessage(room, document, {
        source: "remote",
        ...(kind ? { kind } : {}),
        ...(opId ? { opId } : {}),
        ...(originConnId ? { originConnId } : {}),
        ...(operations ? { operations } : {}),
      });
      return;
    }

    if (type === "ERROR") {
      const error = {
        code: asString(payload["code"]),
        message: asString(payload["message"]),
        ...(requestId ? { requestId } : {}),
      };
      if (pendingRequest?.t === "JOIN" && pendingRequest.room) {
        this.failRoomJoin(
          pendingRequest.room,
          `${asString(payload["code"])}: ${asString(payload["message"])}`.replace(/^: /, ""),
        );
      }
      if (requestId) {
        this.rejectStorageRequest(requestId, new Error(error.message || error.code || "OpenRTC storage request failed"));
      }
      this.emit("error", error);
    }
  }

  private completeRequest(id: string, now: number): number | undefined {
    const pending = this.pendingRequests.get(id);
    if (!pending) {
      return undefined;
    }
    this.pendingRequests.delete(id);
    return now - pending.sentAt;
  }

  private completeTrace(traceId: string, now: number): number | undefined {
    const pending = this.pendingTraces.get(traceId);
    if (!pending) {
      return undefined;
    }
    this.pendingTraces.delete(traceId);
    return now - pending.sentAt;
  }

  private cancelTrace(traceId: string): void {
    this.pendingTraces.delete(traceId);
  }

  private shiftPendingPresence(room: string): PendingPresenceSend | undefined {
    const queue = this.pendingPresenceByRoom.get(room);
    if (!queue || queue.length === 0) {
      return undefined;
    }
    const pending = queue.shift();
    if (queue.length === 0) {
      this.pendingPresenceByRoom.delete(room);
    }
    return pending;
  }

  private applyStorageMessage(
    room: string,
    document: unknown,
    options: Omit<OpenRTCStorageEvent, "room" | "document">,
  ): void {
    this.storageByRoom.set(room, document);
    if (options.source === "rollback") {
      this.setStorageStatus(room, "error");
    } else if (options.source !== "optimistic") {
      this.setStorageStatus(room, "synchronized");
    }
    this.emit("storage", {
      room,
      document,
      ...options,
    });
  }

  private hasPendingStorageGet(room: string): boolean {
    for (const pendingRoom of this.pendingStorageGets.values()) {
      if (pendingRoom === room) {
        return true;
      }
    }
    return false;
  }

  private resolveStorageGetWaiters(room: string, document: unknown): void {
    const waiters = this.storageGetWaitersByRoom.get(room);
    if (!waiters) {
      return;
    }
    this.storageGetWaitersByRoom.delete(room);
    for (const waiter of waiters) {
      waiter.resolve(document);
    }
  }

  private rejectStorageRequest(requestId: string, error: Error): void {
    const getRoom = this.pendingStorageGets.get(requestId);
    if (getRoom) {
      this.pendingStorageGets.delete(requestId);
      this.rejectStorageGetWaiters(getRoom, error);
      this.setStorageStatus(getRoom, "error");
      return;
    }

    const mutation = this.pendingStorageMutations.get(requestId);
    if (mutation) {
      this.pendingStorageMutations.delete(requestId);
      this.rollbackStorageMutation(mutation);
      mutation.reject(error);
    }
  }

  private rejectStorageGetWaiters(room: string, error: Error): void {
    const waiters = this.storageGetWaitersByRoom.get(room);
    if (!waiters) {
      return;
    }
    this.storageGetWaitersByRoom.delete(room);
    for (const waiter of waiters) {
      waiter.reject(error);
    }
  }

  private rejectPendingStorage(error: Error): void {
    for (const room of new Set([...this.pendingStorageGets.values(), ...this.storageGetWaitersByRoom.keys()])) {
      this.rejectStorageGetWaiters(room, error);
      this.setStorageStatus(room, "error");
    }
    this.pendingStorageGets.clear();

    for (const pending of this.pendingStorageMutations.values()) {
      this.rollbackStorageMutation(pending);
      pending.reject(error);
    }
    this.pendingStorageMutations.clear();
  }

  private rollbackStorageMutation(mutation: PendingStorageMutation): void {
    if (mutation.hadPreviousDocument) {
      this.applyStorageMessage(mutation.room, cloneStorageDocument(mutation.previousDocument), { source: "rollback" });
      return;
    }
    this.storageByRoom.delete(mutation.room);
    this.setStorageStatus(mutation.room, "error");
    this.emit("storage", {
      room: mutation.room,
      document: undefined,
      source: "rollback",
    });
  }

  private getOrCreatePresence(room: string): Map<string, PresenceState> {
    let presence = this.presenceByRoom.get(room);
    if (!presence) {
      presence = new Map();
      this.presenceByRoom.set(room, presence);
    }
    return presence;
  }

  private getOrCreateMembers(room: string): Set<string> {
    let members = this.membersByRoom.get(room);
    if (!members) {
      members = new Set();
      this.membersByRoom.set(room, members);
    }
    return members;
  }

  private getOrCreateRoomHandle(room: string): OpenRTCRoomHandle {
    let handle = this.roomHandles.get(room);
    if (!handle) {
      handle = new OpenRTCRoomHandle(this, room);
      this.roomHandles.set(room, handle);
    }
    return handle;
  }

  private setStatus(status: ConnectionStatus): void {
    this.statusValue = status;
    this.emit("status", status);
  }

  private setStorageStatus(room: string, status: OpenRTCStorageStatus): void {
    const current = this.getStorageStatus(room);
    if (current === status) {
      return;
    }
    if (status === "not-loaded") {
      this.storageStatusByRoom.delete(room);
    } else {
      this.storageStatusByRoom.set(room, status);
    }
    this.emit("storage-status", { room, status });
  }

  private resetRooms(options: { rooms?: Iterable<string>; preserveLocal: boolean }): void {
    const rooms = new Set<string>(
      options.rooms
        ? [...options.rooms]
        : [
            ...this.membersByRoom.keys(),
            ...this.presenceByRoom.keys(),
            ...this.localPresenceByRoom.keys(),
            ...this.nextCursorByRoom.keys(),
            ...this.storageByRoom.keys(),
            ...this.storageStatusByRoom.keys(),
            ...this.storageRequestedRooms.keys(),
          ],
    );
    for (const room of rooms) {
      this.membersByRoom.delete(room);
      this.presenceByRoom.delete(room);
      this.nextCursorByRoom.delete(room);
      if (!options.preserveLocal) {
        this.localPresenceByRoom.delete(room);
        this.storageByRoom.delete(room);
        this.storageRequestedRooms.delete(room);
        this.rejectStorageGetWaiters(room, new Error(`Room ${room} was left before storage request completed`));
        for (const [id, pendingRoom] of this.pendingStorageGets) {
          if (pendingRoom === room) {
            this.pendingStorageGets.delete(id);
          }
        }
        for (const [id, pending] of this.pendingStorageMutations) {
          if (pending.room === room) {
            pending.reject(new Error(`Room ${room} was left before storage request completed`));
            this.pendingStorageMutations.delete(id);
          }
        }
        this.setStorageStatus(room, "not-loaded");
      }
    }
    for (const room of rooms) {
      this.emit("room", { room, members: [], others: [] });
    }
  }

  private emit<K extends keyof OpenRTCEventMap>(type: K, event: OpenRTCEventMap[K]): void {
    const handlers = this.handlers.get(type);
    if (!handlers) {
      return;
    }
    for (const handler of handlers) {
      handler(event);
    }
  }

  private emitDiagnostic(event: OpenRTCDiagnosticEvent): void {
    this.emit("diagnostic", event);
  }
}

export class OpenRTCAdminError extends Error {
  constructor(
    readonly status: number,
    readonly body: unknown,
    message: string,
  ) {
    super(message);
    this.name = "OpenRTCAdminError";
  }
}

export class OpenRTCAdminClient {
  private readonly token: OpenRTCAdminClientOptions["token"];
  private readonly fetchImpl: OpenRTCFetch;
  private readonly baseURL: string;

  constructor(options: OpenRTCAdminClientOptions) {
    this.baseURL = options.url.replace(/\/+$/, "");
    this.token = options.token;
    const defaultFetch = globalThis.fetch as unknown as OpenRTCFetch | undefined;
    if (!options.fetch && !defaultFetch) {
      throw new Error("A fetch implementation is required in this environment");
    }
    this.fetchImpl = options.fetch ?? defaultFetch!;
  }

  publish(room: string, event: string, payload: unknown, options: OpenRTCAdminPublishOptions = {}): Promise<void> {
    return this.request<void>("/v1/publish", {
      method: "POST",
      body: {
        room,
        event,
        payload,
        ...(options.excludeSenderConnId ? { exclude_sender_conn_id: options.excludeSenderConnId } : {}),
        ...(options.traceId ? { trace_id: options.traceId } : {}),
      },
      empty: true,
      okStatuses: [202],
    });
  }

  setPresence(
    room: string,
    connId: string,
    state: PresenceState,
    options: OpenRTCAdminPresenceOptions = {},
  ): Promise<void> {
    return this.request<void>("/v1/presence", {
      method: "POST",
      body: {
        room,
        conn_id: connId,
        state,
        ...(options.ttlSeconds !== undefined ? { ttl_seconds: options.ttlSeconds } : {}),
      },
      empty: true,
      okStatuses: [202],
    });
  }

  listRooms(options: { prefix?: string; limit?: number; cursor?: string } = {}): Promise<OpenRTCAdminRoomList> {
    return this.request<OpenRTCAdminRoomList>(this.pathWithQuery("/v1/rooms", options));
  }

  createRoom(room: OpenRTCAdminRoomInput): Promise<OpenRTCAdminRoomRecord> {
    return this.request<OpenRTCAdminRoomRecord>("/v1/rooms", { method: "POST", body: room, okStatuses: [201] });
  }

  getRoom(room: string): Promise<OpenRTCAdminRoomRecord> {
    return this.request<OpenRTCAdminRoomRecord>(`/v1/rooms/${encodeURIComponent(room)}`);
  }

  updateRoom(room: string, update: OpenRTCAdminRoomUpdate): Promise<OpenRTCAdminRoomRecord> {
    return this.request<OpenRTCAdminRoomRecord>(`/v1/rooms/${encodeURIComponent(room)}`, { method: "PATCH", body: update });
  }

  async deleteRoom(room: string): Promise<void> {
    await this.request<void>(`/v1/rooms/${encodeURIComponent(room)}`, { method: "DELETE", empty: true, okStatuses: [204] });
  }

  activeUsers(room: string): Promise<OpenRTCListResponse<OpenRTCAdminActiveUser>> {
    return this.request<OpenRTCListResponse<OpenRTCAdminActiveUser>>(
      `/v1/rooms/${encodeURIComponent(room)}/active_users`,
    );
  }

  listThreads(room: string): Promise<OpenRTCListResponse<OpenRTCAdminThread>> {
    return this.request<OpenRTCListResponse<OpenRTCAdminThread>>(`/v1/rooms/${encodeURIComponent(room)}/threads`);
  }

  createThread(room: string, thread: OpenRTCAdminThreadInput): Promise<OpenRTCAdminThread> {
    return this.request<OpenRTCAdminThread>(`/v1/rooms/${encodeURIComponent(room)}/threads`, {
      method: "POST",
      body: thread,
      okStatuses: [201],
    });
  }

  addComment(room: string, threadId: string, comment: OpenRTCAdminCommentInput): Promise<OpenRTCAdminThread> {
    return this.request<OpenRTCAdminThread>(
      `/v1/rooms/${encodeURIComponent(room)}/threads/${encodeURIComponent(threadId)}/comments`,
      { method: "POST", body: comment, okStatuses: [201] },
    );
  }

  updateComment(
    room: string,
    threadId: string,
    commentId: string,
    update: OpenRTCAdminCommentUpdate,
  ): Promise<OpenRTCAdminThread> {
    return this.request<OpenRTCAdminThread>(
      `/v1/rooms/${encodeURIComponent(room)}/threads/${encodeURIComponent(threadId)}/comments/${encodeURIComponent(commentId)}`,
      { method: "PATCH", body: update },
    );
  }

  triggerInboxNotification(input: OpenRTCAdminInboxNotificationInput): Promise<OpenRTCAdminInboxNotification> {
    return this.request<OpenRTCAdminInboxNotification>("/v1/inbox-notifications/trigger", {
      method: "POST",
      body: input,
      okStatuses: [201],
    });
  }

  listInboxNotifications(
    userId: string,
    options: { limit?: number; cursor?: string; startingAfter?: string; unread?: boolean } = {},
  ): Promise<OpenRTCListResponse<OpenRTCAdminInboxNotification>> {
    return this.request<OpenRTCListResponse<OpenRTCAdminInboxNotification>>(
      this.pathWithQuery(`/v1/users/${encodeURIComponent(userId)}/inbox-notifications`, {
        limit: options.limit,
        cursor: options.cursor,
        startingAfter: options.startingAfter,
        ...(options.unread !== undefined ? { unread: String(options.unread) } : {}),
      }),
    );
  }

  getInboxNotification(userId: string, notificationId: string): Promise<OpenRTCAdminInboxNotification> {
    return this.request<OpenRTCAdminInboxNotification>(
      `/v1/users/${encodeURIComponent(userId)}/inbox-notifications/${encodeURIComponent(notificationId)}`,
    );
  }

  markInboxNotificationRead(notificationId: string): Promise<OpenRTCAdminInboxNotification> {
    return this.request<OpenRTCAdminInboxNotification>(
      `/v1/inbox-notifications/${encodeURIComponent(notificationId)}/read`,
      { method: "POST" },
    );
  }

  async deleteInboxNotification(userId: string, notificationId: string): Promise<void> {
    await this.request<void>(
      `/v1/users/${encodeURIComponent(userId)}/inbox-notifications/${encodeURIComponent(notificationId)}`,
      { method: "DELETE", empty: true, okStatuses: [204] },
    );
  }

  async deleteAllInboxNotifications(userId: string): Promise<void> {
    await this.request<void>(`/v1/users/${encodeURIComponent(userId)}/inbox-notifications`, {
      method: "DELETE",
      empty: true,
      okStatuses: [204],
    });
  }

  getNotificationSettings(userId: string): Promise<unknown> {
    return this.request<unknown>(`/v1/users/${encodeURIComponent(userId)}/notification-settings`);
  }

  setNotificationSettings(userId: string, settings: unknown): Promise<unknown> {
    return this.request<unknown>(`/v1/users/${encodeURIComponent(userId)}/notification-settings`, {
      method: "POST",
      body: settings,
    });
  }

  async deleteNotificationSettings(userId: string): Promise<void> {
    await this.request<void>(`/v1/users/${encodeURIComponent(userId)}/notification-settings`, {
      method: "DELETE",
      empty: true,
      okStatuses: [204],
    });
  }

  getRoomSubscriptionSettings(room: string, userId: string): Promise<OpenRTCAdminRoomSubscriptionSettings> {
    return this.request<OpenRTCAdminRoomSubscriptionSettings>(
      `/v1/rooms/${encodeURIComponent(room)}/users/${encodeURIComponent(userId)}/subscription-settings`,
    );
  }

  setRoomSubscriptionSettings(
    room: string,
    userId: string,
    settings: OpenRTCAdminRoomSubscriptionSettingsInput,
  ): Promise<OpenRTCAdminRoomSubscriptionSettings> {
    return this.request<OpenRTCAdminRoomSubscriptionSettings>(
      `/v1/rooms/${encodeURIComponent(room)}/users/${encodeURIComponent(userId)}/subscription-settings`,
      { method: "POST", body: settings },
    );
  }

  async deleteRoomSubscriptionSettings(room: string, userId: string): Promise<void> {
    await this.request<void>(
      `/v1/rooms/${encodeURIComponent(room)}/users/${encodeURIComponent(userId)}/subscription-settings`,
      { method: "DELETE", empty: true, okStatuses: [204] },
    );
  }

  listRoomSubscriptionSettings(
    userId: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<OpenRTCListResponse<OpenRTCAdminRoomSubscriptionSettings>> {
    return this.request<OpenRTCListResponse<OpenRTCAdminRoomSubscriptionSettings>>(
      this.pathWithQuery(`/v1/users/${encodeURIComponent(userId)}/room-subscription-settings`, options),
    );
  }

  stats(): Promise<unknown> {
    return this.request<unknown>("/v1/stats");
  }

  private async request<T>(
    path: string,
    options: { method?: string; body?: unknown; empty?: boolean; okStatuses?: number[] } = {},
  ): Promise<T> {
    const token = await this.resolveToken();
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    const init: { method: string; headers: Record<string, string>; body?: string } = {
      method: options.method ?? "GET",
      headers,
    };
    if (options.body !== undefined) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(options.body);
    }
    const response = await this.fetchImpl(this.baseURL + path, init);
    const text = await response.text();
    const body = text ? parseJSON(text) : undefined;
    const okStatuses = options.okStatuses ?? [200];
    if (!response.ok || !okStatuses.includes(response.status)) {
      throw new OpenRTCAdminError(response.status, body, errorMessage(response, body));
    }
    return (options.empty ? undefined : body) as T;
  }

  private async resolveToken(): Promise<string> {
    return typeof this.token === "function" ? this.token() : this.token;
  }

  private pathWithQuery(path: string, params: Record<string, string | number | boolean | undefined>): string {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") {
        query.set(key, String(value));
      }
    }
    const encoded = query.toString();
    return encoded ? `${path}?${encoded}` : path;
  }
}

class OpenRTCRoomHandle implements OpenRTCRoom {
  constructor(
    private readonly client: OpenRTCClient,
    readonly id: string,
  ) {}

  getStatus(): ConnectionStatus {
    return this.client.status;
  }

  getPresence(): OpenRTCRoomPresence {
    return this.client.getPresence(this.id);
  }

  getSelf(): PresencePeer | undefined {
    return this.client.getSelf(this.id);
  }

  getOthers(): PresencePeer[] {
    return this.client.getOthers(this.id);
  }

  getMyPresence(): PresenceState {
    return this.client.getMyPresence(this.id);
  }

  updatePresence(patch: PresenceState): string {
    return this.client.patchPresence(this.id, patch);
  }

  setPresence(state: PresenceState): string {
    return this.client.updatePresence(this.id, state);
  }

  setCursor(cursor: OpenRTCCursor | null, options: OpenRTCCursorOptions = {}): string {
    return this.client.setCursor(this.id, cursor, options);
  }

  clearCursor(): string {
    return this.client.clearCursor(this.id);
  }

  broadcastEvent(event: RoomBroadcastInput, payload?: unknown, options: BroadcastOptions = {}): string {
    const normalized = normalizeRoomEvent(event, payload);
    return this.client.broadcast(this.id, normalized.event, normalized.payload, options.traceId);
  }

  broadcastEventWithAck(event: RoomBroadcastInput, payload?: unknown, options: BroadcastOptions = {}): Promise<OpenRTCEvent> {
    const normalized = normalizeRoomEvent(event, payload);
    return this.client.broadcastWithAck(this.id, normalized.event, normalized.payload, options);
  }

  getStorage<TDocument = unknown>(): Promise<TDocument> {
    return this.client.getStorage<TDocument>(this.id);
  }

  getStorageSnapshot<TDocument = unknown>(): TDocument | undefined {
    return this.client.getStorageSnapshot<TDocument>(this.id);
  }

  getStorageStatus(): OpenRTCStorageStatus {
    return this.client.getStorageStatus(this.id);
  }

  setStorage<TDocument = unknown>(
    document: TDocument,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<TDocument> {
    return this.client.setStorage<TDocument>(this.id, document, options);
  }

  setLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    data: TData | OpenRTCLiveObject<TData>,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<OpenRTCLiveObject<TData>> {
    return this.client.setLiveStorage<TData>(this.id, data, options);
  }

  updateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    patch: Partial<TData>,
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<OpenRTCLiveObject<TData>> {
    return this.client.updateLiveStorage<TData>(this.id, patch, options);
  }

  patchStorage<TDocument = unknown>(
    operations: JSONPatchOperation[],
    options: OpenRTCStorageMutationOptions = {},
  ): Promise<TDocument> {
    return this.client.patchStorage<TDocument>(this.id, operations, options);
  }

  subscribe(type: "others", callback: (others: PresencePeer[], event: OpenRTCOthersEvent) => void): () => void;
  subscribe(type: "my-presence", callback: (presence: PresenceState) => void): () => void;
  subscribe(type: "event", callback: (event: OpenRTCEvent) => void): () => void;
  subscribe(type: "comments", callback: (event: OpenRTCCommentEvent) => void): () => void;
  subscribe(type: "storage", callback: (event: OpenRTCStorageEvent) => void): () => void;
  subscribe(type: "storage-status", callback: (status: OpenRTCStorageStatus) => void): () => void;
  subscribe(type: "status", callback: (status: ConnectionStatus) => void): () => void;
  subscribe(type: "error", callback: (error: OpenRTCError) => void): () => void;
  subscribe(type: "lost-connection", callback: (event: OpenRTCLostConnectionEvent) => void): () => void;
  subscribe(
    type:
      | "others"
      | "my-presence"
      | "event"
      | "comments"
      | "storage"
      | "storage-status"
      | "status"
      | "error"
      | "lost-connection",
    callback:
      | ((others: PresencePeer[], event: OpenRTCOthersEvent) => void)
      | ((presence: PresenceState) => void)
      | ((event: OpenRTCEvent) => void)
      | ((event: OpenRTCCommentEvent) => void)
      | ((event: OpenRTCStorageEvent) => void)
      | ((status: OpenRTCStorageStatus) => void)
      | ((status: ConnectionStatus) => void)
      | ((error: OpenRTCError) => void)
      | ((event: OpenRTCLostConnectionEvent) => void),
  ): () => void {
    if (type === "others") {
      return this.subscribeOthers(callback as (others: PresencePeer[], event: OpenRTCOthersEvent) => void);
    }
    if (type === "my-presence") {
      return this.subscribeMyPresence(callback as (presence: PresenceState) => void);
    }
    if (type === "event") {
      return this.client.on("event", (event) => {
        if (event.room === this.id) {
          (callback as (event: OpenRTCEvent) => void)(event);
        }
      });
    }
    if (type === "comments") {
      return this.client.on("comment", (event) => {
        if (event.room === this.id) {
          (callback as (event: OpenRTCCommentEvent) => void)(event);
        }
      });
    }
    if (type === "storage") {
      return this.client.on("storage", (event) => {
        if (event.room === this.id) {
          (callback as (event: OpenRTCStorageEvent) => void)(event);
        }
      });
    }
    if (type === "storage-status") {
      return this.client.on("storage-status", (event) => {
        if (event.room === this.id) {
          (callback as (status: OpenRTCStorageStatus) => void)(event.status);
        }
      });
    }
    if (type === "status") {
      return this.client.on("status", callback as (status: ConnectionStatus) => void);
    }
    if (type === "lost-connection") {
      return this.client.on("lost-connection", (event) => {
        if (event.room === this.id) {
          (callback as (event: OpenRTCLostConnectionEvent) => void)(event.event);
        }
      });
    }
    return this.client.on("error", callback as (error: OpenRTCError) => void);
  }

  leave(): void {
    this.client.leave(this.id);
  }

  reconnect(): Promise<void> {
    return this.client.reconnect();
  }

  private subscribeOthers(callback: (others: PresencePeer[], event: OpenRTCOthersEvent) => void): () => void {
    const offPresence = this.client.on("presence", (event) => {
      if (event.room !== this.id || event.connId === this.client.connId) {
        return;
      }
      const user = { connId: event.connId, state: event.state ?? {} };
      callback(
        this.client.getOthers(this.id),
        event.offline
          ? { type: "leave", user }
          : {
              type: event.entered ? "enter" : "update",
              user,
              updates: event.state ?? {},
            },
      );
    });
    const offRoom = this.client.on("room", (state) => {
      if (state.room === this.id) {
        callback(state.others, { type: "reset" });
      }
    });
    return () => {
      offPresence();
      offRoom();
    };
  }

  private subscribeMyPresence(callback: (presence: PresenceState) => void): () => void {
    const offPresence = this.client.on("presence", (event) => {
      if (event.room === this.id && event.connId === this.client.connId) {
        callback(this.client.getMyPresence(this.id));
      }
    });
    const offRoom = this.client.on("room", (state) => {
      if (state.room === this.id) {
        callback(this.client.getMyPresence(this.id));
      }
    });
    return () => {
      offPresence();
      offRoom();
    };
  }
}

export function liveObject<TData extends Record<string, unknown> = Record<string, unknown>>(
  data: TData = {} as TData,
): OpenRTCLiveObject<TData> {
  assertLiveRecordData(data, "LiveObject");
  return {
    liveblocksType: "LiveObject",
    data: cloneStorageDocument(data),
  };
}

export function liveList<TItem = unknown>(data: TItem[] = []): OpenRTCLiveList<TItem> {
  if (!Array.isArray(data)) {
    throw new Error("LiveList data must be an array");
  }
  return {
    liveblocksType: "LiveList",
    data: cloneStorageDocument(data),
  };
}

export function liveMap<TData extends Record<string, unknown> = Record<string, unknown>>(
  data: TData = {} as TData,
): OpenRTCLiveMap<TData> {
  assertLiveRecordData(data, "LiveMap");
  return {
    liveblocksType: "LiveMap",
    data: cloneStorageDocument(data),
  };
}

export function isLiveObject<TData extends Record<string, unknown> = Record<string, unknown>>(
  value: unknown,
): value is OpenRTCLiveObject<TData> {
  return isLiveStorageNode(value, "LiveObject") && isRecordObject(value.data);
}

export function isLiveList<TItem = unknown>(value: unknown): value is OpenRTCLiveList<TItem> {
  return isLiveStorageNode(value, "LiveList") && Array.isArray(value.data);
}

export function isLiveMap<TData extends Record<string, unknown> = Record<string, unknown>>(
  value: unknown,
): value is OpenRTCLiveMap<TData> {
  return isLiveStorageNode(value, "LiveMap") && isRecordObject(value.data);
}

export function isLiveStorageNode(value: unknown, type?: OpenRTCLiveStorageType): value is OpenRTCLiveStorageNodeValue {
  if (!isRecordObject(value)) {
    return false;
  }
  const liveblocksType = value["liveblocksType"];
  if (liveblocksType !== "LiveObject" && liveblocksType !== "LiveList" && liveblocksType !== "LiveMap") {
    return false;
  }
  return type === undefined || liveblocksType === type;
}

export function liveObjectPatch<TData extends Record<string, unknown>>(
  patch: Partial<TData>,
  options: { basePath?: string } = {},
): JSONPatchOperation[] {
  assertLiveRecordData(patch, "LiveObject patch");
  const entries = Object.entries(patch);
  if (entries.length === 0) {
    throw new Error("LiveObject patch must contain at least one field");
  }
  const basePath = options.basePath ?? "/data";
  return entries.map(([key, value]) => ({
    op: "add",
    path: joinJSONPointer(basePath, key),
    value,
  }));
}

export function isOpenRTCCursor(value: unknown): value is OpenRTCCursor {
  if (!isObject(value)) {
    return false;
  }
  const x = value["x"];
  const y = value["y"];
  return typeof x === "number" && Number.isFinite(x) && typeof y === "number" && Number.isFinite(y);
}

export function getPresenceCursor(presence: PresenceState | undefined, presenceKey = "cursor"): OpenRTCCursor | null {
  const cursor = presence?.[presenceKey];
  return isOpenRTCCursor(cursor) ? cursor : null;
}

export function getPresenceUser(presence: PresenceState | undefined): OpenRTCUserInfo | undefined {
  const user = presence?.["user"];
  return isObject(user) ? (user as OpenRTCUserInfo) : undefined;
}

export function getPresenceColor(presence: PresenceState | undefined): string | undefined {
  const color = presence?.["color"];
  if (typeof color === "string" && color !== "") {
    return color;
  }
  const userColor = getPresenceUser(presence)?.color;
  return typeof userColor === "string" && userColor !== "" ? userColor : undefined;
}

export function getCursorPeers(peers: PresencePeer[], presenceKey = "cursor"): OpenRTCCursorPeer[] {
  const out: OpenRTCCursorPeer[] = [];
  for (const peer of peers) {
    const cursor = getPresenceCursor(peer.state, presenceKey);
    if (!cursor) {
      continue;
    }
    const user = getPresenceUser(peer.state);
    const color = getPresenceColor(peer.state);
    const mode = typeof peer.state["mode"] === "string" ? peer.state["mode"] : cursor.mode;
    out.push({
      ...peer,
      cursor,
      ...(user ? { user } : {}),
      ...(color ? { color } : {}),
      ...(mode ? { mode } : {}),
    });
  }
  return out;
}

export function toWebSocketURL(rawURL: string): string {
  const parsed = new URL(rawURL);
  if (parsed.protocol === "http:") {
    parsed.protocol = "ws:";
  } else if (parsed.protocol === "https:") {
    parsed.protocol = "wss:";
  }
  return parsed.toString();
}

export function withToken(rawURL: string, token: string): string {
  const parsed = new URL(rawURL);
  parsed.searchParams.set("token", token);
  return parsed.toString();
}

function readMessageData(event: unknown): unknown {
  if (isObject(event) && "data" in event) {
    return event["data"];
  }
  return undefined;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isRecordObject(value: unknown): value is Record<string, unknown> {
  return isObject(value) && !Array.isArray(value);
}

function asRecord(value: unknown): Record<string, unknown> {
  return isObject(value) ? value : {};
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function asPresenceMap(value: unknown): Map<string, PresenceState> {
  const out = new Map<string, PresenceState>();
  if (!isObject(value)) {
    return out;
  }
  for (const [connId, state] of Object.entries(value)) {
    out.set(connId, asPresenceState(state));
  }
  return out;
}

function asPresenceState(value: unknown): PresenceState {
  if (typeof value === "string") {
    try {
      return asPresenceState(JSON.parse(value) as unknown);
    } catch {
      return {};
    }
  }
  return isObject(value) ? value : {};
}

function asStorageMutationKind(value: unknown): OpenRTCStorageMutationKind | undefined {
  return value === "set" || value === "patch" ? value : undefined;
}

function asOpenRTCCommentEvent(event: OpenRTCEvent): OpenRTCCommentEvent | undefined {
  const type = commentEventTypeForName(event.event);
  if (!type) {
    return undefined;
  }
  const payload = asRecord(event.payload);
  if (optionalString(payload["type"]) !== type) {
    return undefined;
  }
  if (!isRecordObject(payload["thread"])) {
    return undefined;
  }
  const threadObject = payload["thread"];
  const thread = threadObject as unknown as OpenRTCAdminThread;
  const threadId = optionalString(payload["threadId"]) ?? optionalString(threadObject["id"]);
  if (!threadId) {
    return undefined;
  }
  const roomId = optionalString(payload["roomId"]) ?? event.room;
  const rawComment = payload["comment"];
  const comment = isRecordObject(rawComment) ? (rawComment as unknown as OpenRTCAdminComment) : undefined;
  const commentId = optionalString(payload["commentId"]) ?? optionalString(comment?.id);
  if (type !== "thread-created" && !commentId) {
    return undefined;
  }
  return {
    room: event.room,
    event: event.event as OpenRTCCommentEventName,
    type,
    roomId,
    threadId,
    thread,
    ...(commentId ? { commentId } : {}),
    ...(comment ? { comment } : {}),
    ...(event.traceId ? { traceId: event.traceId } : {}),
  };
}

function commentEventTypeForName(eventName: string): OpenRTCCommentEventType | undefined {
  switch (eventName) {
    case OPENRTC_COMMENT_EVENTS.threadCreated:
      return "thread-created";
    case OPENRTC_COMMENT_EVENTS.commentCreated:
      return "comment-created";
    case OPENRTC_COMMENT_EVENTS.commentUpdated:
      return "comment-updated";
    default:
      return undefined;
  }
}

function asOpenRTCNotificationDelta(eventName: string, rawPayload: unknown): OpenRTCNotificationDelta | undefined {
  const type = notificationDeltaTypeForName(eventName);
  if (!type) {
    return undefined;
  }
  const payload = asRecord(rawPayload);
  if (optionalString(payload["type"]) !== type) {
    return undefined;
  }
  const userId = optionalString(payload["userId"]);
  if (!userId) {
    return undefined;
  }
  const rawNotification = payload["notification"];
  const notification = isRecordObject(rawNotification)
    ? (rawNotification as unknown as OpenRTCAdminInboxNotification)
    : undefined;
  const notificationId = optionalString(payload["notificationId"]) ?? optionalString(notification?.id);
  if (type !== "deleted-all" && !notificationId) {
    return undefined;
  }
  return {
    event: eventName as OpenRTCNotificationEventName,
    type,
    userId,
    ...(notificationId ? { notificationId } : {}),
    ...(notification ? { notification } : {}),
  };
}

function notificationDeltaTypeForName(eventName: string): OpenRTCNotificationDeltaType | undefined {
  switch (eventName) {
    case OPENRTC_NOTIFICATION_EVENTS.inboxCreated:
      return "created";
    case OPENRTC_NOTIFICATION_EVENTS.inboxRead:
      return "read";
    case OPENRTC_NOTIFICATION_EVENTS.inboxDeleted:
      return "deleted";
    case OPENRTC_NOTIFICATION_EVENTS.inboxDeletedAll:
      return "deleted-all";
    default:
      return undefined;
  }
}

function asJSONPatchOperations(value: unknown): JSONPatchOperation[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  const operations: JSONPatchOperation[] = [];
  for (const item of value) {
    if (!isObject(item)) {
      return undefined;
    }
    const op = item["op"];
    const path = item["path"];
    if (!isJSONPatchOperationType(op) || typeof path !== "string") {
      return undefined;
    }
    const from = item["from"];
    operations.push({
      op,
      path,
      ...(typeof from === "string" ? { from } : {}),
      ...("value" in item ? { value: item["value"] } : {}),
    });
  }
  return operations;
}

function isJSONPatchOperationType(value: unknown): value is JSONPatchOperationType {
  return (
    value === "add" ||
    value === "remove" ||
    value === "replace" ||
    value === "move" ||
    value === "copy" ||
    value === "test"
  );
}

function applyJSONPatchOptimistic(document: unknown, operations: JSONPatchOperation[]): unknown {
  let next = cloneStorageDocument(document);
  for (const operation of operations) {
    switch (operation.op) {
      case "add":
        assertPatchValue(operation);
        next = setPatchValue(next, operation.path, operation.value, "add");
        break;
      case "replace":
        assertPatchValue(operation);
        next = setPatchValue(next, operation.path, operation.value, "replace");
        break;
      case "remove":
        next = removePatchValue(next, operation.path).document;
        break;
      case "test":
        assertPatchValue(operation);
        if (!jsonEqual(getPatchValue(next, operation.path), operation.value)) {
          throw new Error(`Storage patch test failed at ${operation.path || "/"}`);
        }
        break;
      case "copy":
        assertPatchFrom(operation);
        next = setPatchValue(next, operation.path, cloneStorageDocument(getPatchValue(next, operation.from)), "add");
        break;
      case "move": {
        assertPatchFrom(operation);
        const value = cloneStorageDocument(getPatchValue(next, operation.from));
        next = removePatchValue(next, operation.from).document;
        next = setPatchValue(next, operation.path, value, "add");
        break;
      }
    }
  }
  return next;
}

function cloneStorageDocument<T>(value: T): T {
  if (value === undefined || value === null || typeof value !== "object") {
    return value;
  }
  if (typeof structuredClone === "function") {
    return structuredClone(value);
  }
  return JSON.parse(JSON.stringify(value)) as T;
}

function normalizeLiveObjectRoot<TData extends Record<string, unknown>>(
  data: TData | OpenRTCLiveObject<TData>,
): OpenRTCLiveObject<TData> {
  if (isLiveObject<TData>(data)) {
    return liveObject(data.data);
  }
  return liveObject(data);
}

function assertLiveRecordData(value: unknown, typeName: string): asserts value is Record<string, unknown> {
  if (!isRecordObject(value)) {
    throw new Error(`${typeName} data must be a JSON object`);
  }
}

function assertPatchValue(operation: JSONPatchOperation): void {
  if (!("value" in operation)) {
    throw new Error(`Storage patch ${operation.op} operation requires value`);
  }
}

function assertPatchFrom(operation: JSONPatchOperation): asserts operation is JSONPatchOperation & { from: string } {
  if (typeof operation.from !== "string") {
    throw new Error(`Storage patch ${operation.op} operation requires from`);
  }
}

function getPatchValue(document: unknown, path: string): unknown {
  const parts = parseJSONPointer(path);
  let current = document;
  for (const part of parts) {
    if (Array.isArray(current)) {
      current = current[parsePatchArrayIndex(part, current.length, false)];
      continue;
    }
    if (isObject(current) && Object.prototype.hasOwnProperty.call(current, part)) {
      current = current[part];
      continue;
    }
    throw new Error(`Storage patch path does not exist: ${path || "/"}`);
  }
  return current;
}

function setPatchValue(document: unknown, path: string, value: unknown, mode: "add" | "replace"): unknown {
  const parts = parseJSONPointer(path);
  const nextValue = cloneStorageDocument(value);
  if (parts.length === 0) {
    return nextValue;
  }
  const { container, key } = getPatchParent(document, parts);
  if (Array.isArray(container)) {
    const index = key === "-" && mode === "add" ? container.length : parsePatchArrayIndex(key, container.length, mode === "add");
    if (mode === "add") {
      container.splice(index, 0, nextValue);
    } else {
      container[index] = nextValue;
    }
    return document;
  }
  if (mode === "replace" && !Object.prototype.hasOwnProperty.call(container, key)) {
    throw new Error(`Storage patch path does not exist: ${path}`);
  }
  container[key] = nextValue;
  return document;
}

function removePatchValue(document: unknown, path: string): { document: unknown; value: unknown } {
  const parts = parseJSONPointer(path);
  if (parts.length === 0) {
    throw new Error("Removing the storage root is not supported");
  }
  const { container, key } = getPatchParent(document, parts);
  if (Array.isArray(container)) {
    const index = parsePatchArrayIndex(key, container.length, false);
    const [value] = container.splice(index, 1);
    return { document, value };
  }
  if (!Object.prototype.hasOwnProperty.call(container, key)) {
    throw new Error(`Storage patch path does not exist: ${path}`);
  }
  const value = container[key];
  delete container[key];
  return { document, value };
}

function getPatchParent(document: unknown, parts: string[]): { container: Record<string, unknown> | unknown[]; key: string } {
  let current = document;
  for (const part of parts.slice(0, -1)) {
    current = getPatchValue(current, `/${escapeJSONPointer(part)}`);
  }
  if (!isObject(current) && !Array.isArray(current)) {
    throw new Error(`Storage patch parent is not a container: /${parts.slice(0, -1).map(escapeJSONPointer).join("/")}`);
  }
  return { container: current as Record<string, unknown> | unknown[], key: parts[parts.length - 1] ?? "" };
}

function parseJSONPointer(path: string): string[] {
  if (path === "") {
    return [];
  }
  if (!path.startsWith("/")) {
    throw new Error(`Storage patch path must be a JSON Pointer: ${path}`);
  }
  return path
    .slice(1)
    .split("/")
    .map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
}

function escapeJSONPointer(part: string): string {
  return part.replace(/~/g, "~0").replace(/\//g, "~1");
}

function joinJSONPointer(basePath: string, part: string): string {
  const normalizedBase = basePath === "" || basePath === "/" ? "" : basePath.replace(/\/+$/, "");
  if (normalizedBase && !normalizedBase.startsWith("/")) {
    throw new Error(`JSON Pointer base path must start with /: ${basePath}`);
  }
  return `${normalizedBase}/${escapeJSONPointer(part)}`;
}

function parsePatchArrayIndex(part: string, length: number, allowEnd: boolean): number {
  if (!/^(0|[1-9]\d*)$/.test(part)) {
    throw new Error(`Storage patch array index is invalid: ${part}`);
  }
  const index = Number(part);
  const max = allowEnd ? length : length - 1;
  if (index < 0 || index > max) {
    throw new Error(`Storage patch array index is out of bounds: ${part}`);
  }
  return index;
}

function jsonEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function normalizeRoomEvent(event: RoomBroadcastInput, payload: unknown): { event: string; payload: unknown } {
  if (typeof event === "string") {
    return { event, payload: payload ?? {} };
  }
  return { event: event.type, payload: event };
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

function errorMessage(response: OpenRTCFetchResponse, body: unknown): string {
  if (isObject(body)) {
    const message = optionalString(body["message"]);
    const code = optionalString(body["code"]);
    if (message && code) {
      return `${code}: ${message}`;
    }
    if (message) {
      return message;
    }
  }
  return `${response.status} ${response.statusText}`.trim();
}

function normalizeLostConnectionTimeout(value: number | undefined): number {
  if (value === undefined || !Number.isFinite(value)) {
    return DEFAULT_LOST_CONNECTION_TIMEOUT_MS;
  }
  return Math.min(MAX_LOST_CONNECTION_TIMEOUT_MS, Math.max(MIN_LOST_CONNECTION_TIMEOUT_MS, Math.floor(value)));
}

function normalizeBackgroundKeepAliveTimeout(value: number | undefined): number | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (!Number.isFinite(value) || value <= 0) {
    return undefined;
  }
  return Math.floor(value);
}

function normalizeReconnectOptions(options: OpenRTCReconnectOptions | undefined): NormalizedReconnectOptions {
  const initialDelayMs = positiveInteger(options?.initialDelayMs, 250);
  const maxDelayMs = Math.max(initialDelayMs, positiveInteger(options?.maxDelayMs, 5000));
  const maxAttempts = options?.maxAttempts === undefined ? Number.POSITIVE_INFINITY : positiveInteger(options.maxAttempts, 1);
  const jitterRatio = Math.min(1, Math.max(0, options?.jitterRatio ?? 0.2));
  return { initialDelayMs, maxDelayMs, maxAttempts, jitterRatio };
}

function positiveInteger(value: number | undefined, fallback: number): number {
  if (value === undefined || !Number.isFinite(value) || value <= 0) {
    return fallback;
  }
  return Math.floor(value);
}

function reconnectDelayMs(attempt: number, options: NormalizedReconnectOptions): number {
  const exponential = options.initialDelayMs * 2 ** Math.max(0, attempt - 1);
  const capped = Math.min(options.maxDelayMs, exponential);
  if (options.jitterRatio === 0) {
    return capped;
  }
  const jitter = capped * options.jitterRatio;
  return Math.max(0, Math.floor(capped - jitter + Math.random() * jitter * 2));
}

function compactJoinOptions(options: JoinOptions): JoinOptions {
  return {
    ...(options.limit !== undefined ? { limit: options.limit } : {}),
    ...(options.cursor !== undefined ? { cursor: options.cursor } : {}),
  };
}

function getBrowserDocument(): OpenRTCBrowserDocument | undefined {
  const candidate = (globalThis as { document?: unknown }).document;
  if (!isObject(candidate)) {
    return undefined;
  }
  if (typeof candidate["addEventListener"] !== "function" || typeof candidate["removeEventListener"] !== "function") {
    return undefined;
  }
  return candidate as unknown as OpenRTCBrowserDocument;
}

function byteLength(value: string): number {
  if (typeof TextEncoder !== "undefined") {
    return new TextEncoder().encode(value).length;
  }
  return value.length;
}
