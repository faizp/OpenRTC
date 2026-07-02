import * as Y from "yjs";
import type { PresencePeer, PresenceState } from "@openrtc/client";

export interface YJSWebSocket {
  binaryType?: "arraybuffer" | "blob";
  readonly readyState: number;
  send(data: ArrayBufferLike | ArrayBufferView): void;
  close(code?: number, reason?: string): void;
  addEventListener(type: string, listener: (event: unknown) => void): void;
  removeEventListener(type: string, listener: (event: unknown) => void): void;
}

export type YJSWebSocketConstructor = new (url: string) => YJSWebSocket;

export interface OpenRTCYjsProviderOptions {
  url: string;
  room: string;
  token: string | (() => string | Promise<string>);
  doc: Y.Doc;
  WebSocket?: YJSWebSocketConstructor;
  snapshotIntervalMs?: number;
  stateVectorSync?: boolean;
  stateVectorSyncDelayMs?: number;
  connect?: boolean;
  awareness?: OpenRTCAwareness;
  presenceClient?: OpenRTCAwarenessPresenceClient;
  awarenessOptions?: OpenRTCAwarenessBridgeOptions;
}

export type YjsConnectionStatus = "idle" | "connecting" | "open" | "closed" | "error";
export type YjsSyncStatus = "idle" | "connecting" | "syncing" | "synced" | "closed" | "error";
export type YjsSyncedFrameKind = "update" | "snapshot" | "state-vector-diff";

export interface OpenRTCYjsSyncState {
  status: YjsSyncStatus;
  stateVectorHash: string;
  snapshotHash?: string;
  lastSyncedAt?: number;
  receivedBytes: number;
  sentBytes: number;
  updatesReceived: number;
  snapshotsReceived: number;
  diffsReceived: number;
}

export interface OpenRTCYjsProviderEventMap {
  status: YjsConnectionStatus;
  "sync-status": OpenRTCYjsSyncState;
  synced: { kind: YjsSyncedFrameKind; bytes: number; stateVectorHash: string; snapshotHash?: string };
  error: Error;
}

type Handler<T> = (event: T) => void;

const WS_OPEN = 1;
const FRAME_UPDATE = 1;
const FRAME_SNAPSHOT = 2;
const FRAME_STATE_VECTOR = 3;
const FRAME_STATE_VECTOR_DIFF = 4;
const DEFAULT_STATE_VECTOR_SYNC_DELAY_MS = 25;
export const OPENRTC_AWARENESS_PRESENCE_KEY = "__openrtc_yjs_awareness";

export class OpenRTCYjsProvider {
  readonly doc: Y.Doc;
  readonly room: string;
  readonly awareness: OpenRTCAwareness;

  private readonly url: string;
  private readonly token: OpenRTCYjsProviderOptions["token"];
  private readonly WebSocketCtor: YJSWebSocketConstructor;
  private readonly snapshotIntervalMs: number | undefined;
  private readonly stateVectorSync: boolean;
  private readonly stateVectorSyncDelayMs: number;
  private readonly ownsAwareness: boolean;
  private socket: YJSWebSocket | undefined;
  private statusValue: YjsConnectionStatus = "idle";
  private syncStatusValue: YjsSyncStatus = "idle";
  private snapshotTimer: ReturnType<typeof setInterval> | undefined;
  private stateVectorTimer: ReturnType<typeof setTimeout> | undefined;
  private awarenessBridge: OpenRTCAwarenessBridge | undefined;
  private snapshotHash: string | undefined;
  private lastSyncedAt: number | undefined;
  private receivedBytes = 0;
  private sentBytes = 0;
  private updatesReceived = 0;
  private snapshotsReceived = 0;
  private diffsReceived = 0;
  private handlers = new Map<keyof OpenRTCYjsProviderEventMap, Set<Handler<OpenRTCYjsProviderEventMap[keyof OpenRTCYjsProviderEventMap]>>>();

