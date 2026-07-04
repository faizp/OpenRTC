import assert from "node:assert/strict";
import * as Y from "yjs";
import { OpenRTCAwareness } from "@openrtc/yjs";
import {
  bindBlockNotePresence,
  bindLexicalPresence,
  bindTiptapPresence,
  createBlockNoteOpenRTCIntegration,
  createBlockNoteOpenRTCCanvas,
  createBlockNoteOpenRTCSession,
  createBlockNoteYjsBinding,
  createLexicalOpenRTCIntegration,
  createLexicalOpenRTCCanvas,
  createLexicalOpenRTCSession,
  createLexicalYjsBinding,
  createRichTextOpenRTCCanvas,
  createRichTextOpenRTCSession,
  createRichTextYjsBinding,
  createTiptapOpenRTCIntegration,
  createTiptapOpenRTCCanvas,
  createTiptapOpenRTCSession,
  createTiptapYjsBinding,
  getRemoteTextSelections,
  isTextSelectionPresence,
  subscribeRemoteTextSelections,
} from "./index.ts";
import {
  useRemoteTextSelections,
  useRichTextSessionRemoteSelections,
  useSelectionPresenceController,
} from "./react.ts";
import type { OpenRTCOthersEvent, PresencePeer, PresenceState } from "@openrtc/client";

assert.equal(typeof useRemoteTextSelections, "function");
assert.equal(typeof useRichTextSessionRemoteSelections, "function");
assert.equal(typeof useSelectionPresenceController, "function");

class FakeRoomHandle {
  readonly subscribers = new Set<(others: PresencePeer[], event: OpenRTCOthersEvent) => void>();
  others: PresencePeer[] = [];

  getOthers(): PresencePeer[] {
    return this.others;
  }

  subscribe(type: "others", callback: (others: PresencePeer[], event: OpenRTCOthersEvent) => void): () => void {
    assert.equal(type, "others");
    this.subscribers.add(callback);
    return () => {
      this.subscribers.delete(callback);
    };
  }

  emitOthers(event: OpenRTCOthersEvent = { type: "reset" }): void {
    for (const subscriber of this.subscribers) {
      subscriber(this.others, event);
    }
  }
}

class FakeClient {
  updates: Array<{ room: string; state: PresenceState }> = [];
  roomHandle = new FakeRoomHandle();
  entered: Array<{ room: string; options: unknown }> = [];
  leaveCount = 0;

  updatePresence(room: string, state: PresenceState): string {
    this.updates.push({ room, state });
    return "presence-1";
  }

  enterRoom(room: string, options: unknown = {}): { room: FakeRoomHandle; leave: () => void } {
    this.entered.push({ room, options });
    return {
      room: this.roomHandle,
      leave: () => {
        this.leaveCount += 1;
      },
    };
  }

  room(_room: string): FakeRoomHandle {
    return this.roomHandle;
  }
}

class FakeAdmin {
  calls: Array<{ method: string; room: string; userId?: string; threadId?: string; input?: unknown }> = [];

  async createThread(
    room: string,
    input: { comment: { userId: string; body: unknown; metadata?: unknown }; metadata?: unknown },
  ) {
    this.calls.push({ method: "createThread", room, input });
    return {
      type: "thread",
      id: "thread-1",
      roomId: room,
      comments: [
        {
          type: "comment",
          id: "comment-1",
          threadId: "thread-1",
          roomId: room,
          userId: input.comment.userId,
          createdAt: "2026-07-04T00:00:00.000Z",
          body: input.comment.body,
          metadata: input.comment.metadata,
        },
      ],
      resolved: false,
      metadata: input.metadata,
      createdAt: "2026-07-04T00:00:00.000Z",
      updatedAt: "2026-07-04T00:00:00.000Z",
    };
  }

