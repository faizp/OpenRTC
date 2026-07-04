import type {
  EnterRoomOptions,
  OpenRTCClient,
  OpenRTCOthersEvent,
  OpenRTCRoom,
  PresencePeer,
  PresenceState,
} from "@openrtc/client";
import type { OpenRTCAwareness, OpenRTCYjsProvider } from "@openrtc/yjs";
import type * as Y from "yjs";

export interface TextSelectionPresence extends PresenceState {
  kind: "text-selection";
  editor: "generic" | "tiptap" | "lexical" | "blocknote";
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
  return value === "generic" || value === "tiptap" || value === "lexical" || value === "blocknote";
}
