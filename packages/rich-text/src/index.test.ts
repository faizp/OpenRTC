import assert from "node:assert/strict";
import * as Y from "yjs";
import { OpenRTCAwareness } from "@openrtc/yjs";
import {
  bindBlockNotePresence,
  bindLexicalPresence,
  bindTiptapPresence,
  createBlockNoteYjsBinding,
  createLexicalYjsBinding,
  createRichTextYjsBinding,
  createTiptapYjsBinding,
} from "./index.ts";
import type { PresenceState } from "@openrtc/client";

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
