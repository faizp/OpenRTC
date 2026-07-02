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

async function tick(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitForSocketCount(count: number): Promise<void> {
  for (let attempt = 0; attempt < 10; attempt++) {
    if (FakeYjsSocket.instances.length >= count) {
      return;
    }
    await tick();
  }
  assert.fail(`timed out waiting for ${count} fake Yjs sockets`);
}
