import type { OpenRTCClient, PresenceState } from "@openrtc/client";
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
