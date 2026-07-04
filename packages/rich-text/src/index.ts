import type {
  EnterRoomOptions,
  OpenRTCAdminClient,
  OpenRTCAdminCommentInput,
  OpenRTCAdminCommentReaction,
  OpenRTCAdminRoomSubscriptionSettings,
  OpenRTCAdminThread,
  OpenRTCClient,
  OpenRTCOthersEvent,
  OpenRTCRoom,
  PresencePeer,
  PresenceState,
} from "@openrtc/client";
import type { OpenRTCAwareness, OpenRTCYjsProvider } from "@openrtc/yjs";
import * as Y from "yjs";

export interface TextSelectionPresence extends PresenceState {
  kind: "text-selection";
  editor: "generic" | "tiptap" | "lexical" | "blocknote" | "slate" | "quill" | "codemirror";
  anchor: number;
  head: number;
  from?: number;
  to?: number;
  updatedAt: number;
}

export interface SelectionPresenceTransport {
  updatePresence(room: string, state: PresenceState): string;
}

export interface RichTextOpenRTCSessionOptions {
  client: OpenRTCClient;
  provider: OpenRTCYjsProvider;
  room: string;
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
  cleanup?: (() => void) | undefined;
}

export interface RichTextOpenRTCSession extends RichTextYjsBinding {
  room: string;
  roomHandle: OpenRTCRoom;
  leave(): void;
  getRemoteSelections(options?: RemoteTextSelectionOptions): RemoteTextSelection[];
  subscribeRemoteSelections(
    callback: (selections: RemoteTextSelection[], event: OpenRTCOthersEvent) => void,
    options?: RemoteTextSelectionOptions,
  ): () => void;
  dispose(): void;
}

export type RichTextSelectionSnapshotInput = Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> &
  Record<string, unknown>;

export interface RichTextCommentAnchor {
  kind: "rich-text-selection";
  room: string;
  editor: TextSelectionPresence["editor"];
  field: string;
  textName: string;
  fragmentName: string;
  anchor: number;
  head: number;
  from?: number | undefined;
  to?: number | undefined;
  blockID?: string | undefined;
  updatedAt: number;
}

export interface RichTextCanvasProductOptions {
  admin?: OpenRTCAdminClient | undefined;
  userId: string;
  readCommentSelection?: (() => RichTextSelectionSnapshotInput | null) | undefined;
}

export interface RichTextOpenRTCCanvasOptions
  extends RichTextOpenRTCSessionOptions,
    RichTextYjsBindingOptions,
    RichTextCanvasProductOptions {
  editor?: TextSelectionPresence["editor"] | undefined;
}

export interface RichTextCanvasCommentInput {
  text?: string | undefined;
  body?: unknown;
  metadata?: unknown;
  userId?: string | undefined;
  mentions?: string[] | undefined;
  reactions?: OpenRTCAdminCommentReaction[] | undefined;
  selection?: RichTextCommentAnchor | RichTextSelectionSnapshotInput | null | undefined;
}

export interface RichTextCanvasThreadInput extends RichTextCanvasCommentInput {
  threadMetadata?: unknown;
}

export interface RichTextOpenRTCCanvas extends RichTextOpenRTCSession {
  admin?: OpenRTCAdminClient | undefined;
  userId: string;
  currentCommentAnchor(): RichTextCommentAnchor | undefined;
  createThread(input: RichTextCanvasThreadInput): Promise<OpenRTCAdminThread>;
  addComment(threadId: string, input: RichTextCanvasCommentInput): Promise<OpenRTCAdminThread>;
  markThreadResolved(threadId: string): Promise<OpenRTCAdminThread>;
  markThreadUnresolved(threadId: string): Promise<OpenRTCAdminThread>;
  subscribeAllThreads(): Promise<OpenRTCAdminRoomSubscriptionSettings>;
  subscribeRepliesAndMentions(): Promise<OpenRTCAdminRoomSubscriptionSettings>;
  muteThreadNotifications(): Promise<OpenRTCAdminRoomSubscriptionSettings>;
}

export interface SelectionPresenceControllerOptions {
  room: string;
  client: SelectionPresenceTransport;
  editor?: TextSelectionPresence["editor"];
  readSelection: () => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null;
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
}

export interface SelectionPresenceController {
  flush(): void;
  dispose(): void;
}

export interface RemoteTextSelection {
  connId: string;
  peer: PresencePeer;
  selection: TextSelectionPresence;
}

export interface RemoteTextSelectionOptions {
  editor?: TextSelectionPresence["editor"] | undefined;
  maxAgeMs?: number | undefined;
  now?: number | undefined;
}

export interface RemoteTextSelectionRoomHandle {
  subscribe(type: "others", callback: (others: PresencePeer[], event: OpenRTCOthersEvent) => void): () => void;
}

export interface RichTextYjsBindingOptions {
  field?: string | undefined;
  text?: string | undefined;
  fragment?: string | undefined;
}

export interface RichTextYjsBinding {
  provider: OpenRTCYjsProvider;
  doc: Y.Doc;
  awareness: OpenRTCAwareness;
  field: string;
  textName: string;
  fragmentName: string;
  text: Y.Text;
  fragment: Y.XmlFragment;
}