  async addComment(
    room: string,
    threadId: string,
    input: { userId: string; body: unknown; metadata?: unknown },
  ) {
    this.calls.push({ method: "addComment", room, threadId, input });
    return {
      type: "thread",
      id: threadId,
      roomId: room,
      comments: [
        {
          type: "comment",
          id: "comment-2",
          threadId,
          roomId: room,
          userId: input.userId,
          createdAt: "2026-07-04T00:01:00.000Z",
          body: input.body,
          metadata: input.metadata,
        },
      ],
      resolved: false,
      createdAt: "2026-07-04T00:00:00.000Z",
      updatedAt: "2026-07-04T00:01:00.000Z",
    };
  }

  async markThreadResolved(room: string, threadId: string) {
    this.calls.push({ method: "markThreadResolved", room, threadId });
    return {
      type: "thread",
      id: threadId,
      roomId: room,
      comments: [],
      resolved: true,
      createdAt: "2026-07-04T00:00:00.000Z",
      updatedAt: "2026-07-04T00:02:00.000Z",
    };
  }

  async markThreadUnresolved(room: string, threadId: string) {
    this.calls.push({ method: "markThreadUnresolved", room, threadId });
    return {
      type: "thread",
      id: threadId,
      roomId: room,
      comments: [],
      resolved: false,
      createdAt: "2026-07-04T00:00:00.000Z",
      updatedAt: "2026-07-04T00:03:00.000Z",
    };
  }

  async subscribeRoomThreads(room: string, userId: string) {
    this.calls.push({ method: "subscribeRoomThreads", room, userId });
    return { roomId: room, userId, threads: "all" as const, textMentions: "mine" as const };
  }

  async subscribeRoomRepliesAndMentions(room: string, userId: string) {
    this.calls.push({ method: "subscribeRoomRepliesAndMentions", room, userId });
    return { roomId: room, userId, threads: "replies_and_mentions" as const, textMentions: "mine" as const };
  }

  async muteRoomThreads(room: string, userId: string) {
    this.calls.push({ method: "muteRoomThreads", room, userId });
    return { roomId: room, userId, threads: "none" as const, textMentions: "none" as const };
  }
}

