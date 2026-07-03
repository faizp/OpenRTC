import assert from "node:assert/strict";
import * as Y from "yjs";
import {
  OPENRTC_AWARENESS_PRESENCE_KEY,
  OpenRTCAwareness,
  OpenRTCYjsProvider,
  bindOpenRTCAwareness,
  yjsWebSocketURL,
  type YJSWebSocket,
  type AwarenessChange,
  type OpenRTCYjsOfflineStore,
  type OpenRTCYjsOfflineUpdateMeta,
} from "./index.ts";
import type { PresencePeer, PresenceState } from "@openrtc/client";

class FakeYjsSocket implements YJSWebSocket {
  static instances: FakeYjsSocket[] = [];

  binaryType: "arraybuffer" | "blob" = "arraybuffer";
  readyState = 0;
  peer: FakeYjsSocket | undefined;
  sent: Uint8Array[] = [];
  private listeners = new Map<string, Set<(event: unknown) => void>>();

  constructor(readonly url: string) {
    FakeYjsSocket.instances.push(this);
  }

  send(data: ArrayBufferLike | ArrayBufferView): void {
    const frame = toUint8(data);
    this.sent.push(frame);
    this.peer?.receive(frame);
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

  receive(frame: Uint8Array): void {
    this.emit("message", { data: frame.slice() });
  }

  private emit(type: string, event: unknown): void {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

class FakePresenceClient {
  readonly sent: Array<{ room: string; state: PresenceState }> = [];
  private readonly handlers = new Map<string, Set<(event: any) => void>>();
  private readonly othersByRoom = new Map<string, PresencePeer[]>();

  constructor(readonly connId: string) {}

  updatePresence(room: string, state: PresenceState): string {
    this.sent.push({ room, state });
    return `presence-${this.sent.length}`;
  }

  patchPresence(room: string, patch: PresenceState): string {
    const current = this.sent.at(-1)?.room === room ? this.sent.at(-1)?.state ?? {} : {};
    return this.updatePresence(room, { ...current, ...patch });
  }

  getOthers(room: string): PresencePeer[] {
    return this.othersByRoom.get(room) ?? [];
  }

  on(type: "room", handler: (event: { room: string; others: PresencePeer[] }) => void): () => void;
  on(type: "presence", handler: (event: { room: string; connId: string; offline: boolean; state?: PresenceState; others: PresencePeer[] }) => void): () => void;
  on(type: string, handler: (event: any) => void): () => void {
    let handlers = this.handlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.handlers.set(type, handlers);
    }
    handlers.add(handler);
    return () => handlers?.delete(handler);
  }

  emitRoom(room: string, others: PresencePeer[]): void {
    this.othersByRoom.set(room, others);
    this.emit("room", { room, others });
  }

  emitPresence(event: { room: string; connId: string; offline: boolean; state?: PresenceState; others?: PresencePeer[] }): void {
    const others = event.others ?? this.othersByRoom.get(event.room) ?? [];
    this.othersByRoom.set(event.room, others);
    this.emit("presence", { ...event, others });
  }

  private emit(type: string, event: unknown): void {
    for (const handler of this.handlers.get(type) ?? []) {
      handler(event);
    }
  }
}

class FakeOfflineStore implements OpenRTCYjsOfflineStore {
  readonly appended: Array<{ update: Uint8Array; meta: OpenRTCYjsOfflineUpdateMeta }> = [];
  loads = 0;

  constructor(private readonly updates: Uint8Array[] = []) {}

  async load(): Promise<Uint8Array[]> {
    this.loads++;
    return this.updates.map((update) => update.slice());
  }

  async append(update: Uint8Array, meta: OpenRTCYjsOfflineUpdateMeta): Promise<void> {
    this.appended.push({ update: update.slice(), meta });
  }
}

assert.equal(
  yjsWebSocketURL("http://localhost:8080/ws", "tenant-a:doc", "token-1"),
  "ws://localhost:8080/yjs/tenant-a%3Adoc?token=token-1",
);

const docA = new Y.Doc();
const docB = new Y.Doc();
const providerA = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:doc",
  token: "token-a",
  doc: docA,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
const providerB = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:doc",
  token: "token-b",
  doc: docB,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});

