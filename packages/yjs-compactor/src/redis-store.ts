import { createClient } from "redis";
import type { SequencedYjsUpdate, YjsCompactionInput, YjsCompactionStore } from "./index.ts";

export interface OpenRTCYjsRedisStoreOptions {
  redisUrl: string;
  onError?: ((error: Error) => void) | undefined;
}

export interface ScanRoomsOptions {
  count?: number | undefined;
}

interface YjsUpdateRecord {
  seq: number;
  kind?: number;
  update: string;
}

interface YjsSnapshotRecord {
  checkpoint_seq: number;
  snapshot: string;
}

export interface RedisCommandClient {
  close(): Promise<unknown>;
  sendCommand(command: string[]): Promise<unknown>;
}

export class OpenRTCYjsRedisStore implements YjsCompactionStore {
  private constructor(private readonly client: RedisCommandClient) {}

  static async connect(options: OpenRTCYjsRedisStoreOptions): Promise<OpenRTCYjsRedisStore> {
    const client = createClient({ url: options.redisUrl });
    client.on("error", (error) => {
      options.onError?.(error instanceof Error ? error : new Error(String(error)));
    });
    await client.connect();
    return new OpenRTCYjsRedisStore(client);
  }

  static fromClient(client: RedisCommandClient): OpenRTCYjsRedisStore {
    return new OpenRTCYjsRedisStore(client);
  }

  async close(): Promise<void> {
    await this.client.close();
  }

  async load(room: string): Promise<YjsCompactionInput> {
    const snapshotRecord = await this.loadSnapshot(room);
    const updates: SequencedYjsUpdate[] = [];
    const legacyUpdates = await this.commandArray(["LRANGE", roomYJSUpdatesKey(room), "0", "-1"]);
    for (const update of legacyUpdates) {
      updates.push({ seq: 0, update: decodeBulkBytes(update) });
    }

    const min = `(${snapshotRecord.checkpoint}`;
    const records = await this.commandArray(["ZRANGEBYSCORE", roomYJSUpdatesV2Key(room), min, "+inf"]);
    for (const raw of records) {
      const record = decodeYJSUpdateRecord(raw);
      if (record.seq > snapshotRecord.checkpoint) {
        updates.push({
          seq: record.seq,
          kind: record.kind,
          update: record.update,
        });
      }
    }

    return {
      snapshot: snapshotRecord.snapshot,
      snapshotCheckpoint: snapshotRecord.checkpoint,
      updates,
    };
  }

  async compact(room: string, checkpointSeq: number, snapshot: Uint8Array): Promise<void> {
    const record = JSON.stringify({
      checkpoint_seq: checkpointSeq,
      snapshot: encodeBytes(snapshot),
    } satisfies YjsSnapshotRecord);

    await this.client.sendCommand(["MULTI"]);
    try {
      await this.client.sendCommand(["SET", roomYJSSnapshotV2Key(room), record]);
      await this.client.sendCommand(["ZREMRANGEBYSCORE", roomYJSUpdatesV2Key(room), "-inf", String(checkpointSeq)]);
      await this.client.sendCommand(["EXEC"]);
    } catch (error) {
      await this.client.sendCommand(["DISCARD"]).catch(() => undefined);
      throw error;
    }
  }

  async scanRooms(options: ScanRoomsOptions = {}): Promise<string[]> {
    let cursor = "0";
    const rooms = new Set<string>();
    do {
      const response = await this.client.sendCommand([
        "SCAN",
        cursor,
        "MATCH",
        "room:*:yjs:updates:v2",
        "COUNT",
        String(options.count ?? 100),
      ]);
      if (!Array.isArray(response) || response.length !== 2) {
        throw new Error("unexpected SCAN response");
      }
      cursor = String(response[0]);
      for (const key of response[1] as unknown[]) {
        const room = roomFromYJSUpdatesV2Key(String(key));
        if (room) {
          rooms.add(room);
        }
      }
    } while (cursor !== "0");
    return [...rooms].sort();
  }

  private async loadSnapshot(room: string): Promise<{ checkpoint: number; snapshot?: Uint8Array | undefined }> {
    const rawV2 = await this.client.sendCommand(["GET", roomYJSSnapshotV2Key(room)]);
    if (rawV2 !== null) {
      const record = decodeYJSSnapshotRecord(rawV2);
      return { checkpoint: record.checkpointSeq, snapshot: record.snapshot };
    }

    const legacy = await this.client.sendCommand(["GET", roomYJSSnapshotKey(room)]);
    return {
      checkpoint: 0,
      snapshot: legacy === null ? undefined : decodeBulkBytes(legacy),
    };
  }

  private async commandArray(command: string[]): Promise<unknown[]> {
    const response = await this.client.sendCommand(command);
    if (!Array.isArray(response)) {
      throw new Error(`unexpected Redis response for ${command[0]}`);
    }
    return response;
  }
}

function decodeYJSUpdateRecord(raw: unknown): { seq: number; kind: "update" | "subdoc-update"; update: Uint8Array } {
  const parsed = JSON.parse(decodeBulkString(raw)) as Partial<YjsUpdateRecord>;
  if (typeof parsed.seq !== "number" || parsed.seq <= 0 || typeof parsed.update !== "string") {
    throw new Error("invalid Yjs update record");
  }
  return { seq: parsed.seq, kind: decodeYJSUpdateKind(parsed.kind), update: decodeBytes(parsed.update) };
}

function decodeYJSUpdateKind(kind: number | undefined): "update" | "subdoc-update" {
  if (kind === undefined || kind === 1) {
    return "update";
  }
  if (kind === 5) {
    return "subdoc-update";
  }
  throw new Error("invalid Yjs update kind");
}

function decodeYJSSnapshotRecord(raw: unknown): { checkpointSeq: number; snapshot: Uint8Array } {
  const parsed = JSON.parse(decodeBulkString(raw)) as Partial<YjsSnapshotRecord>;
  if (typeof parsed.checkpoint_seq !== "number" || parsed.checkpoint_seq < 0 || typeof parsed.snapshot !== "string") {
    throw new Error("invalid Yjs snapshot record");
  }
  return { checkpointSeq: parsed.checkpoint_seq, snapshot: decodeBytes(parsed.snapshot) };
}

function decodeBulkString(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (value instanceof Buffer) {
    return value.toString("utf8");
  }
  throw new Error("expected Redis bulk string");
}

function decodeBulkBytes(value: unknown): Uint8Array {
  if (value instanceof Buffer) {
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength).slice();
  }
  return new TextEncoder().encode(decodeBulkString(value));
}

function decodeBytes(base64: string): Uint8Array {
  const buffer = Buffer.from(base64, "base64");
  return new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength).slice();
}

function encodeBytes(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

function roomYJSSnapshotKey(room: string): string {
  return `room:${room}:yjs:snapshot`;
}

function roomYJSUpdatesKey(room: string): string {
  return `room:${room}:yjs:updates`;
}

function roomYJSSnapshotV2Key(room: string): string {
  return `room:${room}:yjs:snapshot:v2`;
}

function roomYJSUpdatesV2Key(room: string): string {
  return `room:${room}:yjs:updates:v2`;
}

function roomFromYJSUpdatesV2Key(key: string): string | undefined {
  const prefix = "room:";
  const suffix = ":yjs:updates:v2";
  if (!key.startsWith(prefix) || !key.endsWith(suffix)) {
    return undefined;
  }
  return key.slice(prefix.length, -suffix.length);
}
