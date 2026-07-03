import type {
  OpenRTCCursorPeer,
  OpenRTCEvent,
  OpenRTCLiveObject,
  OpenRTCStorageEvent,
  OpenRTCStoragePendingMutation,
  OpenRTCStorageStatus,
  PresencePeer,
  PresenceState,
} from "@openrtc/client";
import type { ReactNode } from "react";
import {
  AvatarStack,
  Cursor,
  Cursors,
  useCursor,
  useCursors,
  useCursorsMapped,
  useBroadcastEvent,
  useBroadcastEventWithAck,
  useEnterRoom,
  useErrorListener,
  useLostConnectionListener,
  useMyPresence,
  useMyPresenceSelector,
  useOther,
  useOtherCursors,
  useOthers,
  useOthersConnectionIds,
  useOthersMapped,
  useRoomReconnect,
  useRoomEvents,
  useRoomSelector,
  useRoomStatus,
  useSelf,
  useSelfCursor,
  useSetCursor,
  useSetLiveStorage,
  useSetStorage,
  useStatus,
  useStorage,
  useStorageListener,
  useStorageMutation,
  useStorageSelector,
  useStoragePendingMutations,
  useStorageStatus,
  useUpdateLiveStorage,
} from "./index.ts";

function expectType<T>(_value: T): void {}

function PresenceIntegrationTypes() {
  const room = useEnterRoom("tenant-a:canvas-1", {
    initialPresence: { cursor: null, user: { id: "user-1", name: "Ada" } },
  });

  expectType<string>(room.id);
  expectType<"idle" | "connecting" | "open" | "reconnecting" | "closed" | "error">(useStatus());
  expectType<"idle" | "connecting" | "open" | "reconnecting" | "closed" | "error">(useRoomStatus(room.id));
  expectType<PresencePeer[]>(useOthers(room.id));
  expectType<string[]>(useOthersConnectionIds(room.id));
  expectType<OpenRTCCursorPeer[]>(useCursors(room.id));
  expectType<OpenRTCCursorPeer[]>(useOtherCursors(room.id, { presenceKey: "selectionCursor" }));
  expectType<string[]>(
    useCursorsMapped(room.id, (cursor) => cursor.user?.name ?? cursor.connId, { presenceKey: "selectionCursor" }),
  );
  expectType<Array<{ id: string; state: PresenceState }>>(
    useOthersMapped(room.id, (other) => ({ id: other.connId, state: other.state })),
  );
  expectType<PresencePeer | undefined>(useOther(room.id, "conn-1"));
  expectType<unknown>(useOther(room.id, "conn-1", (other) => other.state["cursor"]));
  expectType<PresencePeer | undefined>(useSelf(room.id));
  expectType<unknown>(useSelf(room.id, (self) => self.state["user"]));
  expectType<number>(useRoomSelector(room.id, (presence) => presence.others.length));

  const [myPresence, patchMyPresence] = useMyPresence(room.id);
  expectType<PresenceState>(myPresence);
  expectType<(patch: PresenceState) => void>(patchMyPresence);
  expectType<unknown>(useMyPresenceSelector(room.id, (presence) => presence["cursor"]));

  const setCursor = useSetCursor(room.id);
  expectType<(cursor: { x: number; y: number } | null, options?: { color?: string }) => void>(setCursor);
  expectType<{ x: number; y: number } | null>(useSelfCursor(room.id));
  const [cursor, updateCursor] = useCursor(room.id);
  expectType<{ x: number; y: number } | null>(cursor);
  expectType<(cursor: { x: number; y: number } | null, options?: { color?: string }) => void>(updateCursor);
  expectType<ReactNode>(
    Cursor({
      cursor: { x: 24, y: 36, mode: "comment" },
      user: { id: "user-1", name: "Ada" },
      color: "#4fd1b6",
      coordinateSpace: "percent",
    }),
  );
  expectType<ReactNode>(
    Cursors({
      room: room.id,
      children: null,
      coordinateSpace: "percent",
      cursorOptions: { user: { id: "user-1", name: "Ada" }, color: "#4fd1b6" },
      renderCursor: (cursorPeer) => cursorPeer.cursor.label,
    }),
  );
  expectType<ReactNode>(
    AvatarStack({
      room: room.id,
      includeSelf: true,
      max: 4,
      renderAvatar: (peer) => peer.connId,
    }),
  );

  const broadcastWithAck = useBroadcastEventWithAck(room.id);
  const broadcast = useBroadcastEvent(room.id);
  expectType<string>(broadcast("canvas.ping", { ok: true }, { traceId: "trace-0" }));
  expectType<string>(broadcast({ type: "CANVAS_PING", ok: true }));
  expectType<Promise<OpenRTCEvent>>(broadcastWithAck("canvas.ping", { ok: true }));
  expectType<Promise<OpenRTCEvent>>(
    broadcastWithAck({ type: "CANVAS_PING", ok: true }, undefined, { traceId: "trace-1", timeoutMs: 1000 }),
  );
  expectType<OpenRTCEvent[]>(useRoomEvents(room.id, 50));

  useLostConnectionListener(room.id, (event) => {
    expectType<"lost" | "restored" | "failed">(event);
  });
  useErrorListener((error) => {
    expectType<string>(error.code);
  });

  const reconnect = useRoomReconnect(room.id);
  expectType<Promise<void>>(reconnect());

  return null;
}