export interface TiptapYjsBinding extends RichTextYjsBinding {
  collaboration: {
    document: Y.Doc;
    field: string;
  };
  collaborationCursor: {
    provider: OpenRTCYjsProvider;
    user?: PresenceState | undefined;
  };
}

export interface TiptapYjsBindingOptions extends RichTextYjsBindingOptions {
  user?: PresenceState | undefined;
}

export interface LexicalYjsBinding extends RichTextYjsBinding {
  id: string;
  docMap: Map<string, Y.Doc>;
}

export interface LexicalYjsBindingOptions extends RichTextYjsBindingOptions {
  id?: string | undefined;
}

export interface BlockNoteYjsBinding extends RichTextYjsBinding {
  collaboration: {
    provider: OpenRTCYjsProvider;
    fragment: Y.XmlFragment;
    user?: PresenceState | undefined;
  };
}

export interface BlockNoteYjsBindingOptions extends RichTextYjsBindingOptions {
  user?: PresenceState | undefined;
}

export interface TiptapOpenRTCIntegrationOptions extends TiptapYjsBindingOptions, TiptapPresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor?: TiptapEditorLike | undefined;
  cleanup?: (() => void) | undefined;
}

export interface TiptapOpenRTCIntegration {
  binding: TiptapYjsBinding;
  unbindPresence?: (() => void) | undefined;
  dispose(): void;
}

export interface TiptapOpenRTCSessionOptions extends TiptapOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface TiptapOpenRTCSession extends RichTextOpenRTCSession {
  binding: TiptapYjsBinding;
  integration: TiptapOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface TiptapOpenRTCCanvasOptions extends TiptapOpenRTCSessionOptions, RichTextCanvasProductOptions {}

export interface TiptapOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: TiptapYjsBinding;
  integration: TiptapOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface LexicalOpenRTCIntegrationOptions extends LexicalYjsBindingOptions, LexicalPresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor: LexicalEditorLike;
  cleanup?: (() => void) | undefined;
}

export interface LexicalOpenRTCIntegration {
  binding: LexicalYjsBinding;
  unbindPresence: () => void;
  dispose(): void;
}

export interface LexicalOpenRTCSessionOptions extends LexicalOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface LexicalOpenRTCSession extends RichTextOpenRTCSession {
  binding: LexicalYjsBinding;
  integration: LexicalOpenRTCIntegration;
  unbindPresence: () => void;
}

export interface LexicalOpenRTCCanvasOptions extends LexicalOpenRTCSessionOptions, RichTextCanvasProductOptions {}

export interface LexicalOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: LexicalYjsBinding;
  integration: LexicalOpenRTCIntegration;
  unbindPresence: () => void;
}

export interface BlockNoteOpenRTCIntegrationOptions extends BlockNoteYjsBindingOptions, BlockNotePresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor?: BlockNoteEditorLike | undefined;
  cleanup?: (() => void) | undefined;
}

export interface BlockNoteOpenRTCIntegration {
  binding: BlockNoteYjsBinding;
  unbindPresence?: (() => void) | undefined;
  dispose(): void;
}

export interface BlockNoteOpenRTCSessionOptions extends BlockNoteOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface BlockNoteOpenRTCSession extends RichTextOpenRTCSession {
  binding: BlockNoteYjsBinding;
  integration: BlockNoteOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface BlockNoteOpenRTCCanvasOptions extends BlockNoteOpenRTCSessionOptions, RichTextCanvasProductOptions {}

export interface BlockNoteOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: BlockNoteYjsBinding;
  integration: BlockNoteOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface SlateYjsBinding extends RichTextYjsBinding {
  sharedTypeName: string;
  sharedType: Y.XmlText;
  yjs: {
    sharedType: Y.XmlText;
    provider: OpenRTCYjsProvider;
    user?: PresenceState | undefined;
  };
}

export interface SlateYjsBindingOptions extends RichTextYjsBindingOptions {
  sharedType?: string | undefined;
  user?: PresenceState | undefined;
}

export interface SlateOpenRTCIntegrationOptions extends SlateYjsBindingOptions, SlatePresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor?: SlateEditorLike | undefined;
  cleanup?: (() => void) | undefined;
}

export interface SlateOpenRTCIntegration {
  binding: SlateYjsBinding;
  unbindPresence?: (() => void) | undefined;
  dispose(): void;
}

export interface SlateOpenRTCSessionOptions extends SlateOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface SlateOpenRTCSession extends RichTextOpenRTCSession {
  binding: SlateYjsBinding;
  integration: SlateOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface SlateOpenRTCCanvasOptions extends SlateOpenRTCSessionOptions, RichTextCanvasProductOptions {}

export interface SlateOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: SlateYjsBinding;
  integration: SlateOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface QuillYjsBinding extends RichTextYjsBinding {
  ytext: Y.Text;
  quillBinding: {
    ytext: Y.Text;
    awareness: OpenRTCAwareness;
    user?: PresenceState | undefined;
  };
}

export interface QuillYjsBindingOptions extends RichTextYjsBindingOptions {
  user?: PresenceState | undefined;
}

export interface QuillOpenRTCIntegrationOptions extends QuillYjsBindingOptions, QuillPresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor?: QuillEditorLike | undefined;
  cleanup?: (() => void) | undefined;
}

export interface QuillOpenRTCIntegration {
  binding: QuillYjsBinding;
  unbindPresence?: (() => void) | undefined;
  dispose(): void;
}

