import { useEffect, useMemo } from "react";
import { useOpenRTC, useOthers } from "@openrtc/react";
import type { JoinOptions, PresenceState } from "@openrtc/client";
import {
  createSelectionPresenceController,
  getRemoteTextSelections,
  type RemoteTextSelection,
  type RemoteTextSelectionOptions,
  type SelectionPresenceController,
  type TextSelectionPresence,
} from "./index.ts";

export interface UseRemoteTextSelectionsOptions extends JoinOptions, RemoteTextSelectionOptions {}

export interface UseSelectionPresenceControllerOptions {
  room: string;
  editor?: TextSelectionPresence["editor"] | undefined;
  readSelection: () => Omit<TextSelectionPresence, "kind" | "editor" | "updatedAt"> | null;
  throttleMs?: number | undefined;
  extraState?: (() => PresenceState) | undefined;
}

export function useRemoteTextSelections(
  room: string,
  options: UseRemoteTextSelectionsOptions = {},
): RemoteTextSelection[] {
  const others = useOthers(room, options);
  const editor = options.editor;
  const maxAgeMs = options.maxAgeMs;
  const now = options.now;

  return useMemo(
    () =>
      getRemoteTextSelections(others, {
        ...(editor ? { editor } : {}),
        ...(maxAgeMs !== undefined ? { maxAgeMs } : {}),
        ...(now !== undefined ? { now } : {}),
      }),
    [editor, maxAgeMs, now, others],
  );
}

export function useSelectionPresenceController(
  options: UseSelectionPresenceControllerOptions,
): SelectionPresenceController {
  const client = useOpenRTC();
  const room = options.room;
  const editor = options.editor;
  const readSelection = options.readSelection;
  const throttleMs = options.throttleMs;
  const extraState = options.extraState;

  const controller = useMemo(
    () =>
      createSelectionPresenceController({
        room,
        client,
        readSelection,
        ...(editor ? { editor } : {}),
        ...(throttleMs !== undefined ? { throttleMs } : {}),
        ...(extraState ? { extraState } : {}),
      }),
    [client, editor, extraState, readSelection, room, throttleMs],
  );

  useEffect(() => () => controller.dispose(), [controller]);

  return controller;
}
