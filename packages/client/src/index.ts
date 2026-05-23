export type ConnectionStatus = "idle" | "connecting" | "open" | "closed" | "error";

export type JSONValue =
  | null
  | boolean
  | number
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };

export type PresenceState = Record<string, unknown>;

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
}

export interface JoinOptions {
  limit?: number;
  cursor?: string;
}

export interface PresencePeer {
  connId: string;
  state: PresenceState;
}

export interface OpenRTCEvent {
  room: string;
  event: string;
  payload: unknown;
  traceId?: string;
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

export interface OpenRTCPresenceUpdate {
  room: string;
  connId: string;
  offline: boolean;
  others: PresencePeer[];
  state?: PresenceState;
}

export interface OpenRTCEventMap {
  status: ConnectionStatus;
  hello: { connId: string; nodeId?: string };
  room: OpenRTCRoomState;
  presence: OpenRTCPresenceUpdate;
  event: OpenRTCEvent;
  error: OpenRTCError;
  message: unknown;
}

type Handler<T> = (event: T) => void;

const WS_OPEN = 1;

export class OpenRTCClient {
  readonly url: string;

  private readonly token: OpenRTCClientOptions["token"];
  private readonly WebSocketCtor: OpenRTCWebSocketConstructor;
  private socket: OpenRTCWebSocket | undefined;
  private requestCounter = 0;
  private statusValue: ConnectionStatus = "idle";
  private connIdValue: string | undefined;
  private handlers = new Map<keyof OpenRTCEventMap, Set<Handler<OpenRTCEventMap[keyof OpenRTCEventMap]>>>();
  private presenceByRoom = new Map<string, Map<string, PresenceState>>();
  private membersByRoom = new Map<string, Set<string>>();
  private localPresenceByRoom = new Map<string, PresenceState>();

  constructor(options: OpenRTCClientOptions) {
    this.url = options.url;
    this.token = options.token;
    const defaultCtor = globalThis.WebSocket as unknown as OpenRTCWebSocketConstructor | undefined;
    if (!options.WebSocket && !defaultCtor) {
      throw new Error("A WebSocket constructor is required in this environment");
    }
    this.WebSocketCtor = options.WebSocket ?? defaultCtor!;
  }

  get status(): ConnectionStatus {
    return this.statusValue;
  }

  get connId(): string | undefined {
    return this.connIdValue;
  }

  async connect(): Promise<void> {
    if (this.statusValue === "open" || this.statusValue === "connecting") {
      return;
    }
    this.setStatus("connecting");

    const token = await this.resolveToken();
    const socket = new this.WebSocketCtor(withToken(toWebSocketURL(this.url), token));
    this.socket = socket;

    await new Promise<void>((resolve, reject) => {
      const onOpen = (): void => {
        cleanup();
        this.setStatus("open");
        resolve();
      };
      const onError = (event: unknown): void => {
        cleanup();
        this.setStatus("error");
        reject(event instanceof Error ? event : new Error("WebSocket connection failed"));
      };
      const cleanup = (): void => {
        socket.removeEventListener("open", onOpen as (event: unknown) => void);
        socket.removeEventListener("error", onError as (event: unknown) => void);
      };
      socket.addEventListener("open", onOpen as (event: unknown) => void);
      socket.addEventListener("error", onError as (event: unknown) => void);
    });

    socket.addEventListener("message", (event) => {
      const data = readMessageData(event);
      if (typeof data === "string") {
        this.handleMessage(JSON.parse(data) as unknown);
      }
    });
    socket.addEventListener("close", () => {
      this.socket = undefined;
      this.setStatus("closed");
    });
    socket.addEventListener("error", () => {
      this.setStatus("error");
    });
  }

  close(): void {
    this.socket?.close();
    this.socket = undefined;
    this.setStatus("closed");
  }

  join(room: string, options: JoinOptions = {}): string {
    const id = this.nextID("join");
    const meta: Record<string, unknown> = {};
    if (options.limit !== undefined) {
      meta["limit"] = options.limit;
    }
    if (options.cursor !== undefined) {
      meta["cursor"] = options.cursor;
    }
    this.send({
      t: "JOIN",
      id,
      room,
      ...(Object.keys(meta).length > 0 ? { meta } : {}),
    });
    return id;
  }