export interface QuillOpenRTCSessionOptions extends QuillOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface QuillOpenRTCSession extends RichTextOpenRTCSession {
  binding: QuillYjsBinding;
  integration: QuillOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface QuillOpenRTCCanvasOptions extends QuillOpenRTCSessionOptions, RichTextCanvasProductOptions {}

export interface QuillOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: QuillYjsBinding;
  integration: QuillOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface CodeMirrorYjsBinding extends RichTextYjsBinding {
  ytext: Y.Text;
  codeMirrorBinding: {
    ytext: Y.Text;
    awareness: OpenRTCAwareness;
    user?: PresenceState | undefined;
  };
}

export interface CodeMirrorYjsBindingOptions extends RichTextYjsBindingOptions {
  user?: PresenceState | undefined;
}

export interface CodeMirrorOpenRTCIntegrationOptions extends CodeMirrorYjsBindingOptions, CodeMirrorPresenceOptions {
  provider: OpenRTCYjsProvider;
  client: OpenRTCClient;
  room: string;
  editor?: CodeMirrorEditorLike | undefined;
  cleanup?: (() => void) | undefined;
}

export interface CodeMirrorOpenRTCIntegration {
  binding: CodeMirrorYjsBinding;
  unbindPresence?: (() => void) | undefined;
  dispose(): void;
}

export interface CodeMirrorOpenRTCSessionOptions extends CodeMirrorOpenRTCIntegrationOptions {
  enterRoom?: EnterRoomOptions | false | undefined;
  destroyProvider?: boolean | undefined;
}

export interface CodeMirrorOpenRTCSession extends RichTextOpenRTCSession {
  binding: CodeMirrorYjsBinding;
  integration: CodeMirrorOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export interface CodeMirrorOpenRTCCanvasOptions
  extends CodeMirrorOpenRTCSessionOptions,
    RichTextCanvasProductOptions {}

export interface CodeMirrorOpenRTCCanvas extends RichTextOpenRTCCanvas {
  binding: CodeMirrorYjsBinding;
  integration: CodeMirrorOpenRTCIntegration;
  unbindPresence?: (() => void) | undefined;
}

export function createRichTextYjsBinding(
  provider: OpenRTCYjsProvider,
  options: RichTextYjsBindingOptions = {},
): RichTextYjsBinding {
  const field = options.field ?? "default";
  const textName = options.text ?? `${field}:text`;
  const fragmentName = options.fragment ?? field;
  return {
    provider,
    doc: provider.doc,
    awareness: provider.awareness,
    field,
    textName,
    fragmentName,
    text: provider.doc.getText(textName),
    fragment: provider.doc.getXmlFragment(fragmentName),
  };
}

export function createTiptapYjsBinding(
  provider: OpenRTCYjsProvider,
  options: TiptapYjsBindingOptions = {},
): TiptapYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  return {
    ...binding,
    collaboration: {
      document: binding.doc,
      field: binding.field,
    },
    collaborationCursor: {
      provider,
      user: options.user,
    },
  };
}

export function createLexicalYjsBinding(
  provider: OpenRTCYjsProvider,
  options: LexicalYjsBindingOptions = {},
): LexicalYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  const id = options.id ?? binding.field;
  return {
    ...binding,
    id,
    docMap: new Map([[id, binding.doc]]),
  };
}

export function createBlockNoteYjsBinding(
  provider: OpenRTCYjsProvider,
  options: BlockNoteYjsBindingOptions = {},
): BlockNoteYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  return {
    ...binding,
    collaboration: {
      provider,
      fragment: binding.fragment,
      user: options.user,
    },
  };
}

export function createSlateYjsBinding(
  provider: OpenRTCYjsProvider,
  options: SlateYjsBindingOptions = {},
): SlateYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  const sharedTypeName = options.sharedType ?? `${binding.field}:slate`;
  const sharedType = provider.doc.get(sharedTypeName, Y.XmlText);
  return {
    ...binding,
    sharedTypeName,
    sharedType,
    yjs: {
      sharedType,
      provider,
      user: options.user,
    },
  };
}

export function createQuillYjsBinding(
  provider: OpenRTCYjsProvider,
  options: QuillYjsBindingOptions = {},
): QuillYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  return {
    ...binding,
    ytext: binding.text,
    quillBinding: {
      ytext: binding.text,
      awareness: binding.awareness,
      user: options.user,
    },
  };
}

export function createCodeMirrorYjsBinding(
  provider: OpenRTCYjsProvider,
  options: CodeMirrorYjsBindingOptions = {},
): CodeMirrorYjsBinding {
  const binding = createRichTextYjsBinding(provider, options);
  return {
    ...binding,
    ytext: binding.text,
    codeMirrorBinding: {
      ytext: binding.text,
      awareness: binding.awareness,
      user: options.user,
    },
  };
}

