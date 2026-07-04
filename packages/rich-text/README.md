# @openrtc/rich-text

Editor integration helpers for OpenRTC collaborative rich text. The package keeps
durable text content in Yjs and sends editor selection/cursor state through
OpenRTC presence.

Use this package with:

- `@openrtc/client` for room presence and user metadata.
- `@openrtc/yjs` for binary Yjs document sync.
- `yjs` for the shared document.

The helpers are intentionally adapter-shaped and do not depend on Tiptap,
Lexical, or BlockNote packages. Install the editor packages in your app and pass
their editor instances into the helper that matches your editor.

## Shared Setup

```ts
import * as Y from "yjs";
import { OpenRTCClient } from "@openrtc/client";
import { OpenRTCYjsProvider, createIndexedDBYjsStore } from "@openrtc/yjs";

const room = "tenant-a:doc-1";
const user = { id: "user-1", name: "Ada", color: "#0f766e" };

const client = new OpenRTCClient({
  url: "https://openrtc.example.com/ws",
  token: () => fetch("/api/openrtc-token").then((res) => res.text()),
});

await client.connect();
const { room: roomHandle, leave } = client.enterRoom(room, {
  initialPresence: { user },
});

const doc = new Y.Doc();
const provider = new OpenRTCYjsProvider({
  url: "https://openrtc.example.com",
  room,
  token: () => fetch("/api/openrtc-token").then((res) => res.text()),
  doc,
  offlineStore: createIndexedDBYjsStore({ room }),
  presenceClient: client,
  awarenessOptions: {
    extraPresence: () => ({ user }),
  },
});
```

Keep the OpenRTC room handle and the Yjs provider for the lifetime of the editor.
On unmount, call the editor-specific unbind function, `provider.destroy()`, and
`leave()`.

If you want the package to own binding plus presence cleanup, use the
editor-specific integration helpers. They are thin wrappers around the lower
level functions below and still keep editor imports in your app:

```ts
import { createTiptapOpenRTCIntegration } from "@openrtc/rich-text";

const integration = createTiptapOpenRTCIntegration({
  provider,
  client,
  room,
  editor,
  field: "body",
  user,
  extraState: () => ({ user }),
  cleanup: () => {
    provider.destroy();
    leave();
  },
});

// Pass these into Tiptap extensions:
integration.binding.collaboration;
integration.binding.collaborationCursor;

// Cleanup:
integration.dispose();
```

Lexical and BlockNote use the same pattern through
`createLexicalOpenRTCIntegration()` and `createBlockNoteOpenRTCIntegration()`.

## Tiptap

```ts
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCursor from "@tiptap/extension-collaboration-cursor";
import { useEditor } from "@tiptap/react";
import { bindTiptapPresence, createTiptapYjsBinding } from "@openrtc/rich-text";

const binding = createTiptapYjsBinding(provider, {
  field: "body",
  user,
});

const editor = useEditor({
  extensions: [
    Collaboration.configure(binding.collaboration),
    CollaborationCursor.configure(binding.collaborationCursor),
  ],
});

const unbindPresence = editor
  ? bindTiptapPresence(editor, client, room, {
      extraState: () => ({ user }),
    })
  : undefined;

// Cleanup:
unbindPresence?.();
provider.destroy();
leave();
```

Tiptap selection updates are read from `editor.state.selection`. The helper
publishes presence shaped like:

```json
{
  "kind": "text-selection",
  "editor": "tiptap",
  "anchor": 1,
  "head": 4,
  "from": 1,
  "to": 4,
  "updatedAt": 1783040000000
}
```

## Lexical

```ts
import { $getSelection, $isRangeSelection } from "lexical";
import { createLexicalYjsBinding, bindLexicalPresence } from "@openrtc/rich-text";

const binding = createLexicalYjsBinding(provider, {
  field: "body",
  id: room,
});

// Pass binding.docMap to your @lexical/yjs setup.
const docMap = binding.docMap;

const unbindPresence = bindLexicalPresence(lexicalEditor, client, room, {
  extraState: () => ({ user }),
  readSelection: () =>
    lexicalEditor.getEditorState().read(() => {
      const selection = $getSelection();
      if (!$isRangeSelection(selection)) {
        return null;
      }
      const anchor = selection.anchor.offset;
      const head = selection.focus.offset;
      return {
        anchor,
        head,
        from: Math.min(anchor, head),
        to: Math.max(anchor, head),
      };
    }),
});

// Cleanup:
unbindPresence();
provider.destroy();
leave();
```

Lexical exposes selection state through editor reads, so the OpenRTC helper asks
the app for `readSelection`. This keeps Lexical-specific imports in the
application and out of `@openrtc/rich-text`.

## BlockNote

```ts
import { useCreateBlockNote } from "@blocknote/react";
import { bindBlockNotePresence, createBlockNoteYjsBinding } from "@openrtc/rich-text";

const binding = createBlockNoteYjsBinding(provider, {
  field: "body",
  user,
});

const editor = useCreateBlockNote({
  collaboration: binding.collaboration,
});

const unbindPresence = bindBlockNotePresence(editor, client, room, {
  extraState: () => ({ user }),
});

// Cleanup:
unbindPresence();
provider.destroy();
leave();
```

By default, BlockNote selection presence uses
`editor.getTextCursorPosition()` and sends the nearest block ID with the generic
selection fields. If your BlockNote setup exposes richer offsets, pass
`readSelection` to `bindBlockNotePresence`.

## Rendering Remote Selections

Selection presence is normal OpenRTC presence. Render it from a room handle:

```ts
import { getRemoteTextSelections, subscribeRemoteTextSelections } from "@openrtc/rich-text";

const unsubscribe = roomHandle.subscribe("others", (others) => {
  const selections = getRemoteTextSelections(others, {
    maxAgeMs: 30_000,
  });

  renderRemoteSelections(selections);
});

// Or subscribe directly to filtered selections:
const unsubscribeSelections = subscribeRemoteTextSelections(roomHandle, (selections) => {
  renderRemoteSelections(selections);
});
```

Yjs awareness is still available through `provider.awareness` for editor plugins
that expect provider-style awareness. OpenRTC presence remains the source for
app-level user metadata and diagnostics.

The exported `isTextSelectionPresence()` guard is useful when an app already has
its own presence subscription and wants to branch on editor selections inline.