  leave(room: string): string {
    const id = this.nextID("leave");
    this.send({ t: "LEAVE", id, room });
    return id;
  }

  broadcast(room: string, event: string, payload: unknown, traceId?: string): string {
    const id = this.nextID("emit");
    this.send({
      t: "EMIT",
      id,
      room,
      event,
      payload,
      ...(traceId ? { meta: { trace_id: traceId } } : {}),
    });
    return id;
  }

  updatePresence(room: string, state: PresenceState): string {
    this.localPresenceByRoom.set(room, state);
    const id = this.nextID("presence");
    this.send({ t: "PRESENCE_SET", id, room, payload: state });
    return id;
  }

  patchPresence(room: string, patch: PresenceState): string {
    const current = this.localPresenceByRoom.get(room) ?? {};
    return this.updatePresence(room, { ...current, ...patch });
  }

  getRoomState(room: string): OpenRTCRoomState {
    return {
      room,
      members: [...(this.membersByRoom.get(room) ?? new Set<string>())],
      others: this.getOthers(room),
    };
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

  private async resolveToken(): Promise<string> {
    return typeof this.token === "function" ? this.token() : this.token;
  }

  private send(payload: unknown): void {
    if (!this.socket || this.socket.readyState !== WS_OPEN) {
      throw new Error("OpenRTC socket is not open");
    }
    this.socket.send(JSON.stringify(payload));
  }

  private nextID(prefix: string): string {
    this.requestCounter += 1;
    return `${prefix}-${this.requestCounter}`;
  }

  private handleMessage(message: unknown): void {
    this.emit("message", message);
    if (!isObject(message)) {
      return;
    }

    const type = asString(message["t"]);
    if (type === "HELLO") {
      const payload = asRecord(message["payload"]);
      const connId = asString(payload["conn_id"]);
      this.connIdValue = connId;
      const server = asRecord(payload["server"]);
      const nodeId = optionalString(server["node_id"]);
      this.emit("hello", {
        connId,
        ...(nodeId ? { nodeId } : {}),
      });
      return;
    }

    if (type === "JOINED") {
      const room = asString(message["room"]);
      const payload = asRecord(message["payload"]);
      const members = asStringArray(payload["members"]);
      const presence = asPresenceMap(payload["presence"]);
      this.membersByRoom.set(room, new Set(members));
      this.presenceByRoom.set(room, presence);
      const nextCursor = optionalString(payload["next_cursor"]);
      this.emit("room", {
        room,
        members,
        others: this.getOthers(room),
        ...(nextCursor ? { nextCursor } : {}),
      });
      return;
    }

    if (type === "LEFT") {
      const room = asString(message["room"]);
      this.membersByRoom.delete(room);
      this.presenceByRoom.delete(room);
      this.emit("room", { room, members: [], others: [] });
      return;
    }

    if (type === "PRESENCE") {
      const room = asString(message["room"]);
      const payload = asRecord(message["payload"]);
      const connId = asString(payload["conn_id"]);
      const offline = payload["offline"] === true;
      const roomPresence = this.getOrCreatePresence(room);
      const members = this.getOrCreateMembers(room);
      if (offline) {
        roomPresence.delete(connId);
        members.delete(connId);
        this.emit("presence", { room, connId, offline, others: this.getOthers(room) });
      } else {
        const state = asPresenceState(payload["state"]);
        roomPresence.set(connId, state);
        members.add(connId);
        this.emit("presence", { room, connId, offline, state, others: this.getOthers(room) });
      }
      return;
    }

    if (type === "EVENT") {
      const meta = asRecord(message["meta"]);
      const traceId = optionalString(meta["trace_id"]);
      this.emit("event", {
        room: asString(message["room"]),
        event: asString(message["event"]),
        payload: message["payload"],
        ...(traceId ? { traceId } : {}),
      });
      return;
    }

    if (type === "ERROR") {
      const payload = asRecord(message["payload"]);
      const requestId = optionalString(payload["request_id"]);
      this.emit("error", {
        code: asString(payload["code"]),
        message: asString(payload["message"]),
        ...(requestId ? { requestId } : {}),
      });
    }
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

  private setStatus(status: ConnectionStatus): void {
    this.statusValue = status;
    this.emit("status", status);
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