export function createRichTextOpenRTCSession(
  options: RichTextOpenRTCSessionOptions,
  bindingOptions: RichTextYjsBindingOptions = {},
): RichTextOpenRTCSession {
  const binding = createRichTextYjsBinding(options.provider, bindingOptions);
  const entered =
    options.enterRoom === false ? undefined : options.client.enterRoom(options.room, options.enterRoom ?? {});
  const roomHandle = entered?.room ?? options.client.room(options.room);
  const leave = createDisposable(entered?.leave);
  const dispose = createDisposable(
    leave,
    options.destroyProvider === false ? undefined : () => options.provider.destroy(),
    options.cleanup,
  );
  return {
    ...binding,
    room: options.room,
    roomHandle,
    leave,
    getRemoteSelections(remoteOptions = {}) {
      return getRemoteTextSelections(roomHandle.getOthers(), remoteOptions);
    },
    subscribeRemoteSelections(callback, remoteOptions = {}) {
      return subscribeRemoteTextSelections(roomHandle, callback, remoteOptions);
    },
    dispose,
  };
}

export function createRichTextOpenRTCCanvas(options: RichTextOpenRTCCanvasOptions): RichTextOpenRTCCanvas {
  const session = createRichTextOpenRTCSession(options, options);
  return attachRichTextCanvasActions(session, options, options.editor ?? "generic", options.readCommentSelection);
}

export function isTextSelectionPresence(
  state: unknown,
  editor?: TextSelectionPresence["editor"],
): state is TextSelectionPresence {
  if (!isRecord(state)) {
    return false;
  }
  if (state["kind"] !== "text-selection") {
    return false;
  }
  if (!isEditorValue(state["editor"])) {
    return false;
  }
  if (editor && state["editor"] !== editor) {
    return false;
  }
  if (!isFiniteNumber(state["anchor"]) || !isFiniteNumber(state["head"]) || !isFiniteNumber(state["updatedAt"])) {
    return false;
  }
  if (state["from"] !== undefined && !isFiniteNumber(state["from"])) {
    return false;
  }
  if (state["to"] !== undefined && !isFiniteNumber(state["to"])) {
    return false;
  }
  return true;
}

export function getRemoteTextSelections(
  others: readonly PresencePeer[],
  options: RemoteTextSelectionOptions = {},
): RemoteTextSelection[] {
  const now = options.now ?? Date.now();
  return others.flatMap((peer) => {
    const selection = peer.state;
    if (!isTextSelectionPresence(selection, options.editor)) {
      return [];
    }
    if (options.maxAgeMs !== undefined && now - selection.updatedAt > options.maxAgeMs) {
      return [];
    }
    return [{ connId: peer.connId, peer, selection }];
  });
}

export function subscribeRemoteTextSelections(
  roomHandle: RemoteTextSelectionRoomHandle,
  callback: (selections: RemoteTextSelection[], event: OpenRTCOthersEvent) => void,
  options: RemoteTextSelectionOptions = {},
): () => void {
  return roomHandle.subscribe("others", (others, event) => {
    callback(getRemoteTextSelections(others, options), event);
  });
}

export function createSelectionPresenceController(
  options: SelectionPresenceControllerOptions,
): SelectionPresenceController {
  let disposed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const throttleMs = options.throttleMs ?? 50;

  const send = (): void => {
    timer = undefined;
    if (disposed) {
      return;
    }
    const selection = options.readSelection();
    if (!selection) {
      return;
    }
    options.client.updatePresence(options.room, {
      ...options.extraState?.(),
      ...selection,
      kind: "text-selection",
      editor: options.editor ?? "generic",
      updatedAt: Date.now(),
    });
  };

  return {
    flush() {
      if (disposed) {
        return;
      }
      if (throttleMs <= 0) {
        send();
        return;
      }
      if (timer) {
        return;
      }
      timer = setTimeout(send, throttleMs);
    },
    dispose() {
      disposed = true;
      if (timer) {
        clearTimeout(timer);
      }
    },
  };
}

export interface TiptapEditorLike {
  state: {
    selection: {
      anchor?: number;
      head?: number;
      from: number;
      to: number;
    };
  };
  on?(event: "selectionUpdate" | "transaction", handler: () => void): void;
  off?(event: "selectionUpdate" | "transaction", handler: () => void): void;
}

export interface TiptapPresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
}

export function bindTiptapPresence(
  editor: TiptapEditorLike,
  client: OpenRTCClient,
  room: string,
  options: TiptapPresenceOptions = {},
): () => void {
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "tiptap",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection: () => ({
      anchor: editor.state.selection.anchor ?? editor.state.selection.from,
      head: editor.state.selection.head ?? editor.state.selection.to,
      from: editor.state.selection.from,
      to: editor.state.selection.to,
    }),
  });
  const handler = (): void => controller.flush();
  editor.on?.("selectionUpdate", handler);
  editor.on?.("transaction", handler);
  controller.flush();
  return () => {
    editor.off?.("selectionUpdate", handler);
    editor.off?.("transaction", handler);
    controller.dispose();
  };
}

export function createTiptapOpenRTCIntegration(
  options: TiptapOpenRTCIntegrationOptions,
): TiptapOpenRTCIntegration {
  const binding = createTiptapYjsBinding(options.provider, options);
  const unbindPresence = options.editor
    ? bindTiptapPresence(options.editor, options.client, options.room, options)
    : undefined;
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createTiptapOpenRTCSession(options: TiptapOpenRTCSessionOptions): TiptapOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createTiptapOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createTiptapOpenRTCCanvas(options: TiptapOpenRTCCanvasOptions): TiptapOpenRTCCanvas {
  const session = createTiptapOpenRTCSession(options);
  const readSelection = options.readCommentSelection ?? tiptapSelectionReader(options.editor);
  return attachRichTextCanvasActions(session, options, "tiptap", readSelection) as TiptapOpenRTCCanvas;
}

export interface LexicalEditorLike {
  registerUpdateListener(listener: () => void): () => void;
}

export interface LexicalPresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
  readSelection: () => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null;
}

