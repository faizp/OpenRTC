import assert from "node:assert/strict";
import { OpenRTCYjsRedisStore, type RedisCommandClient } from "./redis-store.ts";

class ScriptedRedisClient implements RedisCommandClient {
  readonly commands: string[][] = [];

  constructor(private readonly responses: unknown[]) {}

  async close(): Promise<void> {}

  async sendCommand(command: string[]): Promise<unknown> {
    this.commands.push(command);
    const response = this.responses.shift();
    if (response instanceof Error) {
      throw response;
    }
    return response;
  }
}

const snapshot = new Uint8Array([1, 2, 3]);
const update = new Uint8Array([4, 5, 6]);

const loadClient = new ScriptedRedisClient([
  JSON.stringify({ checkpoint_seq: 2, snapshot: encode(snapshot) }),
  [],
  [JSON.stringify({ seq: 3, update: encode(update) }), JSON.stringify({ seq: 1, update: encode(new Uint8Array([9])) })],
]);
const loaded = await OpenRTCYjsRedisStore.fromClient(loadClient).load("tenant-a:doc-1");
assert.deepEqual(loaded.snapshot, snapshot);
assert.equal(loaded.snapshotCheckpoint, 2);
assert.equal(loaded.updates.length, 1);
assert.equal(loaded.updates[0]!.seq, 3);
assert.deepEqual(loaded.updates[0]!.update, update);
assert.deepEqual(loadClient.commands, [
  ["GET", "room:tenant-a:doc-1:yjs:snapshot:v2"],
  ["LRANGE", "room:tenant-a:doc-1:yjs:updates", "0", "-1"],
  ["ZRANGEBYSCORE", "room:tenant-a:doc-1:yjs:updates:v2", "(2", "+inf"],
]);

const compactClient = new ScriptedRedisClient(["OK", "QUEUED", "QUEUED", ["OK", 2]]);
await OpenRTCYjsRedisStore.fromClient(compactClient).compact("tenant-a:doc-1", 7, snapshot);
assert.deepEqual(compactClient.commands[0], ["MULTI"]);
assert.equal(compactClient.commands[1]![0], "SET");
assert.equal(compactClient.commands[1]![1], "room:tenant-a:doc-1:yjs:snapshot:v2");
assert.deepEqual(JSON.parse(compactClient.commands[1]![2]!), {
  checkpoint_seq: 7,
  snapshot: encode(snapshot),
});
assert.deepEqual(compactClient.commands[2], ["ZREMRANGEBYSCORE", "room:tenant-a:doc-1:yjs:updates:v2", "-inf", "7"]);
assert.deepEqual(compactClient.commands[3], ["EXEC"]);

const scanClient = new ScriptedRedisClient([
  ["1", ["room:tenant-a:doc-1:yjs:updates:v2", "unrelated"]],
  ["0", ["room:tenant-a:doc-2:yjs:updates:v2"]],
]);
const rooms = await OpenRTCYjsRedisStore.fromClient(scanClient).scanRooms({ count: 10 });
assert.deepEqual(rooms, ["tenant-a:doc-1", "tenant-a:doc-2"]);
assert.deepEqual(scanClient.commands, [
  ["SCAN", "0", "MATCH", "room:*:yjs:updates:v2", "COUNT", "10"],
  ["SCAN", "1", "MATCH", "room:*:yjs:updates:v2", "COUNT", "10"],
]);

function encode(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}
