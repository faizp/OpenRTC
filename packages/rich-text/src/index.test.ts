import assert from "node:assert/strict";
import * as Y from "yjs";
import { OpenRTCAwareness } from "@openrtc/yjs";
import {
  bindBlockNotePresence,
  bindLexicalPresence,
  bindTiptapPresence,
  createBlockNoteOpenRTCIntegration,
  createBlockNoteYjsBinding,
  createLexicalOpenRTCIntegration,
  createLexicalYjsBinding,
  createRichTextYjsBinding,
  createTiptapOpenRTCIntegration,
  createTiptapYjsBinding,
  getRemoteTextSelections,
  isTextSelectionPresence,
  subscribeRemoteTextSelections,
} from "./index.ts";
import { useRemoteTextSelections, useSelectionPresenceController } from "./react.ts";
import type { PresenceState } from "@openrtc/client";

assert.equal(typeof useRemoteTextSelections, "function");
assert.equal(typeof useSelectionPresenceController, "function");

class FakeClient {
  updates: Array<{ room: string; state: PresenceState }> = [];

  updatePresence(room: string, state: PresenceState): string {
    this.updates.push({ room, state });
    return "presence-1";
  }
}

const doc = new Y.Doc();
const provider = {
  doc,
  awareness: new OpenRTCAwareness(doc),
};

const richTextBinding = createRichTextYjsBinding(provider as never, { field: "body" });
assert.equal(richTextBinding.doc, doc);
assert.equal(richTextBinding.awareness, provider.awareness);
assert.equal(richTextBinding.textName, "body:text");
assert.equal(richTextBinding.fragmentName, "body");
assert.equal(richTextBinding.text, doc.getText("body:text"));
assert.equal(richTextBinding.fragment, doc.getXmlFragment("body"));

const tiptapBinding = createTiptapYjsBinding(provider as never, {
  field: "article",
  user: { name: "Ada" },
});
assert.equal(tiptapBinding.collaboration.document, doc);
assert.equal(tiptapBinding.collaboration.field, "article");
assert.equal(tiptapBinding.collaborationCursor.provider, provider);
assert.equal(tiptapBinding.collaborationCursor.user?.["name"], "Ada");

const lexicalBinding = createLexicalYjsBinding(provider as never, { field: "lexical", id: "doc-1" });
assert.equal(lexicalBinding.id, "doc-1");
assert.equal(lexicalBinding.docMap.get("doc-1"), doc);

const blockNoteBinding = createBlockNoteYjsBinding(provider as never, { fragment: "blocks" });
assert.equal(blockNoteBinding.fragment, doc.getXmlFragment("blocks"));
assert.equal(blockNoteBinding.collaboration.provider, provider);

const tiptapHandlers = new Map<string, () => void>();
const tiptap = {
  state: {
    selection: { anchor: 1, head: 4, from: 1, to: 4 },
  },
  on(event: "selectionUpdate" | "transaction", handler: () => void) {
    tiptapHandlers.set(event, handler);
  },
  off(event: "selectionUpdate" | "transaction") {
    tiptapHandlers.delete(event);
  },
};

const tiptapClient = new FakeClient();
const unbindTiptap = bindTiptapPresence(tiptap, tiptapClient as never, "tenant-a:doc", { throttleMs: 0 });
tiptap.state.selection = { anchor: 2, head: 8, from: 2, to: 8 };
tiptapHandlers.get("selectionUpdate")?.();
assert.equal(tiptapClient.updates.at(-1)?.state["editor"], "tiptap");
assert.equal(tiptapClient.updates.at(-1)?.state["from"], 2);
unbindTiptap();
assert.equal(tiptapHandlers.size, 0);

let lexicalListener: (() => void) | undefined;
const lexicalClient = new FakeClient();
const unbindLexical = bindLexicalPresence(
  {
    registerUpdateListener(listener: () => void) {
      lexicalListener = listener;
      return () => {
        lexicalListener = undefined;
      };
    },
  },
  lexicalClient as never,
  "tenant-a:doc",
  {
    throttleMs: 0,
    readSelection: () => ({ anchor: 3, head: 3, from: 3, to: 3 }),
  },
);
lexicalListener?.();
assert.equal(lexicalClient.updates.at(-1)?.state["editor"], "lexical");
unbindLexical();
assert.equal(lexicalListener, undefined);

let blockNoteHandler: (() => void) | undefined;
const blockNoteClient = new FakeClient();
const unbindBlockNote = bindBlockNotePresence(
  {
    onSelectionChange(handler: () => void) {
      blockNoteHandler = handler;
      return () => {
        blockNoteHandler = undefined;
      };
    },
    getTextCursorPosition() {
      return { block: { id: "block-1" } };
    },
  },
  blockNoteClient as never,
  "tenant-a:doc",
  { throttleMs: 0 },
);
blockNoteHandler?.();
assert.equal(blockNoteClient.updates.at(-1)?.state["editor"], "blocknote");
assert.equal(blockNoteClient.updates.at(-1)?.state["blockID"], "block-1");
unbindBlockNote();
assert.equal(blockNoteHandler, undefined);