export function bindLexicalPresence(
  editor: LexicalEditorLike,
  client: OpenRTCClient,
  room: string,
  options: LexicalPresenceOptions,
): () => void {
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "lexical",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection: options.readSelection,
  });
  const unregister = editor.registerUpdateListener(() => controller.flush());
  controller.flush();
  return () => {
    unregister();
    controller.dispose();
  };
}

export function createLexicalOpenRTCIntegration(
  options: LexicalOpenRTCIntegrationOptions,
): LexicalOpenRTCIntegration {
  const binding = createLexicalYjsBinding(options.provider, options);
  const unbindPresence = bindLexicalPresence(options.editor, options.client, options.room, options);
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createLexicalOpenRTCSession(options: LexicalOpenRTCSessionOptions): LexicalOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createLexicalOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createLexicalOpenRTCCanvas(options: LexicalOpenRTCCanvasOptions): LexicalOpenRTCCanvas {
  const session = createLexicalOpenRTCSession(options);
  const readSelection = options.readCommentSelection ?? options.readSelection;
  return attachRichTextCanvasActions(session, options, "lexical", readSelection) as LexicalOpenRTCCanvas;
}

export interface BlockNoteEditorLike {
  onSelectionChange?(handler: () => void): () => void;
  on?(event: "selectionChange", handler: () => void): void;
  off?(event: "selectionChange", handler: () => void): void;
  getTextCursorPosition?(): {
    block?: { id?: string };
    prevBlock?: { id?: string };
    nextBlock?: { id?: string };
  };
}

export interface BlockNotePresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
  readSelection?: () => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null;
}

export function bindBlockNotePresence(
  editor: BlockNoteEditorLike,
  client: OpenRTCClient,
  room: string,
  options: BlockNotePresenceOptions = {},
): () => void {
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "blocknote",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection:
      options.readSelection ??
      (() => {
        const cursor = editor.getTextCursorPosition?.();
        const blockID = cursor?.block?.id ?? cursor?.prevBlock?.id ?? cursor?.nextBlock?.id;
        return blockID ? { anchor: 0, head: 0, from: 0, to: 0, blockID } : null;
      }),
  });

  const handler = (): void => controller.flush();
  const unregisterSelection = editor.onSelectionChange?.(handler);
  editor.on?.("selectionChange", handler);
  controller.flush();

  return () => {
    unregisterSelection?.();
    editor.off?.("selectionChange", handler);
    controller.dispose();
  };
}

export function createBlockNoteOpenRTCIntegration(
  options: BlockNoteOpenRTCIntegrationOptions,
): BlockNoteOpenRTCIntegration {
  const binding = createBlockNoteYjsBinding(options.provider, options);
  const unbindPresence = options.editor
    ? bindBlockNotePresence(options.editor, options.client, options.room, options)
    : undefined;
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createBlockNoteOpenRTCSession(options: BlockNoteOpenRTCSessionOptions): BlockNoteOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createBlockNoteOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createBlockNoteOpenRTCCanvas(options: BlockNoteOpenRTCCanvasOptions): BlockNoteOpenRTCCanvas {
  const session = createBlockNoteOpenRTCSession(options);
  const readSelection = options.readCommentSelection ?? options.readSelection ?? blockNoteSelectionReader(options.editor);
  return attachRichTextCanvasActions(session, options, "blocknote", readSelection) as BlockNoteOpenRTCCanvas;
}

export type SlatePointLike = number | { offset?: number; path?: Array<string | number> };

export interface SlateRangeLike {
  anchor?: SlatePointLike | undefined;
  focus?: SlatePointLike | undefined;
}

export interface SlateEditorLike {
  selection?: SlateRangeLike | null | undefined;
  on?(event: "change" | "selectionChange", handler: () => void): void;
  off?(event: "change" | "selectionChange", handler: () => void): void;
}

export interface SlatePresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
  readSelection?: (() => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null) | undefined;
}

export function bindSlatePresence(
  editor: SlateEditorLike,
  client: OpenRTCClient,
  room: string,
  options: SlatePresenceOptions = {},
): () => void {
  const readSelection = options.readSelection ?? slateSelectionReader(editor) ?? (() => null);
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "slate",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection,
  });
  const handler = (): void => controller.flush();
  editor.on?.("change", handler);
  editor.on?.("selectionChange", handler);
  controller.flush();
  return () => {
    editor.off?.("change", handler);
    editor.off?.("selectionChange", handler);
    controller.dispose();
  };
}

