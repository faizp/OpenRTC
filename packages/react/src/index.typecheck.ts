import type {
  ConnectionStatus,
  OpenRTCAdminInboxNotification,
  OpenRTCAdminInboxNotificationInput,
  OpenRTCAdminRoomSubscriptionSettings,
  OpenRTCAdminThread,
  OpenRTCAdminThreadInput,
  OpenRTCAdminThreadReadState,
  OpenRTCAdminThreadUpdate,
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
  CommentsPanel,
  Cursor,
  Cursors,
  RoomProvider,
  createRoomContext,
  useAddCommentMention,
  useAddReaction,
  useCursor,
  useCursors,
  useCursorsMapped,
  useBroadcastEvent,
  useBroadcastEventWithAck,
  useCurrentRoom,
  useCreateComment,
  useCreateThread,
  useDeleteAllInboxNotifications,
  useDeleteInboxNotification,
  useDeleteThread,
  useEditComment,
  useEditCommentMetadata,
  useEditThread,
  useEditThreadMetadata,
  useEnterRoom,
  useErrorListener,
  useGetThread,
  useGetThreadReadState,
  useLostConnectionListener,
  useMarkInboxNotificationAsRead,
  useMarkThreadRead,
  useMarkThreadResolved,
  useMarkThreadUnresolved,
  useMarkThreadUnread,
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
  useRemoveCommentMention,
  useRemoveReaction,
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
  useTriggerInboxNotification,
  useHistory,
  useUndo,
  useUpdateRoomSubscriptionSettings,
  useUpdateLiveStorage,
  useResetRoomSubscriptionSettings,
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

function ProductSurfaceActionTypes() {
  const roomId = "tenant-a:canvas-1";
  const getThread = useGetThread(roomId);
  const createThread = useCreateThread(roomId);
  const editThread = useEditThread(roomId);
  const editThreadMetadata = useEditThreadMetadata(roomId);
  const markThreadResolved = useMarkThreadResolved(roomId);
  const markThreadUnresolved = useMarkThreadUnresolved(roomId);
  const getThreadReadState = useGetThreadReadState(roomId, "user-1");
  const markThreadRead = useMarkThreadRead(roomId, "user-1");
  const markThreadUnread = useMarkThreadUnread(roomId, "user-1");
  const deleteThread = useDeleteThread(roomId);
  const createComment = useCreateComment(roomId);
  const editComment = useEditComment(roomId);
  const editCommentMetadata = useEditCommentMetadata(roomId);
  const addReaction = useAddReaction(roomId);
  const removeReaction = useRemoveReaction(roomId);
  const addMention = useAddCommentMention(roomId);
  const removeMention = useRemoveCommentMention(roomId);
  const triggerInboxNotification = useTriggerInboxNotification();
  const markInboxRead = useMarkInboxNotificationAsRead();
  const deleteInboxNotification = useDeleteInboxNotification("user-1");
  const deleteAllInboxNotifications = useDeleteAllInboxNotifications("user-1");
  const updateRoomSubscriptionSettings = useUpdateRoomSubscriptionSettings(roomId, "user-1");
  const resetRoomSubscriptionSettings = useResetRoomSubscriptionSettings(roomId, "user-1");

  const threadInput: OpenRTCAdminThreadInput = {
    comment: {
      userId: "user-1",
      body: { type: "text", text: "Review" },
    },
  };
  const threadUpdate: OpenRTCAdminThreadUpdate = {
    metadata: { status: "resolved" },
    resolved: true,
  };
  expectType<Promise<OpenRTCAdminThread>>(getThread("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(createThread(threadInput));
  expectType<Promise<OpenRTCAdminThread>>(editThread("thread-1", threadUpdate));
  expectType<Promise<OpenRTCAdminThread>>(
    editThreadMetadata({ threadId: "thread-1", metadata: { status: "open" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(markThreadResolved("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(markThreadUnresolved("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(getThreadReadState("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(markThreadRead("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(markThreadUnread("thread-1"));
  expectType<Promise<void>>(deleteThread("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(
    createComment("thread-1", { userId: "user-1", body: { type: "text", text: "Next" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(editComment("thread-1", "comment-1", { metadata: { status: "open" } }));
  expectType<Promise<OpenRTCAdminThread>>(
    editCommentMetadata({ threadId: "thread-1", commentId: "comment-1", metadata: { status: "resolved" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    addReaction({
      threadId: "thread-1",
      commentId: "comment-1",
      reaction: { emoji: "+1", userId: "user-2" },
      currentReactions: [],
    }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    removeReaction({
      threadId: "thread-1",
      commentId: "comment-1",
      reaction: { emoji: "+1", userId: "user-2" },
    }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    addMention({ threadId: "thread-1", commentId: "comment-1", userId: "user-2", currentMentions: [] }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    removeMention({ threadId: "thread-1", commentId: "comment-1", userId: "user-2" }),
  );
  const inboxInput: OpenRTCAdminInboxNotificationInput = {
    userId: "user-1",
    kind: "$custom",
    roomId,
  };
  expectType<Promise<OpenRTCAdminInboxNotification>>(triggerInboxNotification(inboxInput));
  expectType<Promise<OpenRTCAdminInboxNotification>>(markInboxRead("in_1"));
  expectType<Promise<void>>(deleteInboxNotification("in_1"));
  expectType<Promise<void>>(deleteAllInboxNotifications());
  expectType<Promise<OpenRTCAdminRoomSubscriptionSettings>>(
    updateRoomSubscriptionSettings({ threads: "all", textMentions: "mine" }),
  );
  expectType<Promise<void>>(resetRoomSubscriptionSettings());
  expectType<ReactNode>(
    CommentsPanel({
      room: roomId,
      userId: "user-1",
      initialThreads: [],
      fetch: false,
      bodyFromText: (text, context) => ({ type: "text", text, kind: context.kind }),
      textFromBody: (_body, context) => (context.kind === "thread" ? "Thread" : "Reply"),
      threadMetadata: ({ kind, text }) => ({ kind, text }),
      commentMetadata: { source: "panel" },
      renderComment: ({ text }) => text,
      renderThreadActions: ({ pending }) => (pending ? "Pending" : null),
    }),
  );

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
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useCreateThread()({ comment: { userId: "user-1", body: { type: "text", text: "Review" } } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(boundRoom.useGetThread()("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useEditThread()("thread-1", { metadata: { status: "resolved" }, resolved: true }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useEditThreadMetadata()({ threadId: "thread-1", metadata: { status: "open" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(boundRoom.useMarkThreadResolved()("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(boundRoom.useMarkThreadUnresolved()("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(boundRoom.useGetThreadReadState("user-1")("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(boundRoom.useMarkThreadRead("user-1")("thread-1"));
  expectType<Promise<OpenRTCAdminThreadReadState>>(boundRoom.useMarkThreadUnread("user-1")("thread-1"));
  expectType<Promise<void>>(boundRoom.useDeleteThread()("thread-1"));
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useCreateComment()("thread-1", { userId: "user-1", body: { type: "text", text: "Reply" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useEditComment()("thread-1", "comment-1", { body: { type: "text", text: "Edited" } }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useEditCommentMetadata()({
      threadId: "thread-1",
      commentId: "comment-1",
      metadata: { status: "resolved" },
    }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useAddReaction()({
      threadId: "thread-1",
      commentId: "comment-1",
      reaction: { emoji: "+1", userId: "user-2" },
    }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useRemoveReaction()({
      threadId: "thread-1",
      commentId: "comment-1",
      reaction: { emoji: "+1", userId: "user-2" },
    }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useAddCommentMention()({ threadId: "thread-1", commentId: "comment-1", userId: "user-2" }),
  );
  expectType<Promise<OpenRTCAdminThread>>(
    boundRoom.useRemoveCommentMention()({ threadId: "thread-1", commentId: "comment-1", userId: "user-2" }),
  );
  expectType<Promise<OpenRTCAdminRoomSubscriptionSettings>>(
    boundRoom.useUpdateRoomSubscriptionSettings("user-1")({ threads: "none" }),
  );
  expectType<Promise<void>>(boundRoom.useResetRoomSubscriptionSettings("user-1")());
  expectType<Promise<CanvasStorage>>(boundRoom.useSetStorage<CanvasStorage>()({ title: "Draft", items: [] }));
  expectType<ReactNode>(
    boundRoom.CommentsPanel({
      userId: "user-1",
      initialThreads: [],
      fetch: false,
      showResolved: false,
    }),
  );

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
void ProductSurfaceActionTypes;
void RoomContextIntegrationTypes;