const socketStart = FakeYjsSocket.instances.length;
const connectA = providerA.connect();
const connectB = providerB.connect();
await waitForSocketCount(socketStart + 2);
const socketA = FakeYjsSocket.instances[socketStart];
const socketB = FakeYjsSocket.instances[socketStart + 1];
assert.ok(socketA);
assert.ok(socketB);
socketA.peer = socketB;
socketB.peer = socketA;
socketA.open();
socketB.open();
await connectA;
await connectB;

const textA = docA.getText("body");
const textB = docB.getText("body");
textA.insert(0, "A");
textB.insert(0, "B");

await tick();
assert.equal(textA.toString(), textB.toString());
assert.equal(textA.length, 2);

providerA.sendSnapshot();
assert.equal(socketA.sent.at(-1)?.[0], 1);

providerA.destroy();
providerB.destroy();

const stateVectorDocA = new Y.Doc();
const stateVectorDocB = new Y.Doc();
stateVectorDocA.getText("body").insert(0, "server text");
const stateVectorProviderA = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:state-vector-doc",
  token: "token-a",
  doc: stateVectorDocA,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
const stateVectorProviderB = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:state-vector-doc",
  token: "token-b",
  doc: stateVectorDocB,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
let diffSyncEvent: { kind: string; bytes: number; stateVectorHash: string } | undefined;
stateVectorProviderB.on("synced", (event) => {
  diffSyncEvent = event;
});
const stateVectorSocketStart = FakeYjsSocket.instances.length;
const stateVectorConnectA = stateVectorProviderA.connect();
const stateVectorConnectB = stateVectorProviderB.connect();
await waitForSocketCount(stateVectorSocketStart + 2);
const stateVectorSocketA = FakeYjsSocket.instances[stateVectorSocketStart];
const stateVectorSocketB = FakeYjsSocket.instances[stateVectorSocketStart + 1];
assert.ok(stateVectorSocketA);
assert.ok(stateVectorSocketB);
stateVectorSocketA.peer = stateVectorSocketB;
stateVectorSocketB.peer = stateVectorSocketA;
stateVectorSocketA.open();
stateVectorSocketB.open();
await stateVectorConnectA;
await stateVectorConnectB;

assert.equal(stateVectorProviderB.requestSync(), true);
await tick();
assert.equal(stateVectorSocketB.sent.at(-1)?.[0], 3);
assert.equal(stateVectorSocketA.sent.at(-1)?.[0], 4);
assert.equal(stateVectorDocB.getText("body").toString(), "server text");
assert.equal(diffSyncEvent?.kind, "state-vector-diff");
assert.equal(typeof diffSyncEvent?.stateVectorHash, "string");
assert.equal(stateVectorProviderB.getSyncState().status, "synced");
assert.equal(stateVectorProviderB.getSyncState().diffsReceived, 1);
assert.equal(stateVectorProviderA.getSyncState().sentBytes > 0, true);
stateVectorProviderA.destroy();
stateVectorProviderB.destroy();

const subdocParentA = new Y.Doc();
const subdocParentB = new Y.Doc();
const subdocA = new Y.Doc({ guid: "subdoc-live-1" });
const subdocB = new Y.Doc({ guid: "subdoc-live-1" });
subdocParentA.getMap<Y.Doc>("subdocs").set("body", subdocA);
subdocParentB.getMap<Y.Doc>("subdocs").set("body", subdocB);
const subdocProviderA = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:subdoc-doc",
  token: "token-a",
  doc: subdocParentA,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
const subdocProviderB = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:subdoc-doc",
  token: "token-b",
  doc: subdocParentB,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
let subdocSyncEvent: { kind: string; bytes: number; stateVectorHash: string } | undefined;
subdocProviderB.on("synced", (event) => {
  subdocSyncEvent = event;
});
const subdocSocketStart = FakeYjsSocket.instances.length;
const subdocConnectA = subdocProviderA.connect();
const subdocConnectB = subdocProviderB.connect();
await waitForSocketCount(subdocSocketStart + 2);
const subdocSocketA = FakeYjsSocket.instances[subdocSocketStart];
const subdocSocketB = FakeYjsSocket.instances[subdocSocketStart + 1];
assert.ok(subdocSocketA);
assert.ok(subdocSocketB);
subdocSocketA.peer = subdocSocketB;
subdocSocketB.peer = subdocSocketA;
subdocSocketA.open();
subdocSocketB.open();
await subdocConnectA;
await subdocConnectB;

