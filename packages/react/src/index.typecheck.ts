import type {
  ConnectionStatus,
  OpenRTCCursorPeer,
  OpenRTCEvent,
  OpenRTCLiveObject,
  OpenRTCRoom,
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
  RoomProvider,
  createRoomContext,
  useCursor,
  useCursors,
  useCursorsMapped,
  useBroadcastEvent,
  useBroadcastEventWithAck,
  useCurrentRoom,
  useEnterRoom,
  useErrorListener,
  useLostConnectionListener,
  useMyPresence,
  useMyPresenceSelector,
  useMutation,
  useCanRedo,
  useCanUndo,
  useOther,
  useOtherCursors,
  useOthers,
  useOthersConnectionIds,
  useOthersMapped,
  useRoomReconnect,
  useRedo,
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
  useStorageSequence,
  useStorageStatus,
  useHistory,
  useUndo,
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
  expectType<number | undefined>(useStorageSequence(roomId));
  expectType<OpenRTCStoragePendingMutation[]>(useStoragePendingMutations(roomId));
  expectType<boolean>(useCanUndo(roomId));
  expectType<boolean>(useCanRedo(roomId));
  expectType<boolean>(useHistory(roomId).canUndo());
  expectType<Promise<CanvasStorage | undefined>>(useUndo<CanvasStorage>(roomId)({ opId: "undo-1" }));
  expectType<Promise<CanvasStorage | undefined>>(useRedo<CanvasStorage>(roomId)({ opId: "redo-1" }));

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

  const roomMutation = useMutation<CanvasStorage, [title: string], Promise<void>>(
    roomId,
    async ({ room, storage, self, others, myPresence, setMyPresence, updateMyPresence, broadcastEvent, setStorage }, title) => {
      expectType<OpenRTCRoom>(room);
      expectType<CanvasStorage | undefined>(storage);
      expectType<PresencePeer | undefined>(self);
      expectType<PresencePeer[]>(others);
      expectType<PresenceState>(myPresence);
      setMyPresence({ editing: true });
      updateMyPresence({ title });
      expectType<string>(broadcastEvent("canvas.title", { title }));
      await setStorage({ title, items: [] });
    },
    [],
  );
  expectType<Promise<void>>(roomMutation("Published"));

  useStorageListener<OpenRTCLiveObject<CanvasStorage>>(roomId, (event) => {
    expectType<OpenRTCStorageEvent<OpenRTCLiveObject<CanvasStorage>>>(event);
    expectType<number | undefined>(event.sequence);
  });

  return null;
}

const boundRoom = createRoomContext();

function RoomContextIntegrationTypes() {
  type CanvasStorage = {
    title: string;
    items: unknown[];
  };

  expectType<ReactNode>(
    RoomProvider({
      id: "tenant-a:canvas-1",
      initialPresence: { cursor: null },
      afterSequence: 12,
      children: null,
    }),
  );
  expectType<ReactNode>(
    boundRoom.RoomProvider({
      id: "tenant-a:canvas-1",
      initialPresence: { cursor: null },
      afterSequence: 12,
      children: null,
    }),
  );

  expectType<OpenRTCRoom>(useCurrentRoom());
  expectType<OpenRTCRoom>(boundRoom.useRoom());
  expectType<ConnectionStatus>(boundRoom.useStatus());
  expectType<PresencePeer[]>(boundRoom.useOthers());
  expectType<string[]>(boundRoom.useOthersConnectionIds());
  expectType<string[]>(boundRoom.useOthersMapped((other) => other.connId));
  expectType<PresencePeer | undefined>(boundRoom.useOther("conn-1"));
  expectType<unknown>(boundRoom.useOther("conn-1", (other) => other.state["cursor"]));
  expectType<PresencePeer | undefined>(boundRoom.useSelf());
  expectType<unknown>(boundRoom.useSelf((self) => self.state["user"]));
  expectType<[PresenceState, (patch: PresenceState) => void]>(boundRoom.useMyPresence());
  expectType<unknown>(boundRoom.useMyPresenceSelector((presence) => presence["cursor"]));
  expectType<(state: PresenceState) => void>(boundRoom.useUpdateMyPresence());
  expectType<(patch: PresenceState) => void>(boundRoom.usePatchMyPresence());
  expectType<(event: string, payload?: unknown) => string>(boundRoom.useBroadcastEvent());
  expectType<CanvasStorage | undefined>(boundRoom.useStorage<CanvasStorage>());
  expectType<string>(boundRoom.useStorageSelector<CanvasStorage, string>((storage) => storage?.title ?? "Untitled"));
  expectType<OpenRTCStorageStatus>(boundRoom.useStorageStatus());
  expectType<number | undefined>(boundRoom.useStorageSequence());
  expectType<OpenRTCStoragePendingMutation[]>(boundRoom.useStoragePendingMutations());
  expectType<boolean>(boundRoom.useCanUndo());
  expectType<boolean>(boundRoom.useCanRedo());
  expectType<boolean>(boundRoom.useHistory().canRedo());
  expectType<Promise<CanvasStorage | undefined>>(boundRoom.useUndo<CanvasStorage>()({ opId: "bound-undo-1" }));
  expectType<Promise<CanvasStorage | undefined>>(boundRoom.useRedo<CanvasStorage>()({ opId: "bound-redo-1" }));
  expectType<Promise<CanvasStorage>>(boundRoom.useSetStorage<CanvasStorage>()({ title: "Draft", items: [] }));

  const boundMutation = boundRoom.useMutation<CanvasStorage, [title: string], Promise<void>>(
    async ({ room, storage, self, others, myPresence, updateMyPresence, broadcastEventWithAck, setStorage }, title) => {
      expectType<OpenRTCRoom>(room);
      expectType<CanvasStorage | undefined>(storage);
      expectType<PresencePeer | undefined>(self);
      expectType<PresencePeer[]>(others);
      expectType<PresenceState>(myPresence);
      updateMyPresence({ title });
      expectType<Promise<OpenRTCEvent>>(broadcastEventWithAck("canvas.title", { title }));
      await setStorage({ title, items: [] });
    },
    [],
  );
  expectType<Promise<void>>(boundMutation("Published"));

  return null;
}

void PresenceIntegrationTypes;
void StorageIntegrationTypes;
void RoomContextIntegrationTypes;