  constructor(options: OpenRTCYjsProviderOptions) {
    this.url = options.url;
    this.room = options.room;
    this.token = options.token;
    this.doc = options.doc;
    this.snapshotIntervalMs = options.snapshotIntervalMs;
    this.stateVectorSync = options.stateVectorSync ?? true;
    this.stateVectorSyncDelayMs = options.stateVectorSyncDelayMs ?? DEFAULT_STATE_VECTOR_SYNC_DELAY_MS;
    this.awareness = options.awareness ?? new OpenRTCAwareness(options.doc);
    this.ownsAwareness = !options.awareness;
    const defaultCtor = globalThis.WebSocket as unknown as YJSWebSocketConstructor | undefined;
    if (!options.WebSocket && !defaultCtor) {
      throw new Error("A WebSocket constructor is required in this environment");
    }
    this.WebSocketCtor = options.WebSocket ?? defaultCtor!;
    this.doc.on("update", this.handleLocalUpdate);
    if (options.presenceClient) {
      this.awarenessBridge = bindOpenRTCAwareness(
        options.presenceClient,
        this.room,
        this.awareness,
        options.awarenessOptions,
      );
    }

    if (options.connect !== false) {
      void this.connect();
    }
  }

  get status(): YjsConnectionStatus {
    return this.statusValue;
  }

  getSyncState(): OpenRTCYjsSyncState {
    return this.currentSyncState();
  }

  async connect(): Promise<void> {
    if (this.statusValue === "open" || this.statusValue === "connecting") {
      return;
    }
    this.setStatus("connecting");
    this.setSyncStatus("connecting");

    const token = await this.resolveToken();
    const socket = new this.WebSocketCtor(yjsWebSocketURL(this.url, this.room, token));
    socket.binaryType = "arraybuffer";
    this.socket = socket;

    socket.addEventListener("message", (event) => {
      void this.handleRemoteFrame(readMessageData(event));
    });
    socket.addEventListener("close", () => {
      this.stopSnapshotTimer();
      this.socket = undefined;
      this.setStatus("closed");
      this.setSyncStatus("closed");
    });
    socket.addEventListener("error", () => {
      this.setStatus("error");
      this.setSyncStatus("error");
      this.emit("error", new Error("Yjs WebSocket error"));
    });

    await new Promise<void>((resolve, reject) => {
      const onOpen = (): void => {
        cleanup();
        this.setStatus("open");
        this.setSyncStatus("syncing");
        this.startSnapshotTimer();
        this.scheduleStateVectorSync();
        resolve();
      };
      const onError = (): void => {
        cleanup();
        this.setStatus("error");
        this.setSyncStatus("error");
        reject(new Error("Yjs WebSocket connection failed"));
      };
      const cleanup = (): void => {
        socket.removeEventListener("open", onOpen as (event: unknown) => void);
        socket.removeEventListener("error", onError as (event: unknown) => void);
      };
      socket.addEventListener("open", onOpen as (event: unknown) => void);
      socket.addEventListener("error", onError as (event: unknown) => void);
    });
  }

  disconnect(): void {
    this.stopSnapshotTimer();
    this.stopStateVectorTimer();
    this.socket?.close();
    this.socket = undefined;
    this.setStatus("closed");
    this.setSyncStatus("closed");
  }

  destroy(): void {
    this.disconnect();
    this.doc.off("update", this.handleLocalUpdate);
    if (this.ownsAwareness) {
      this.awareness.setLocalState(null);
      this.awarenessBridge?.flush();
    }
    this.awarenessBridge?.dispose();
    if (this.ownsAwareness) {
      this.awareness.destroy();
    }
    this.handlers.clear();
  }

  sendSnapshot(): void {
    this.sendFrame(FRAME_UPDATE, Y.encodeStateAsUpdate(this.doc));
  }

  requestSync(): boolean {
    const sent = this.sendFrame(FRAME_STATE_VECTOR, Y.encodeStateVector(this.doc));
    if (sent) {
      this.setSyncStatus("syncing");
    }
    return sent;
  }