subdocA.getText("body").insert(0, "subdoc text");
await tick();
assert.equal(subdocB.getText("body").toString(), "subdoc text");
assert.equal(subdocSocketA.sent.at(-1)?.[0], 5);
const subdocPayload = decodeTestSubdocPayload(subdocSocketA.sent.at(-1)!.subarray(1));
assert.equal(subdocPayload.guid, "subdoc-live-1");
assert.equal(subdocProviderA.getSyncState().subdocsTracked, 1);
assert.equal(subdocProviderA.getSyncState().subdocUpdatesSent, 1);
assert.equal(subdocProviderB.getSyncState().subdocUpdatesReceived, 1);
assert.equal(subdocSyncEvent?.kind, "subdoc-update");
subdocProviderA.destroy();
subdocProviderB.destroy();

const subdocStateParentA = new Y.Doc();
const subdocStateParentB = new Y.Doc();
const subdocStateA = new Y.Doc({ guid: "subdoc-state-1" });
const subdocStateB = new Y.Doc({ guid: "subdoc-state-1" });
subdocStateA.getText("body").insert(0, "server subdoc");
subdocStateParentA.getMap<Y.Doc>("subdocs").set("body", subdocStateA);
subdocStateParentB.getMap<Y.Doc>("subdocs").set("body", subdocStateB);
const subdocStateProviderA = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:subdoc-state-doc",
  token: "token-a",
  doc: subdocStateParentA,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
const subdocStateProviderB = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:subdoc-state-doc",
  token: "token-b",
  doc: subdocStateParentB,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
});
let subdocDiffSyncEvent: { kind: string; bytes: number; stateVectorHash: string } | undefined;
subdocStateProviderB.on("synced", (event) => {
  subdocDiffSyncEvent = event;
});
const subdocStateSocketStart = FakeYjsSocket.instances.length;
const subdocStateConnectA = subdocStateProviderA.connect();
const subdocStateConnectB = subdocStateProviderB.connect();
await waitForSocketCount(subdocStateSocketStart + 2);
const subdocStateSocketA = FakeYjsSocket.instances[subdocStateSocketStart];
const subdocStateSocketB = FakeYjsSocket.instances[subdocStateSocketStart + 1];
assert.ok(subdocStateSocketA);
assert.ok(subdocStateSocketB);
subdocStateSocketA.peer = subdocStateSocketB;
subdocStateSocketB.peer = subdocStateSocketA;
subdocStateSocketA.open();
subdocStateSocketB.open();
await subdocStateConnectA;
await subdocStateConnectB;

assert.equal(subdocStateProviderB.requestSubdocSync("subdoc-state-1"), true);
await tick();
assert.equal(subdocStateSocketB.sent.at(-1)?.[0], 6);
assert.equal(decodeTestSubdocPayload(subdocStateSocketB.sent.at(-1)!.subarray(1)).guid, "subdoc-state-1");
assert.equal(subdocStateSocketA.sent.at(-1)?.[0], 7);
assert.equal(subdocStateB.getText("body").toString(), "server subdoc");
assert.equal(subdocDiffSyncEvent?.kind, "subdoc-state-vector-diff");
assert.equal(subdocStateProviderB.getSyncState().subdocDiffsReceived, 1);
subdocStateProviderA.destroy();
subdocStateProviderB.destroy();

const cachedDoc = new Y.Doc();
cachedDoc.getText("body").insert(0, "cached");
const offlineStore = new FakeOfflineStore([Y.encodeStateAsUpdate(cachedDoc)]);
const offlineDoc = new Y.Doc();
const offlineProvider = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:offline-doc",
  token: "token-offline",
  doc: offlineDoc,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSync: false,
  offlineStore,
});
const offlineSocketStart = FakeYjsSocket.instances.length;
const offlineConnect = offlineProvider.connect();
await waitForSocketCount(offlineSocketStart + 1);
const offlineSocket = FakeYjsSocket.instances.at(-1);
assert.ok(offlineSocket);
offlineSocket.open();
await offlineConnect;
assert.equal(offlineStore.loads, 1);
assert.equal(offlineDoc.getText("body").toString(), "cached");
assert.equal(offlineProvider.getSyncState().offlineLoaded, true);
assert.equal(offlineProvider.getSyncState().offlineUpdatesLoaded, 1);
assert.equal(offlineProvider.getSyncState().offlineBytesLoaded > 0, true);