const remotePeers = [
  {
    connId: "peer-tiptap",
    state: {
      kind: "text-selection",
      editor: "tiptap",
      anchor: 1,
      head: 3,
      from: 1,
      to: 3,
      updatedAt: 1_000,
      user: { id: "user-2" },
    },
  },
  {
    connId: "peer-lexical",
    state: {
      kind: "text-selection",
      editor: "lexical",
      anchor: 5,
      head: 2,
      from: 2,
      to: 5,
      updatedAt: 400,
    },
  },
  {
    connId: "peer-cursor",
    state: { cursor: { x: 1, y: 2 } },
  },
];

assert.equal(isTextSelectionPresence(remotePeers[0]?.state), true);
assert.equal(isTextSelectionPresence({ kind: "text-selection", editor: "slate", anchor: 1, head: 1, updatedAt: 1 }), false);
assert.deepEqual(
  getRemoteTextSelections(remotePeers, { now: 1_100, maxAgeMs: 500 }).map((entry) => entry.connId),
  ["peer-tiptap"],
);
assert.deepEqual(
  getRemoteTextSelections(remotePeers, { editor: "lexical", now: 1_100 }).map((entry) => entry.selection.editor),
  ["lexical"],
);

let subscribedCallback:
  | ((others: typeof remotePeers, event: { type: "reset" }) => void)
  | undefined;
let unsubscribed = false;
const unsubscribeSelections = subscribeRemoteTextSelections(
  {
    subscribe(type, callback) {
      assert.equal(type, "others");
      subscribedCallback = callback as (others: typeof remotePeers, event: { type: "reset" }) => void;
      return () => {
        unsubscribed = true;
      };
    },
  },
  (selections, event) => {
    assert.equal(event.type, "reset");
    assert.deepEqual(
      selections.map((entry) => entry.connId),
      ["peer-tiptap", "peer-lexical"],
    );
  },
);
subscribedCallback?.(remotePeers, { type: "reset" });
unsubscribeSelections();
assert.equal(unsubscribed, true);

let tiptapCleanupCount = 0;
const tiptapIntegrationClient = new FakeClient();
const tiptapIntegration = createTiptapOpenRTCIntegration({
  provider: provider as never,
  client: tiptapIntegrationClient as never,
  room: "tenant-a:doc",
  field: "article",
  editor: tiptap,
  throttleMs: 0,
  cleanup: () => {
    tiptapCleanupCount += 1;
  },
});
assert.equal(tiptapIntegration.binding.field, "article");
assert.equal(tiptapHandlers.size, 2);
tiptapIntegration.dispose();
tiptapIntegration.dispose();
assert.equal(tiptapHandlers.size, 0);
assert.equal(tiptapCleanupCount, 1);

let lexicalIntegrationListener: (() => void) | undefined;
let lexicalCleanupCount = 0;
const lexicalIntegration = createLexicalOpenRTCIntegration({
  provider: provider as never,
  client: new FakeClient() as never,
  room: "tenant-a:doc",
  editor: {
    registerUpdateListener(listener: () => void) {
      lexicalIntegrationListener = listener;
      return () => {
        lexicalIntegrationListener = undefined;
      };
    },
  },
  readSelection: () => ({ anchor: 7, head: 7, from: 7, to: 7 }),
  cleanup: () => {
    lexicalCleanupCount += 1;
  },
});
assert.equal(lexicalIntegration.binding.id, "default");
assert.equal(typeof lexicalIntegrationListener, "function");
lexicalIntegration.dispose();
lexicalIntegration.dispose();
assert.equal(lexicalIntegrationListener, undefined);
assert.equal(lexicalCleanupCount, 1);

let blockNoteIntegrationHandler: (() => void) | undefined;
let blockNoteCleanupCount = 0;
const blockNoteIntegration = createBlockNoteOpenRTCIntegration({
  provider: provider as never,
  client: new FakeClient() as never,
  room: "tenant-a:doc",
  editor: {
    onSelectionChange(handler: () => void) {
      blockNoteIntegrationHandler = handler;
      return () => {
        blockNoteIntegrationHandler = undefined;
      };
    },
    getTextCursorPosition() {
      return { block: { id: "block-2" } };
    },
  },
  cleanup: () => {
    blockNoteCleanupCount += 1;
  },
});
assert.equal(blockNoteIntegration.binding.field, "default");
assert.equal(typeof blockNoteIntegrationHandler, "function");
blockNoteIntegration.dispose();
blockNoteIntegration.dispose();
assert.equal(blockNoteIntegrationHandler, undefined);
assert.equal(blockNoteCleanupCount, 1);