  on<K extends keyof OpenRTCYjsProviderEventMap>(
    type: K,
    handler: Handler<OpenRTCYjsProviderEventMap[K]>,
  ): () => void {
    let handlers = this.handlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.handlers.set(type, handlers);
    }
    handlers.add(handler as Handler<OpenRTCYjsProviderEventMap[keyof OpenRTCYjsProviderEventMap]>);
    return () => {
      handlers?.delete(handler as Handler<OpenRTCYjsProviderEventMap[keyof OpenRTCYjsProviderEventMap]>);
    };
  }

  private handleLocalUpdate = (update: Uint8Array, origin: unknown): void => {
    if (origin === this) {
      return;
    }
    this.sendFrame(FRAME_UPDATE, update);
  };

  private async handleRemoteFrame(data: unknown): Promise<void> {
    const frame = await toUint8Array(data);
    if (frame.length < 1) {
      return;
    }
    const kind = frame[0]!;
    const payload = frame.subarray(1);
    if (kind === FRAME_STATE_VECTOR) {
      this.handleStateVectorRequest(payload);
      return;
    }
    if (kind !== FRAME_UPDATE && kind !== FRAME_SNAPSHOT && kind !== FRAME_STATE_VECTOR_DIFF) {
      return;
    }
    Y.applyUpdate(this.doc, payload, this);
    this.recordRemoteSyncFrame(kind, payload);
  }

  private handleStateVectorRequest(stateVector: Uint8Array): void {
    try {
      this.sendFrame(FRAME_STATE_VECTOR_DIFF, Y.encodeStateAsUpdate(this.doc, stateVector));
    } catch (error) {
      this.emit("error", error instanceof Error ? error : new Error("Yjs state vector sync failed"));
    }
  }

  private recordRemoteSyncFrame(kind: number, update: Uint8Array): void {
    this.receivedBytes += update.byteLength;
    if (kind === FRAME_SNAPSHOT) {
      this.snapshotsReceived++;
      this.snapshotHash = hashBytes(update);
    } else if (kind === FRAME_STATE_VECTOR_DIFF) {
      this.diffsReceived++;
    } else {
      this.updatesReceived++;
    }
    this.lastSyncedAt = Date.now();
    this.setSyncStatus("synced");
    this.emit("synced", {
      kind: syncedKind(kind),
      bytes: update.byteLength,
      stateVectorHash: this.stateVectorHash(),
      ...(this.snapshotHash ? { snapshotHash: this.snapshotHash } : {}),
    });
  }

  private sendFrame(kind: number, update: Uint8Array): boolean {
    if (!this.socket || this.socket.readyState !== WS_OPEN) {
      return false;
    }
    const frame = new Uint8Array(1 + update.byteLength);
    frame[0] = kind;
    frame.set(update, 1);
    this.socket.send(frame);
    this.sentBytes += update.byteLength;
    this.emitSyncStatus();
    return true;
  }

  private startSnapshotTimer(): void {
    if (!this.snapshotIntervalMs || this.snapshotTimer) {
      return;
    }
    this.snapshotTimer = setInterval(() => this.sendSnapshot(), this.snapshotIntervalMs);
  }

  private stopSnapshotTimer(): void {
    if (this.snapshotTimer) {
      clearInterval(this.snapshotTimer);
      this.snapshotTimer = undefined;
    }
  }

  private scheduleStateVectorSync(): void {
    if (!this.stateVectorSync || this.stateVectorTimer) {
      return;
    }
    this.stateVectorTimer = setTimeout(() => {
      this.stateVectorTimer = undefined;
      this.requestSync();
    }, Math.max(0, this.stateVectorSyncDelayMs));
  }

  private stopStateVectorTimer(): void {
    if (this.stateVectorTimer) {
      clearTimeout(this.stateVectorTimer);
      this.stateVectorTimer = undefined;
    }
  }

  private async resolveToken(): Promise<string> {
    return typeof this.token === "function" ? this.token() : this.token;
  }

  private setStatus(status: YjsConnectionStatus): void {
    this.statusValue = status;
    this.emit("status", status);
  }

  private setSyncStatus(status: YjsSyncStatus): void {
    this.syncStatusValue = status;
    this.emitSyncStatus();
  }

  private emitSyncStatus(): void {
    this.emit("sync-status", this.currentSyncState());
  }

  private currentSyncState(): OpenRTCYjsSyncState {
    return {
      status: this.syncStatusValue,
      stateVectorHash: this.stateVectorHash(),
      ...(this.snapshotHash ? { snapshotHash: this.snapshotHash } : {}),
      ...(this.lastSyncedAt !== undefined ? { lastSyncedAt: this.lastSyncedAt } : {}),
      receivedBytes: this.receivedBytes,
      sentBytes: this.sentBytes,
      updatesReceived: this.updatesReceived,
      snapshotsReceived: this.snapshotsReceived,
      diffsReceived: this.diffsReceived,
    };
  }

  private stateVectorHash(): string {
    return hashBytes(Y.encodeStateVector(this.doc));
  }

  private emit<K extends keyof OpenRTCYjsProviderEventMap>(type: K, event: OpenRTCYjsProviderEventMap[K]): void {
    const handlers = this.handlers.get(type);
    if (!handlers) {
      return;
    }
    for (const handler of handlers) {
      handler(event);
    }
  }
}

export type AwarenessState = Record<string, unknown>;

export interface AwarenessChange {
  added: number[];
  updated: number[];
  removed: number[];
}

export type AwarenessEventType = "change" | "update";
export type AwarenessEventHandler = (change: AwarenessChange, origin: unknown) => void;