offlineDoc.getText("body").insert(6, " local");
await tick();
assert.equal(offlineStore.appended.at(-1)?.meta.source, "local");
assert.equal(offlineStore.appended.at(-1)?.meta.kind, "update");
assert.equal(offlineStore.appended.at(-1)?.meta.room, "tenant-a:offline-doc");
assert.equal(offlineSocket.sent.at(-1)?.[0], 1);

const remoteOfflineDoc = new Y.Doc();
remoteOfflineDoc.getText("remote").insert(0, "remote");
const remoteOfflineUpdate = Y.encodeStateAsUpdate(remoteOfflineDoc);
offlineSocket.receive(frame(1, remoteOfflineUpdate));
await tick();
assert.equal(offlineStore.appended.at(-1)?.meta.source, "remote");
assert.equal(offlineStore.appended.at(-1)?.meta.kind, "update");
assert.equal(offlineProvider.getSyncState().offlineUpdatesStored, 2);
assert.equal(offlineProvider.getSyncState().offlineBytesStored > 0, true);
offlineProvider.destroy();

const reconnectDoc = new Y.Doc();
const reconnectProvider = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:reconnect-doc",
  token: () => `token-reconnect-${FakeYjsSocket.instances.length}`,
  doc: reconnectDoc,
  WebSocket: FakeYjsSocket,
  connect: false,
  stateVectorSyncDelayMs: 0,
  reconnect: { initialDelayMs: 1, maxDelayMs: 1, jitterRatio: 0 },
});
const reconnectStatuses: string[] = [];
reconnectProvider.on("status", (status) => {
  reconnectStatuses.push(status);
});
const reconnectSocketStart = FakeYjsSocket.instances.length;
const reconnectConnect = reconnectProvider.connect();
await waitForSocketCount(reconnectSocketStart + 1);
const reconnectSocketA = FakeYjsSocket.instances[reconnectSocketStart];
assert.ok(reconnectSocketA);
reconnectSocketA.open();
await reconnectConnect;
reconnectDoc.getText("body").insert(0, "online");
assert.equal(reconnectSocketA.sent.at(-1)?.[0], 1);

reconnectSocketA.close();
assert.equal(reconnectProvider.status, "reconnecting");
assert.equal(reconnectProvider.getSyncState().status, "reconnecting");
assert.equal(reconnectProvider.getSyncState().reconnectAttempts, 1);
assert.equal(typeof reconnectProvider.getSyncState().lastDisconnectedAt, "number");
reconnectDoc.getText("body").insert(6, " local");
assert.equal(reconnectProvider.getSyncState().pendingLocalSync, true);

await waitForSocketCount(reconnectSocketStart + 2);
const reconnectSocketB = FakeYjsSocket.instances[reconnectSocketStart + 1];
assert.ok(reconnectSocketB);
reconnectSocketB.open();
await waitFor(() => reconnectProvider.status === "open", "expected Yjs provider to reopen after reconnect");
assert.equal(reconnectProvider.getSyncState().pendingLocalSync, false);
assert.equal(reconnectProvider.getSyncState().reconnectAttempts, 0);
assert.equal(reconnectSocketB.sent[0]?.[0], 1);
await waitFor(() => reconnectSocketB.sent.some((sent) => sent[0] === 3), "expected state-vector sync after reconnect");
assert.equal(reconnectStatuses.includes("reconnecting"), true);

const explicitReconnectSocketStart = FakeYjsSocket.instances.length;
const explicitReconnect = reconnectProvider.reconnect();
await waitForSocketCount(explicitReconnectSocketStart + 1);
const reconnectSocketC = FakeYjsSocket.instances[explicitReconnectSocketStart];
assert.ok(reconnectSocketC);
reconnectSocketC.open();
await explicitReconnect;
assert.equal(reconnectProvider.status, "open");
await waitFor(() => reconnectSocketC.sent.some((sent) => sent[0] === 3), "expected state-vector sync after explicit reconnect");
reconnectProvider.destroy();

const noReconnectProvider = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:no-reconnect-doc",
  token: "token-no-reconnect",
  doc: new Y.Doc(),
  WebSocket: FakeYjsSocket,
  connect: false,
  autoReconnect: false,
});
const noReconnectSocketStart = FakeYjsSocket.instances.length;
const noReconnectConnect = noReconnectProvider.connect();
await waitForSocketCount(noReconnectSocketStart + 1);
const noReconnectSocket = FakeYjsSocket.instances[noReconnectSocketStart];
assert.ok(noReconnectSocket);
noReconnectSocket.open();
await noReconnectConnect;
noReconnectSocket.close();
await tick();
assert.equal(noReconnectProvider.status, "closed");
assert.equal(FakeYjsSocket.instances.length, noReconnectSocketStart + 1);
noReconnectProvider.destroy();