function StorageIntegrationTypes() {
  type CanvasStorage = {
    title: string;
    items: unknown;
  };

  const roomId = "tenant-a:canvas-1";
  expectType<CanvasStorage | undefined>(useStorage<CanvasStorage>(roomId));
  expectType<string>(useStorageSelector<CanvasStorage, string>(roomId, (storage) => storage?.title ?? "Untitled"));
  expectType<string>(
    useStorageSelector<CanvasStorage, string>(
      roomId,
      (storage) => storage?.title ?? "Untitled",
      { isEqual: (previous, next) => previous === next },
    ),
  );
  expectType<OpenRTCStorageStatus>(useStorageStatus(roomId));
  expectType<OpenRTCStoragePendingMutation[]>(useStoragePendingMutations(roomId));

  const setStorage = useSetStorage<CanvasStorage>(roomId);
  expectType<Promise<CanvasStorage>>(setStorage({ title: "Draft", items: [] }, { opId: "set-1" }));

  const setLiveStorage = useSetLiveStorage<CanvasStorage>(roomId);
  expectType<Promise<OpenRTCLiveObject<CanvasStorage>>>(setLiveStorage({ title: "Draft", items: [] }));
  expectType<Promise<OpenRTCLiveObject<CanvasStorage>>>(
    setLiveStorage({ liveblocksType: "LiveObject", data: { title: "Draft", items: [] } }),
  );

  const updateLiveStorage = useUpdateLiveStorage<CanvasStorage>(roomId);
  expectType<Promise<OpenRTCLiveObject<CanvasStorage>>>(updateLiveStorage({ title: "Published" }));

  const mutateStorage = useStorageMutation<OpenRTCLiveObject<CanvasStorage>, [title: string], Promise<void>>(
    roomId,
    async ({ storage, setLiveStorage, updateLiveStorage }, title) => {
      expectType<OpenRTCLiveObject<CanvasStorage> | undefined>(storage);
      await setLiveStorage<CanvasStorage>({ title, items: [] });
      await updateLiveStorage<CanvasStorage>({ title });
    },
    [],
  );
  expectType<Promise<void>>(mutateStorage("Published"));

  useStorageListener<OpenRTCLiveObject<CanvasStorage>>(roomId, (event) => {
    expectType<OpenRTCStorageEvent<OpenRTCLiveObject<CanvasStorage>>>(event);
  });

  return null;
}

void PresenceIntegrationTypes;
void StorageIntegrationTypes;