export class OpenRTCAwareness {
  readonly clientID: number;
  readonly states = new Map<number, AwarenessState>();

  private readonly handlers = new Map<AwarenessEventType, Set<AwarenessEventHandler>>();

  constructor(doc: Pick<Y.Doc, "clientID">) {
    this.clientID = doc.clientID;
  }

  getLocalState(): AwarenessState | null {
    return this.states.get(this.clientID) ?? null;
  }

  getStates(): Map<number, AwarenessState> {
    return this.states;
  }

  setLocalState(state: AwarenessState | null): void {
    this.setState(this.clientID, state, "local");
  }

  setLocalStateField(field: string, value: unknown): void {
    this.setLocalState({
      ...(this.getLocalState() ?? {}),
      [field]: value,
    });
  }

  applyRemoteState(clientID: number, state: AwarenessState | null, origin: unknown): void {
    if (clientID === this.clientID) {
      return;
    }
    this.setState(clientID, state, origin);
  }

  on(type: AwarenessEventType, handler: AwarenessEventHandler): void {
    let handlers = this.handlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.handlers.set(type, handlers);
    }
    handlers.add(handler);
  }

  off(type: AwarenessEventType, handler: AwarenessEventHandler): void {
    this.handlers.get(type)?.delete(handler);
  }

  destroy(): void {
    this.setLocalState(null);
    this.handlers.clear();
  }

  private setState(clientID: number, state: AwarenessState | null, origin: unknown): void {
    const hadState = this.states.has(clientID);
    const current = this.states.get(clientID);
    if (state === null) {
      if (!hadState) {
        return;
      }
      this.states.delete(clientID);
      this.emit({ added: [], updated: [], removed: [clientID] }, origin);
      return;
    }
    if (hadState && awarenessStateEqual(current, state)) {
      return;
    }
    this.states.set(clientID, state);
    this.emit({
      added: hadState ? [] : [clientID],
      updated: hadState ? [clientID] : [],
      removed: [],
    }, origin);
  }

  private emit(change: AwarenessChange, origin: unknown): void {
    for (const handler of this.handlers.get("change") ?? []) {
      handler(change, origin);
    }
    for (const handler of this.handlers.get("update") ?? []) {
      handler(change, origin);
    }
  }
}

export interface OpenRTCAwarenessPresenceClient {
  readonly connId?: string;
  updatePresence(room: string, state: PresenceState): string;
  patchPresence?(room: string, patch: PresenceState): string;
  getOthers(room: string): PresencePeer[];
  on(type: "room", handler: (event: { room: string; others: PresencePeer[] }) => void): () => void;
  on(type: "presence", handler: (event: { room: string; connId: string; offline: boolean; state?: PresenceState; others: PresencePeer[] }) => void): () => void;
}

export interface OpenRTCAwarenessBridgeOptions {
  presenceKey?: string;
  throttleMs?: number;
  extraPresence?: () => PresenceState;
}

export interface OpenRTCAwarenessBridge {
  flush(): void;
  dispose(): void;
}

interface AwarenessPresencePayload {
  kind: "yjs-awareness";
  version: 1;
  clientId: number;
  state: AwarenessState | null;
  updatedAt: number;
}

