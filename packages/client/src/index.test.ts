import assert from "node:assert/strict";
import { OpenRTCClient, type OpenRTCWebSocket } from "./index.ts";

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

const client = new OpenRTCClient({
  url: "http://localhost:8080/ws",
  token: "token-1",
  WebSocket: FakeWebSocket,
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
  },
});
assert.deepEqual(client.getOthers("tenant-a:doc"), [{ connId: "peer-1", state: { name: "B" } }]);

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
