import type { RemoteTextSelection, RichTextOpenRTCSession, SelectionPresenceController } from "./index.ts";
import {
  useRemoteTextSelections,
  useRichTextSessionRemoteSelections,
  useSelectionPresenceController,
} from "./react.ts";

function expectType<T>(_value: T): void {}

function RichTextReactIntegrationTypes() {
  const selections = useRemoteTextSelections("tenant-a:doc-1", {
    editor: "tiptap",
    maxAgeMs: 30_000,
    limit: 50,
  });
  expectType<RemoteTextSelection[]>(selections);

  const sessionSelections = useRichTextSessionRemoteSelections({} as RichTextOpenRTCSession, {
    editor: "blocknote",
    maxAgeMs: 30_000,
  });
  expectType<RemoteTextSelection[]>(sessionSelections);

  const controller = useSelectionPresenceController({
    room: "tenant-a:doc-1",
    editor: "lexical",
    throttleMs: 0,
    extraState: () => ({ user: { id: "user-1", name: "Ada" } }),
    readSelection: () => ({
      anchor: 1,
      head: 4,
      from: 1,
      to: 4,
    }),
  });
  expectType<SelectionPresenceController>(controller);
  controller.flush();
  controller.dispose();

  return null;
}

void RichTextReactIntegrationTypes;