const awarenessA = new OpenRTCAwareness(new Y.Doc());
const awarenessB = new OpenRTCAwareness(new Y.Doc());
const presenceA = new FakePresenceClient("conn-a");
const presenceB = new FakePresenceClient("conn-b");
const bridgeA = bindOpenRTCAwareness(presenceA, "tenant-a:doc", awarenessA, { throttleMs: 0 });
const bridgeB = bindOpenRTCAwareness(presenceB, "tenant-a:doc", awarenessB, { throttleMs: 0 });

let awarenessBChange: AwarenessChange | undefined;
awarenessB.on("change", (change) => {
  awarenessBChange = change;
});
awarenessA.setLocalStateField("user", { name: "A", color: "#f00" });
const awarenessPayloadA = presenceA.sent.at(-1)?.state[OPENRTC_AWARENESS_PRESENCE_KEY];
assert.ok(awarenessPayloadA);
presenceB.emitRoom("tenant-a:doc", [{ connId: "conn-a", state: { [OPENRTC_AWARENESS_PRESENCE_KEY]: awarenessPayloadA } }]);
assert.deepEqual(awarenessB.getStates().get(awarenessA.clientID), { user: { name: "A", color: "#f00" } });
assert.deepEqual(awarenessBChange?.added, [awarenessA.clientID]);

presenceB.emitPresence({ room: "tenant-a:doc", connId: "conn-a", offline: true });
assert.equal(awarenessB.getStates().has(awarenessA.clientID), false);

awarenessA.setLocalStateField("cursor", { anchor: 1, head: 2 });
assert.equal(presenceA.sent.at(-1)?.state[OPENRTC_AWARENESS_PRESENCE_KEY] !== undefined, true);
assert.deepEqual(presenceA.sent.at(-1)?.state["user"], undefined);

const providerPresence = new FakePresenceClient("conn-provider");
const providerWithAwareness = new OpenRTCYjsProvider({
  url: "http://localhost:8080",
  room: "tenant-a:provider-doc",
  token: "token-provider",
  doc: new Y.Doc(),
  WebSocket: FakeYjsSocket,
  connect: false,
  presenceClient: providerPresence,
  awarenessOptions: { throttleMs: 0, extraPresence: () => ({ user: { name: "Provider" } }) },
});
providerWithAwareness.awareness.setLocalStateField("cursor", { anchor: 3, head: 4 });
assert.deepEqual(providerPresence.sent.at(-1)?.state["user"], { name: "Provider" });
assert.ok(providerPresence.sent.at(-1)?.state[OPENRTC_AWARENESS_PRESENCE_KEY]);
providerWithAwareness.destroy();

bridgeA.dispose();
bridgeB.dispose();

function toUint8(data: ArrayBufferLike | ArrayBufferView): Uint8Array {
  if (data instanceof Uint8Array) {
    return data.slice();
  }
  if (ArrayBuffer.isView(data)) {
    return new Uint8Array(data.buffer, data.byteOffset, data.byteLength).slice();
  }
  return new Uint8Array(data).slice();
}

function frame(kind: number, update: Uint8Array): Uint8Array {
  const data = new Uint8Array(1 + update.byteLength);
  data[0] = kind;
  data.set(update, 1);
  return data;
}

function decodeTestSubdocPayload(payload: Uint8Array): { guid: string; update: Uint8Array } {
  assert.equal(payload.byteLength >= 3, true);
  const guidLength = (payload[0]! << 8) | payload[1]!;
  assert.equal(guidLength > 0, true);
  assert.equal(payload.byteLength > 2 + guidLength, true);
  return {
    guid: new TextDecoder().decode(payload.subarray(2, 2 + guidLength)),
    update: payload.subarray(2 + guidLength),
  };
}

async function tick(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitForSocketCount(count: number): Promise<void> {
  await waitFor(() => FakeYjsSocket.instances.length >= count, `timed out waiting for ${count} fake Yjs sockets`);
}

async function waitFor(predicate: () => boolean, message: string): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt++) {
    if (predicate()) {
      return;
    }
    await tick();
  }
  assert.fail(message);
}