export function createSlateOpenRTCIntegration(options: SlateOpenRTCIntegrationOptions): SlateOpenRTCIntegration {
  const binding = createSlateYjsBinding(options.provider, options);
  const unbindPresence = options.editor
    ? bindSlatePresence(options.editor, options.client, options.room, options)
    : undefined;
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createSlateOpenRTCSession(options: SlateOpenRTCSessionOptions): SlateOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createSlateOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createSlateOpenRTCCanvas(options: SlateOpenRTCCanvasOptions): SlateOpenRTCCanvas {
  const session = createSlateOpenRTCSession(options);
  const readSelection = options.readCommentSelection ?? options.readSelection ?? slateSelectionReader(options.editor);
  return attachRichTextCanvasActions(session, options, "slate", readSelection) as SlateOpenRTCCanvas;
}

export interface QuillRangeLike {
  index: number;
  length: number;
}

export interface QuillEditorLike {
  getSelection?(focus?: boolean): QuillRangeLike | null;
  on?(event: "selection-change" | "editor-change", handler: () => void): void;
  off?(event: "selection-change" | "editor-change", handler: () => void): void;
}

export interface QuillPresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
  readSelection?: (() => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null) | undefined;
}

export function bindQuillPresence(
  editor: QuillEditorLike,
  client: OpenRTCClient,
  room: string,
  options: QuillPresenceOptions = {},
): () => void {
  const readSelection = options.readSelection ?? quillSelectionReader(editor) ?? (() => null);
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "quill",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection,
  });
  const handler = (): void => controller.flush();
  editor.on?.("selection-change", handler);
  editor.on?.("editor-change", handler);
  controller.flush();
  return () => {
    editor.off?.("selection-change", handler);
    editor.off?.("editor-change", handler);
    controller.dispose();
  };
}