const doc = new Y.Doc();
let providerDestroyCount = 0;
const provider = {
  doc,
  awareness: new OpenRTCAwareness(doc),
  destroy() {
    providerDestroyCount += 1;
  },
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

const sessionClient = new FakeClient();
sessionClient.roomHandle.others = remotePeers;
let sessionCleanupCount = 0;
const richTextSession = createRichTextOpenRTCSession({
  provider: provider as never,
  client: sessionClient as never,
  room: "tenant-a:doc",
  enterRoom: { initialPresence: { user: { id: "user-1" } } },
  cleanup: () => {
    sessionCleanupCount += 1;
  },
});
assert.equal(richTextSession.roomHandle, sessionClient.roomHandle);
assert.equal(richTextSession.room, "tenant-a:doc");
assert.equal(sessionClient.entered[0]?.room, "tenant-a:doc");
assert.deepEqual(richTextSession.getRemoteSelections({ editor: "tiptap" }).map((entry) => entry.connId), [
  "peer-tiptap",
]);
let sessionSelectionEvent: OpenRTCOthersEvent | undefined;
const unsubscribeSessionSelections = richTextSession.subscribeRemoteSelections((selections, event) => {
  sessionSelectionEvent = event;
  assert.deepEqual(
    selections.map((entry) => entry.connId),
    ["peer-tiptap", "peer-lexical"],
  );
});
sessionClient.roomHandle.emitOthers({ type: "reset" });
assert.deepEqual(sessionSelectionEvent, { type: "reset" });
unsubscribeSessionSelections();
assert.equal(sessionClient.roomHandle.subscribers.size, 0);
richTextSession.leave();
richTextSession.leave();
assert.equal(sessionClient.leaveCount, 1);
richTextSession.dispose();
richTextSession.dispose();
assert.equal(providerDestroyCount, 1);
assert.equal(sessionCleanupCount, 1);

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

let tiptapSessionCleanupCount = 0;
const tiptapSessionClient = new FakeClient();
const tiptapSession = createTiptapOpenRTCSession({
  provider: provider as never,
  client: tiptapSessionClient as never,
  room: "tenant-a:doc",
  field: "session-article",
  editor: tiptap,
  throttleMs: 0,
  destroyProvider: false,
  cleanup: () => {
    tiptapSessionCleanupCount += 1;
  },
});
assert.equal(tiptapSession.binding.field, "session-article");
assert.equal(tiptapSession.integration.binding, tiptapSession.binding);
assert.equal(tiptapHandlers.size, 2);
tiptapSession.dispose();
tiptapSession.dispose();
assert.equal(tiptapHandlers.size, 0);
assert.equal(tiptapSessionClient.leaveCount, 1);
assert.equal(tiptapSessionCleanupCount, 1);
assert.equal(providerDestroyCount, 1);

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

let lexicalSessionListener: (() => void) | undefined;
let lexicalSessionCleanupCount = 0;
const lexicalSessionClient = new FakeClient();
const lexicalSession = createLexicalOpenRTCSession({
  provider: provider as never,
  client: lexicalSessionClient as never,
  room: "tenant-a:doc",
  id: "lexical-session",
  editor: {
    registerUpdateListener(listener: () => void) {
      lexicalSessionListener = listener;
      return () => {
        lexicalSessionListener = undefined;
      };
    },
  },
  readSelection: () => ({ anchor: 9, head: 9, from: 9, to: 9 }),
  destroyProvider: false,
  cleanup: () => {
    lexicalSessionCleanupCount += 1;
  },
});
assert.equal(lexicalSession.binding.id, "lexical-session");
assert.equal(typeof lexicalSessionListener, "function");
lexicalSession.dispose();
lexicalSession.dispose();
assert.equal(lexicalSessionListener, undefined);
assert.equal(lexicalSessionClient.leaveCount, 1);
assert.equal(lexicalSessionCleanupCount, 1);

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

let blockNoteSessionHandler: (() => void) | undefined;
let blockNoteSessionCleanupCount = 0;
const blockNoteSessionClient = new FakeClient();
const blockNoteSession = createBlockNoteOpenRTCSession({
  provider: provider as never,
  client: blockNoteSessionClient as never,
  room: "tenant-a:doc",
  fragment: "session-blocks",
  editor: {
    onSelectionChange(handler: () => void) {
      blockNoteSessionHandler = handler;
      return () => {
        blockNoteSessionHandler = undefined;
      };
    },
    getTextCursorPosition() {
      return { block: { id: "block-3" } };
    },
  },
  destroyProvider: false,
  cleanup: () => {
    blockNoteSessionCleanupCount += 1;
  },
});
assert.equal(blockNoteSession.binding.fragmentName, "session-blocks");
assert.equal(typeof blockNoteSessionHandler, "function");
blockNoteSession.dispose();
blockNoteSession.dispose();
assert.equal(blockNoteSessionHandler, undefined);
assert.equal(blockNoteSessionClient.leaveCount, 1);
assert.equal(blockNoteSessionCleanupCount, 1);

const canvasDoc = new Y.Doc();
let canvasDestroyCount = 0;
const canvasProvider = {
  doc: canvasDoc,
  awareness: new OpenRTCAwareness(canvasDoc),
  destroy() {
    canvasDestroyCount += 1;
  },
};
const canvasAdmin = new FakeAdmin();
const canvasClient = new FakeClient();
const canvas = createRichTextOpenRTCCanvas({
  provider: canvasProvider as never,
  client: canvasClient as never,
  admin: canvasAdmin as never,
  room: "tenant-a:doc-canvas",
  userId: "user-1",
  field: "body",
  readCommentSelection: () => ({ anchor: 10, head: 14, from: 10, to: 14 }),
});
const canvasAnchor = canvas.currentCommentAnchor();
assert.equal(canvasAnchor?.kind, "rich-text-selection");
assert.equal(canvasAnchor?.editor, "generic");
assert.equal(canvasAnchor?.field, "body");
assert.equal(canvasAnchor?.textName, "body:text");
const createdCanvasThread = await canvas.createThread({ text: "Review this", mentions: ["user-2"] });
assert.equal(createdCanvasThread.id, "thread-1");
const createdCanvasComment = createdCanvasThread.comments[0];
assert.ok(createdCanvasComment);
assert.equal((createdCanvasComment.body as { type?: string }).type, "rich-text-comment");
assert.equal(
  (((createdCanvasComment.metadata as { openrtcRichText?: { anchor?: { anchor?: number } } }).openrtcRichText?.anchor)
    ?.anchor),
  10,
);
assert.equal(
  ((createdCanvasThread.metadata as { openrtcRichText?: { anchor?: { field?: string } } }).openrtcRichText?.anchor)
    ?.field,
  "body",
);
const repliedCanvasThread = await canvas.addComment("thread-1", {
  text: "Reply",
  metadata: { status: "open" },
  selection: { anchor: 20, head: 22, from: 20, to: 22 },
});
assert.equal(repliedCanvasThread.comments[0]?.id, "comment-2");
assert.equal(
  ((repliedCanvasThread.comments[0]?.metadata as { openrtcRichText?: { anchor?: { from?: number } } })
    .openrtcRichText?.anchor)?.from,
  20,
);
assert.equal((await canvas.markThreadResolved("thread-1")).resolved, true);
assert.equal((await canvas.markThreadUnresolved("thread-1")).resolved, false);
assert.equal((await canvas.subscribeAllThreads()).threads, "all");
assert.equal((await canvas.subscribeRepliesAndMentions()).threads, "replies_and_mentions");
assert.equal((await canvas.muteThreadNotifications()).threads, "none");
assert.deepEqual(
  canvasAdmin.calls.map((call) => call.method),
  [
    "createThread",
    "addComment",
    "markThreadResolved",
    "markThreadUnresolved",
    "subscribeRoomThreads",
    "subscribeRoomRepliesAndMentions",
    "muteRoomThreads",
  ],
);
canvas.dispose();
canvas.dispose();
assert.equal(canvasClient.leaveCount, 1);
assert.equal(canvasDestroyCount, 1);

const tiptapCanvas = createTiptapOpenRTCCanvas({
  provider: canvasProvider as never,
  client: new FakeClient() as never,
  admin: new FakeAdmin() as never,
  room: "tenant-a:tiptap-canvas",
  userId: "user-1",
  field: "article",
  editor: tiptap,
  destroyProvider: false,
});
assert.equal(tiptapCanvas.binding.field, "article");
assert.equal(tiptapCanvas.currentCommentAnchor()?.editor, "tiptap");
tiptapCanvas.dispose();

const lexicalCanvas = createLexicalOpenRTCCanvas({
  provider: canvasProvider as never,
  client: new FakeClient() as never,
  admin: new FakeAdmin() as never,
  room: "tenant-a:lexical-canvas",
  userId: "user-1",
  id: "lexical-canvas",
  editor: {
    registerUpdateListener() {
      return () => undefined;
    },
  },
  readSelection: () => ({ anchor: 4, head: 6, from: 4, to: 6 }),
  destroyProvider: false,
});
assert.equal(lexicalCanvas.binding.id, "lexical-canvas");
assert.equal(lexicalCanvas.currentCommentAnchor()?.editor, "lexical");
lexicalCanvas.dispose();

const blockNoteCanvas = createBlockNoteOpenRTCCanvas({
  provider: canvasProvider as never,
  client: new FakeClient() as never,
  admin: new FakeAdmin() as never,
  room: "tenant-a:blocknote-canvas",
  userId: "user-1",
  fragment: "canvas-blocks",
  editor: {
    onSelectionChange() {
      return () => undefined;
    },
    getTextCursorPosition() {
      return { block: { id: "block-canvas" } };
    },
  },
  destroyProvider: false,
});
assert.equal(blockNoteCanvas.binding.fragmentName, "canvas-blocks");
assert.equal(blockNoteCanvas.currentCommentAnchor()?.blockID, "block-canvas");
blockNoteCanvas.dispose();
