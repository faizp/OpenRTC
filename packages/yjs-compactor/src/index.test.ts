import assert from "node:assert/strict";
import * as Y from "yjs";
import {
  compactRoom,
  compactYjsDocument,
  type SequencedYjsUpdate,
  type YjsCompactionInput,
  type YjsCompactionStore,
} from "./index.ts";

class MemoryCompactionStore implements YjsCompactionStore {
  compactedCheckpoint: number | undefined;
  compactedSnapshot: Uint8Array | undefined;

  constructor(private readonly input: YjsCompactionInput) {}

  async load(): Promise<YjsCompactionInput> {
    return this.input;
  }

  async compact(_room: string, checkpointSeq: number, snapshot: Uint8Array): Promise<void> {
    this.compactedCheckpoint = checkpointSeq;
    this.compactedSnapshot = snapshot;
  }
}

const updates = makeTextUpdates("body", ["hello", " world"]);
const compacted = compactYjsDocument({ updates });
assert.equal(compacted.checkpointSeq, 2);
assert.equal(compacted.compactedUpdates, 2);
assertTextSnapshot(compacted.snapshot, "body", "hello world");

const nextUpdate = makeUpdateFromSnapshot(compacted.snapshot, "body", "!");
const compactedAgain = compactYjsDocument({
  snapshot: compacted.snapshot,
  snapshotCheckpoint: compacted.checkpointSeq,
  updates: [{ seq: 3, update: nextUpdate }],
});
assert.equal(compactedAgain.checkpointSeq, 3);
assert.equal(compactedAgain.compactedUpdates, 1);
assertTextSnapshot(compactedAgain.snapshot, "body", "hello world!");

const store = new MemoryCompactionStore({
  snapshot: compacted.snapshot,
  snapshotCheckpoint: 2,
  updates: [{ seq: 3, update: nextUpdate }],
});
const outcome = await compactRoom(store, "tenant-a:doc-1", { minUpdates: 1 });
assert.equal(outcome.skipped, false);
assert.equal(store.compactedCheckpoint, 3);
assert.ok(store.compactedSnapshot);
assertTextSnapshot(store.compactedSnapshot, "body", "hello world!");

const belowThreshold = await compactRoom(
  new MemoryCompactionStore({ updates: [{ seq: 1, update: updates[0]!.update }] }),
  "tenant-a:doc-2",
  { minUpdates: 2 },
);
assert.deepEqual(belowThreshold, {
  room: "tenant-a:doc-2",
  skipped: true,
  reason: "below-threshold",
  updateCount: 1,
  updateBytes: updates[0]!.update.byteLength,
});

const legacySkipped = await compactRoom(
  new MemoryCompactionStore({ updates: [{ seq: 0, update: updates[0]!.update }] }),
  "tenant-a:legacy",
);
assert.equal(legacySkipped.skipped, true);
if (legacySkipped.skipped) {
  assert.equal(legacySkipped.reason, "legacy-updates");
}

const subdocSkipped = await compactRoom(
  new MemoryCompactionStore({ updates: [{ seq: 1, kind: "subdoc-update", update: new Uint8Array([1, 2, 3]) }] }),
  "tenant-a:subdoc",
);
assert.equal(subdocSkipped.skipped, true);
if (subdocSkipped.skipped) {
  assert.equal(subdocSkipped.reason, "subdoc-updates");
}
assert.throws(() => compactYjsDocument({
  updates: [{ seq: 1, kind: "subdoc-update", update: new Uint8Array([1, 2, 3]) }],
}), /subdoc updates/);

function makeTextUpdates(name: string, inserts: string[]): SequencedYjsUpdate[] {
  const doc = new Y.Doc();
  const out: SequencedYjsUpdate[] = [];
  doc.on("update", (update) => {
    out.push({ seq: out.length + 1, update });
  });
  const text = doc.getText(name);
  for (const value of inserts) {
    text.insert(text.length, value);
  }
  doc.destroy();
  return out;
}

function makeUpdateFromSnapshot(snapshot: Uint8Array, name: string, insert: string): Uint8Array {
  const doc = new Y.Doc();
  let captured: Uint8Array | undefined;
  Y.applyUpdate(doc, snapshot);
  doc.on("update", (update) => {
    captured = update;
  });
  const text = doc.getText(name);
  text.insert(text.length, insert);
  doc.destroy();
  assert.ok(captured);
  return captured;
}

function assertTextSnapshot(snapshot: Uint8Array, name: string, expected: string): void {
  const doc = new Y.Doc();
  Y.applyUpdate(doc, snapshot);
  assert.equal(doc.getText(name).toString(), expected);
  doc.destroy();
}