export function bindOpenRTCAwareness(
  client: OpenRTCAwarenessPresenceClient,
  room: string,
  awareness: OpenRTCAwareness,
  options: OpenRTCAwarenessBridgeOptions = {},
): OpenRTCAwarenessBridge {
  const presenceKey = options.presenceKey ?? OPENRTC_AWARENESS_PRESENCE_KEY;
  const throttleMs = options.throttleMs ?? 50;
  const origin = {};
  const clientIDByConnID = new Map<string, number>();
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const send = (): void => {
    timer = undefined;
    if (disposed) {
      return;
    }
    const payload: AwarenessPresencePayload = {
      kind: "yjs-awareness",
      version: 1,
      clientId: awareness.clientID,
      state: awareness.getLocalState(),
      updatedAt: Date.now(),
    };
    const patch: PresenceState = {
      ...options.extraPresence?.(),
      [presenceKey]: payload,
    };
    if ("patchPresence" in client && typeof client.patchPresence === "function") {
      client.patchPresence(room, patch);
      return;
    }
    client.updatePresence(room, patch);
  };

  const schedule = (): void => {
    if (disposed) {
      return;
    }
    if (throttleMs <= 0) {
      send();
      return;
    }
    if (!timer) {
      timer = setTimeout(send, throttleMs);
    }
  };

  const applyPeer = (peer: PresencePeer): void => {
    if (peer.connId === client.connId) {
      return;
    }
    const payload = parseAwarenessPresence(peer.state[presenceKey]);
    if (!payload) {
      removePeer(peer.connId);
      return;
    }
    clientIDByConnID.set(peer.connId, payload.clientId);
    awareness.applyRemoteState(payload.clientId, payload.state, origin);
  };

  const removePeer = (connId: string): void => {
    const clientID = clientIDByConnID.get(connId);
    if (clientID === undefined) {
      return;
    }
    clientIDByConnID.delete(connId);
    awareness.applyRemoteState(clientID, null, origin);
  };

  const syncPeers = (peers: PresencePeer[]): void => {
    for (const peer of peers) {
      applyPeer(peer);
    }
  };

  const onAwarenessChange: AwarenessEventHandler = (_change, eventOrigin) => {
    if (eventOrigin === origin) {
      return;
    }
    schedule();
  };
  const offRoom = client.on("room", (event: { room: string; others: PresencePeer[] }) => {
    if (event.room === room) {
      syncPeers(event.others);
    }
  });
  const offPresence = client.on("presence", (event: { room: string; connId: string; offline: boolean; state?: PresenceState; others: PresencePeer[] }) => {
    if (event.room !== room) {
      return;
    }
    if (event.offline) {
      removePeer(event.connId);
      return;
    }
    if (event.connId !== client.connId && event.state) {
      applyPeer({ connId: event.connId, state: event.state });
    }
    syncPeers(event.others);
  });
  awareness.on("change", onAwarenessChange);
  syncPeers(client.getOthers(room));

  return {
    flush() {
      if (timer) {
        clearTimeout(timer);
      }
      send();
    },
    dispose() {
      disposed = true;
      if (timer) {
        clearTimeout(timer);
        timer = undefined;
      }
      awareness.off("change", onAwarenessChange);
      offRoom();
      offPresence();
    },
  };
}

export function yjsWebSocketURL(rawURL: string, room: string, token: string): string {
  const parsed = new URL(rawURL);
  if (parsed.protocol === "http:") {
    parsed.protocol = "ws:";
  } else if (parsed.protocol === "https:") {
    parsed.protocol = "wss:";
  }
  parsed.pathname = `/yjs/${encodeURIComponent(room)}`;
  parsed.searchParams.set("token", token);
  return parsed.toString();
}

function readMessageData(event: unknown): unknown {
  if (typeof event === "object" && event !== null && "data" in event) {
    return event.data;
  }
  return undefined;
}

async function toUint8Array(data: unknown): Promise<Uint8Array> {
  if (data instanceof Uint8Array) {
    return data;
  }
  if (data instanceof ArrayBuffer) {
    return new Uint8Array(data);
  }
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
  }
  if (isBlobLike(data)) {
    return new Uint8Array(await data.arrayBuffer());
  }
  return new Uint8Array();
}

function isBlobLike(value: unknown): value is { arrayBuffer(): Promise<ArrayBuffer> } {
  return typeof value === "object" && value !== null && "arrayBuffer" in value;
}

function syncedKind(kind: number): YjsSyncedFrameKind {
  if (kind === FRAME_SNAPSHOT) {
    return "snapshot";
  }
  if (kind === FRAME_STATE_VECTOR_DIFF) {
    return "state-vector-diff";
  }
  return "update";
}

function hashBytes(bytes: Uint8Array): string {
  let hash = 0x811c9dc5;
  for (const byte of bytes) {
    hash ^= byte;
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

function parseAwarenessPresence(value: unknown): AwarenessPresencePayload | null {
  if (!isRecord(value)) {
    return null;
  }
  if (value["kind"] !== "yjs-awareness" || value["version"] !== 1) {
    return null;
  }
  const clientId = value["clientId"];
  const state = value["state"];
  if (typeof clientId !== "number" || !Number.isSafeInteger(clientId) || clientId < 0) {
    return null;
  }
  const parsedState = state === null ? null : isRecord(state) ? state : undefined;
  if (parsedState === undefined) {
    return null;
  }
  return {
    kind: "yjs-awareness",
    version: 1,
    clientId,
    state: parsedState,
    updatedAt: typeof value["updatedAt"] === "number" ? value["updatedAt"] : 0,
  };
}

function awarenessStateEqual(left: AwarenessState | undefined, right: AwarenessState): boolean {
  if (!left) {
    return false;
  }
  return JSON.stringify(left) === JSON.stringify(right);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