export function createQuillOpenRTCIntegration(options: QuillOpenRTCIntegrationOptions): QuillOpenRTCIntegration {
  const binding = createQuillYjsBinding(options.provider, options);
  const unbindPresence = options.editor
    ? bindQuillPresence(options.editor, options.client, options.room, options)
    : undefined;
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createQuillOpenRTCSession(options: QuillOpenRTCSessionOptions): QuillOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createQuillOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createQuillOpenRTCCanvas(options: QuillOpenRTCCanvasOptions): QuillOpenRTCCanvas {
  const session = createQuillOpenRTCSession(options);
  const readSelection = options.readCommentSelection ?? options.readSelection ?? quillSelectionReader(options.editor);
  return attachRichTextCanvasActions(session, options, "quill", readSelection) as QuillOpenRTCCanvas;
}

export interface CodeMirrorSelectionRangeLike {
  anchor: number;
  head: number;
  from?: number | undefined;
  to?: number | undefined;
}

export interface CodeMirrorEditorLike {
  state: {
    selection: {
      main: CodeMirrorSelectionRangeLike;
    };
  };
  dom?: {
    addEventListener(type: "keyup" | "mouseup" | "touchend" | "focus", handler: () => void): void;
    removeEventListener(type: "keyup" | "mouseup" | "touchend" | "focus", handler: () => void): void;
  };
}

export interface CodeMirrorPresenceOptions {
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
  readSelection?: (() => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null) | undefined;
  subscribeSelection?: ((handler: () => void) => () => void) | undefined;
}

export function bindCodeMirrorPresence(
  editor: CodeMirrorEditorLike,
  client: OpenRTCClient,
  room: string,
  options: CodeMirrorPresenceOptions = {},
): () => void {
  const readSelection = options.readSelection ?? codeMirrorSelectionReader(editor) ?? (() => null);
  const controller = createSelectionPresenceController({
    room,
    client,
    editor: "codemirror",
    throttleMs: options.throttleMs,
    extraState: options.extraState,
    readSelection,
  });
  const handler = (): void => controller.flush();
  const unsubscribeSelection = options.subscribeSelection?.(handler);
  editor.dom?.addEventListener("keyup", handler);
  editor.dom?.addEventListener("mouseup", handler);
  editor.dom?.addEventListener("touchend", handler);
  editor.dom?.addEventListener("focus", handler);
  controller.flush();
  return () => {
    unsubscribeSelection?.();
    editor.dom?.removeEventListener("keyup", handler);
    editor.dom?.removeEventListener("mouseup", handler);
    editor.dom?.removeEventListener("touchend", handler);
    editor.dom?.removeEventListener("focus", handler);
    controller.dispose();
  };
}

export function createCodeMirrorOpenRTCIntegration(
  options: CodeMirrorOpenRTCIntegrationOptions,
): CodeMirrorOpenRTCIntegration {
  const binding = createCodeMirrorYjsBinding(options.provider, options);
  const unbindPresence = options.editor
    ? bindCodeMirrorPresence(options.editor, options.client, options.room, options)
    : undefined;
  const dispose = createDisposable(unbindPresence, options.cleanup);
  return {
    binding,
    unbindPresence,
    dispose,
  };
}

export function createCodeMirrorOpenRTCSession(options: CodeMirrorOpenRTCSessionOptions): CodeMirrorOpenRTCSession {
  const session = createRichTextOpenRTCSession(sessionLifecycleOptions(options), options);
  const integration = createCodeMirrorOpenRTCIntegration({ ...options, cleanup: undefined });
  const dispose = createDisposable(integration.dispose, session.dispose, options.cleanup);
  return {
    ...session,
    binding: integration.binding,
    integration,
    unbindPresence: integration.unbindPresence,
    dispose,
  };
}

export function createCodeMirrorOpenRTCCanvas(options: CodeMirrorOpenRTCCanvasOptions): CodeMirrorOpenRTCCanvas {
  const session = createCodeMirrorOpenRTCSession(options);
  const readSelection =
    options.readCommentSelection ?? options.readSelection ?? codeMirrorSelectionReader(options.editor);
  return attachRichTextCanvasActions(session, options, "codemirror", readSelection) as CodeMirrorOpenRTCCanvas;
}

function attachRichTextCanvasActions<TSession extends RichTextOpenRTCSession>(
  session: TSession,
  options: RichTextCanvasProductOptions,
  editor: TextSelectionPresence["editor"],
  readSelection: (() => RichTextSelectionSnapshotInput | null) | undefined,
): TSession & RichTextOpenRTCCanvas {
  const currentCommentAnchor = (): RichTextCommentAnchor | undefined =>
    createRichTextCommentAnchor(session, editor, readSelection?.());

  const commentInput = (
    input: RichTextCanvasCommentInput,
    anchor = normalizeRichTextCommentAnchor(session, editor, input.selection, currentCommentAnchor),
  ): OpenRTCAdminCommentInput => {
    const body = input.body ?? richTextCommentBody(input.text, anchor);
    const metadata = richTextCommentMetadata(input.metadata, anchor);
    return compactCommentInput({
      userId: input.userId ?? options.userId,
      body,
      metadata,
      mentions: input.mentions,
      reactions: input.reactions,
    });
  };

  const requireAdmin = (): OpenRTCAdminClient => {
    if (!options.admin) {
      throw new Error("Rich text canvas actions require an OpenRTCAdminClient");
    }
    return options.admin;
  };

  return {
    ...session,
    admin: options.admin,
    userId: options.userId,
    currentCommentAnchor,
    createThread(input: RichTextCanvasThreadInput): Promise<OpenRTCAdminThread> {
      const anchor = normalizeRichTextCommentAnchor(session, editor, input.selection, currentCommentAnchor);
      const comment = commentInput(input, anchor);
      const threadMetadata =
        input.threadMetadata === undefined
          ? richTextCommentMetadata(undefined, anchor)
          : input.threadMetadata;
      return requireAdmin().createThread(session.room, {
        ...(threadMetadata !== undefined ? { metadata: threadMetadata } : {}),
        comment,
      });
    },
    addComment(threadId: string, input: RichTextCanvasCommentInput): Promise<OpenRTCAdminThread> {
      return requireAdmin().addComment(session.room, threadId, commentInput(input));
    },
    markThreadResolved(threadId: string): Promise<OpenRTCAdminThread> {
      return requireAdmin().markThreadResolved(session.room, threadId);
    },
    markThreadUnresolved(threadId: string): Promise<OpenRTCAdminThread> {
      return requireAdmin().markThreadUnresolved(session.room, threadId);
    },
    subscribeAllThreads(): Promise<OpenRTCAdminRoomSubscriptionSettings> {
      return requireAdmin().subscribeRoomThreads(session.room, options.userId);
    },
    subscribeRepliesAndMentions(): Promise<OpenRTCAdminRoomSubscriptionSettings> {
      return requireAdmin().subscribeRoomRepliesAndMentions(session.room, options.userId);
    },
    muteThreadNotifications(): Promise<OpenRTCAdminRoomSubscriptionSettings> {
      return requireAdmin().muteRoomThreads(session.room, options.userId);
    },
  };
}

function compactCommentInput(input: {
  userId: string;
  body: unknown;
  metadata: unknown;
  mentions: string[] | undefined;
  reactions: OpenRTCAdminCommentReaction[] | undefined;
}): OpenRTCAdminCommentInput {
  return {
    userId: input.userId,
    body: input.body,
    ...(input.metadata !== undefined ? { metadata: input.metadata } : {}),
    ...(input.mentions !== undefined ? { mentions: input.mentions } : {}),
    ...(input.reactions !== undefined ? { reactions: input.reactions } : {}),
  };
}

function richTextCommentBody(text: string | undefined, anchor: RichTextCommentAnchor | undefined): unknown {
  if (text === undefined) {
    throw new Error("Rich text canvas comments require text or a custom body");
  }
  return {
    type: "rich-text-comment",
    text,
    ...(anchor ? { anchor } : {}),
  };
}

function richTextCommentMetadata(
  metadata: unknown,
  anchor: RichTextCommentAnchor | undefined,
): unknown {
  if (!anchor) {
    return metadata;
  }
  if (metadata === undefined) {
    return { openrtcRichText: { anchor } };
  }
  if (isRecord(metadata) && metadata["openrtcRichText"] === undefined) {
    return {
      ...metadata,
      openrtcRichText: { anchor },
    };
  }
  return metadata;
}

function normalizeRichTextCommentAnchor(
  binding: RichTextYjsBinding & { room: string },
  editor: TextSelectionPresence["editor"],
  selection: RichTextCommentAnchor | RichTextSelectionSnapshotInput | null | undefined,
  fallback: () => RichTextCommentAnchor | undefined,
): RichTextCommentAnchor | undefined {
  if (selection === null) {
    return undefined;
  }
  if (selection === undefined) {
    return fallback();
  }
  if (isRichTextCommentAnchor(selection)) {
    return selection;
  }
  return createRichTextCommentAnchor(binding, editor, selection);
}

function createRichTextCommentAnchor(
  binding: RichTextYjsBinding & { room: string },
  editor: TextSelectionPresence["editor"],
  selection: RichTextSelectionSnapshotInput | null | undefined,
): RichTextCommentAnchor | undefined {
  if (!selection || !isFiniteNumber(selection.anchor) || !isFiniteNumber(selection.head)) {
    return undefined;
  }
  const from = isFiniteNumber(selection.from) ? selection.from : undefined;
  const to = isFiniteNumber(selection.to) ? selection.to : undefined;
  const blockID = typeof selection["blockID"] === "string" ? selection["blockID"] : undefined;
  return {
    kind: "rich-text-selection",
    room: binding.room,
    editor,
    field: binding.field,
    textName: binding.textName,
    fragmentName: binding.fragmentName,
    anchor: selection.anchor,
    head: selection.head,
    ...(from !== undefined ? { from } : {}),
    ...(to !== undefined ? { to } : {}),
    ...(blockID !== undefined ? { blockID } : {}),
    updatedAt: Date.now(),
  };
}

function isRichTextCommentAnchor(value: unknown): value is RichTextCommentAnchor {
  return isRecord(value) && value["kind"] === "rich-text-selection";
}

function tiptapSelectionReader(
  editor: TiptapEditorLike | undefined,
): (() => RichTextSelectionSnapshotInput | null) | undefined {
  if (!editor) {
    return undefined;
  }
  return () => ({
    anchor: editor.state.selection.anchor ?? editor.state.selection.from,
    head: editor.state.selection.head ?? editor.state.selection.to,
    from: editor.state.selection.from,
    to: editor.state.selection.to,
  });
}

function blockNoteSelectionReader(
  editor: BlockNoteEditorLike | undefined,
): (() => RichTextSelectionSnapshotInput | null) | undefined {
  if (!editor) {
    return undefined;
  }
  return () => {
    const cursor = editor.getTextCursorPosition?.();
    const blockID = cursor?.block?.id ?? cursor?.prevBlock?.id ?? cursor?.nextBlock?.id;
    return blockID ? { anchor: 0, head: 0, from: 0, to: 0, blockID } : null;
  };
}

function slateSelectionReader(
  editor: SlateEditorLike | undefined,
): (() => RichTextSelectionSnapshotInput | null) | undefined {
  if (!editor) {
    return undefined;
  }
  return () => {
    const selection = editor.selection;
    if (!selection) {
      return null;
    }
    const anchor = slatePointOffset(selection.anchor);
    const head = slatePointOffset(selection.focus);
    if (anchor === undefined || head === undefined) {
      return null;
    }
    const from = Math.min(anchor, head);
    const to = Math.max(anchor, head);
    const blockID = slatePointBlockID(selection.anchor) ?? slatePointBlockID(selection.focus);
    return {
      anchor,
      head,
      from,
      to,
      ...(blockID ? { blockID } : {}),
    };
  };
}

function quillSelectionReader(
  editor: QuillEditorLike | undefined,
): (() => RichTextSelectionSnapshotInput | null) | undefined {
  if (!editor) {
    return undefined;
  }
  return () => {
    const range = editor.getSelection?.() ?? null;
    if (!range || !isFiniteNumber(range.index) || !isFiniteNumber(range.length)) {
      return null;
    }
    const anchor = range.index;
    const head = range.index + range.length;
    return {
      anchor,
      head,
      from: Math.min(anchor, head),
      to: Math.max(anchor, head),
    };
  };
}

function codeMirrorSelectionReader(
  editor: CodeMirrorEditorLike | undefined,
): (() => RichTextSelectionSnapshotInput | null) | undefined {
  if (!editor) {
    return undefined;
  }
  return () => {
    const range = editor.state.selection.main;
    if (!isFiniteNumber(range.anchor) || !isFiniteNumber(range.head)) {
      return null;
    }
    return {
      anchor: range.anchor,
      head: range.head,
      from: isFiniteNumber(range.from) ? range.from : Math.min(range.anchor, range.head),
      to: isFiniteNumber(range.to) ? range.to : Math.max(range.anchor, range.head),
    };
  };
}

function slatePointOffset(point: SlatePointLike | undefined): number | undefined {
  if (typeof point === "number" && Number.isFinite(point)) {
    return point;
  }
  if (isRecord(point) && isFiniteNumber(point["offset"])) {
    return point["offset"];
  }
  return undefined;
}

function slatePointBlockID(point: SlatePointLike | undefined): string | undefined {
  if (!isRecord(point) || !Array.isArray(point["path"])) {
    return undefined;
  }
  return point["path"].map(String).join("/");
}

function createDisposable(...callbacks: Array<(() => void) | undefined>): () => void {
  let disposed = false;
  return () => {
    if (disposed) {
      return;
    }
    disposed = true;
    for (const callback of callbacks) {
      callback?.();
    }
  };
}

function sessionLifecycleOptions(options: RichTextOpenRTCSessionOptions): RichTextOpenRTCSessionOptions {
  return {
    client: options.client,
    provider: options.provider,
    room: options.room,
    ...(options.enterRoom !== undefined ? { enterRoom: options.enterRoom } : {}),
    ...(options.destroyProvider !== undefined ? { destroyProvider: options.destroyProvider } : {}),
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function isEditorValue(value: unknown): value is TextSelectionPresence["editor"] {
  return (
    value === "generic" ||
    value === "tiptap" ||
    value === "lexical" ||
    value === "blocknote" ||
    value === "slate" ||
    value === "quill" ||
    value === "codemirror"
  );
}
