import * as Y from "yjs";

export interface SequencedYjsUpdate {
  seq: number;
  update: Uint8Array;
}

export interface YjsCompactionInput {
  snapshot?: Uint8Array | undefined;
  snapshotCheckpoint?: number | undefined;
  updates: SequencedYjsUpdate[];
}

export interface YjsCompactionResult {
  snapshot: Uint8Array;
  checkpointSeq: number;
  compactedUpdates: number;
  beforeBytes: number;
  afterBytes: number;
}

export interface YjsCompactionStore {
  load(room: string): Promise<YjsCompactionInput>;
  compact(room: string, checkpointSeq: number, snapshot: Uint8Array): Promise<void>;
}

export interface CompactRoomOptions {
  minUpdates?: number | undefined;
  minBytes?: number | undefined;
}

export interface CompactRoomResult extends YjsCompactionResult {
  room: string;
  skipped: false;
}

export interface SkippedCompactRoomResult {
  room: string;
  skipped: true;
  reason: "no-updates" | "below-threshold" | "legacy-updates";
  updateCount: number;
  updateBytes: number;
}

export type CompactRoomOutcome = CompactRoomResult | SkippedCompactRoomResult;

export async function compactRoom(
  store: YjsCompactionStore,
  room: string,
  options: CompactRoomOptions = {},
): Promise<CompactRoomOutcome> {
  const input = await store.load(room);
  const sequencedUpdates = input.updates.filter((update) => update.seq > 0);
  const legacyUpdates = input.updates.length - sequencedUpdates.length;
  const updateBytes = sequencedUpdates.reduce((total, update) => total + update.update.byteLength, 0);

  if (legacyUpdates > 0) {
    return { room, skipped: true, reason: "legacy-updates", updateCount: input.updates.length, updateBytes };
  }
  if (sequencedUpdates.length === 0) {
    return { room, skipped: true, reason: "no-updates", updateCount: 0, updateBytes: 0 };
  }
  if (options.minUpdates !== undefined && sequencedUpdates.length < options.minUpdates) {
    return { room, skipped: true, reason: "below-threshold", updateCount: sequencedUpdates.length, updateBytes };
  }
  if (options.minBytes !== undefined && updateBytes < options.minBytes) {
    return { room, skipped: true, reason: "below-threshold", updateCount: sequencedUpdates.length, updateBytes };
  }

  const result = compactYjsDocument({
    snapshot: input.snapshot,
    snapshotCheckpoint: input.snapshotCheckpoint,
    updates: sequencedUpdates,
  });
  await store.compact(room, result.checkpointSeq, result.snapshot);
  return { room, skipped: false, ...result };
}

export function compactYjsDocument(input: YjsCompactionInput): YjsCompactionResult {
  const updates = input.updates.filter((update) => update.seq > (input.snapshotCheckpoint ?? 0));
  if (updates.length === 0 && !input.snapshot) {
    throw new Error("at least one update or snapshot is required for Yjs compaction");
  }

  const mergedInputs: Uint8Array[] = [];
  if (input.snapshot && input.snapshot.byteLength > 0) {
    mergedInputs.push(input.snapshot);
  }
  for (const update of updates) {
    if (update.seq <= 0) {
      throw new Error("Yjs update sequence must be positive");
    }
    if (update.update.byteLength === 0) {
      throw new Error("Yjs update payload is required");
    }
    mergedInputs.push(update.update);
  }
  if (mergedInputs.length === 0) {
    throw new Error("nothing to compact");
  }

  const snapshot = normalizeMergedUpdate(Y.mergeUpdates(mergedInputs));
  const checkpointSeq = updates.reduce((max, update) => Math.max(max, update.seq), input.snapshotCheckpoint ?? 0);
  return {
    snapshot,
    checkpointSeq,
    compactedUpdates: updates.length,
    beforeBytes: mergedInputs.reduce((total, update) => total + update.byteLength, 0),
    afterBytes: snapshot.byteLength,
  };
}

function normalizeMergedUpdate(update: Uint8Array): Uint8Array {
  const doc = new Y.Doc();
  Y.applyUpdate(doc, update);
  const snapshot = Y.encodeStateAsUpdate(doc);
  doc.destroy();
  return snapshot;
}
