import assert from "node:assert/strict";
import { isValidElement, type ReactElement } from "react";
import {
  Cursor,
  OpenRTCAdminProvider,
  RoomProvider,
  createRoomContext,
  useAddCommentMention,
  useAddReaction,
  useCanRedo,
  useCanUndo,
  useCommentListener,
  useCreateComment,
  useCreateThread,
  useCurrentRoom,
  useDeleteAllInboxNotifications,
  useDeleteInboxNotification,
  useDeleteThread,
  useEditComment,
  useEditCommentMetadata,
  useEditThread,
  useEditThreadMetadata,
  useGetThread,
  useHistory,
  useMarkInboxNotificationAsRead,
  useMarkThreadResolved,
  useMarkThreadUnresolved,
  useOpenRTCAdmin,
  useMutation,
  useNotificationEvents,
  useNotificationListener,
  useInboxNotifications,
  useInboxNotificationsState,
  useMutateLiveStorage,
  usePatchStorage,
  useRemoveCommentMention,
  useRemoveReaction,
  useRoomThread,
  useRoomThreads,
  useRoomThreadsState,
  useRoomCommentEvents,
  useRedo,
  useSetLiveStorage,
  useSetStorage,
  useStorage,
  useStorageListener,
  useStorageMutation,
  useStoragePendingMutations,
  useStorageSequence,
  useStorageSelector,
  useStorageStatus,
  useTriggerInboxNotification,
  useUndo,
  useUpdateRoomSubscriptionSettings,
  useUpdateLiveStorage,
  useUnreadInboxCount,
  useResetRoomSubscriptionSettings,
} from "./index.ts";

type ElementProps = Record<string, unknown>;
type ElementWithProps = ReactElement<ElementProps>;

function asElement(value: unknown): ElementWithProps {
  assert.equal(isValidElement(value), true);
  return value as ElementWithProps;
}

function childrenOf(element: ElementWithProps): unknown[] {
  return Array.isArray(element.props.children) ? element.props.children : [element.props.children];
}

assert.equal(typeof useStorage, "function");
assert.equal(typeof useStorageSelector, "function");
assert.equal(typeof useStorageStatus, "function");
assert.equal(typeof useStorageSequence, "function");
assert.equal(typeof useSetStorage, "function");
assert.equal(typeof usePatchStorage, "function");
assert.equal(typeof useSetLiveStorage, "function");
assert.equal(typeof useUpdateLiveStorage, "function");
assert.equal(typeof useMutateLiveStorage, "function");
assert.equal(typeof useStorageMutation, "function");
assert.equal(typeof useMutation, "function");
assert.equal(typeof useStorageListener, "function");
assert.equal(typeof useStoragePendingMutations, "function");
assert.equal(typeof useHistory, "function");
assert.equal(typeof useUndo, "function");
assert.equal(typeof useRedo, "function");
assert.equal(typeof useCanUndo, "function");
assert.equal(typeof useCanRedo, "function");
assert.equal(typeof useCommentListener, "function");
assert.equal(typeof useRoomCommentEvents, "function");
assert.equal(typeof useNotificationListener, "function");
assert.equal(typeof useNotificationEvents, "function");
assert.equal(typeof useRoomThreads, "function");
assert.equal(typeof useRoomThread, "function");
assert.equal(typeof useRoomThreadsState, "function");
assert.equal(typeof useInboxNotifications, "function");
assert.equal(typeof useInboxNotificationsState, "function");
assert.equal(typeof useUnreadInboxCount, "function");
assert.equal(typeof useCreateThread, "function");
assert.equal(typeof useGetThread, "function");
assert.equal(typeof useEditThread, "function");
assert.equal(typeof useEditThreadMetadata, "function");
assert.equal(typeof useMarkThreadResolved, "function");
assert.equal(typeof useMarkThreadUnresolved, "function");
assert.equal(typeof useDeleteThread, "function");
assert.equal(typeof useCreateComment, "function");
assert.equal(typeof useEditComment, "function");
assert.equal(typeof useEditCommentMetadata, "function");
assert.equal(typeof useAddReaction, "function");
assert.equal(typeof useRemoveReaction, "function");
assert.equal(typeof useAddCommentMention, "function");
assert.equal(typeof useRemoveCommentMention, "function");
assert.equal(typeof useTriggerInboxNotification, "function");
assert.equal(typeof useMarkInboxNotificationAsRead, "function");
assert.equal(typeof useDeleteInboxNotification, "function");
assert.equal(typeof useDeleteAllInboxNotifications, "function");
assert.equal(typeof useUpdateRoomSubscriptionSettings, "function");
assert.equal(typeof useResetRoomSubscriptionSettings, "function");
assert.equal(typeof OpenRTCAdminProvider, "function");
assert.equal(typeof useOpenRTCAdmin, "function");
assert.equal(typeof RoomProvider, "function");
assert.equal(typeof useCurrentRoom, "function");

const roomContext = createRoomContext();
assert.equal(typeof roomContext.RoomProvider, "function");
assert.equal(typeof roomContext.useRoom, "function");
assert.equal(typeof roomContext.useOthers, "function");
assert.equal(typeof roomContext.useStorage, "function");
assert.equal(typeof roomContext.useHistory, "function");
assert.equal(typeof roomContext.useUndo, "function");
assert.equal(typeof roomContext.useRedo, "function");
assert.equal(typeof roomContext.useCanUndo, "function");
assert.equal(typeof roomContext.useCanRedo, "function");
assert.equal(typeof roomContext.useMutation, "function");
assert.equal(typeof roomContext.useThreads, "function");
assert.equal(typeof roomContext.useThreadsState, "function");
assert.equal(typeof roomContext.useThread, "function");
assert.equal(typeof roomContext.useCommentEvents, "function");
assert.equal(typeof roomContext.useCreateThread, "function");
assert.equal(typeof roomContext.useGetThread, "function");
assert.equal(typeof roomContext.useEditThread, "function");
assert.equal(typeof roomContext.useEditThreadMetadata, "function");
assert.equal(typeof roomContext.useMarkThreadResolved, "function");
assert.equal(typeof roomContext.useMarkThreadUnresolved, "function");
assert.equal(typeof roomContext.useDeleteThread, "function");
assert.equal(typeof roomContext.useCreateComment, "function");
assert.equal(typeof roomContext.useEditComment, "function");
assert.equal(typeof roomContext.useEditCommentMetadata, "function");
assert.equal(typeof roomContext.useAddReaction, "function");
assert.equal(typeof roomContext.useRemoveReaction, "function");
assert.equal(typeof roomContext.useAddCommentMention, "function");
assert.equal(typeof roomContext.useRemoveCommentMention, "function");
assert.equal(typeof roomContext.useUpdateRoomSubscriptionSettings, "function");
assert.equal(typeof roomContext.useResetRoomSubscriptionSettings, "function");

const labeledCursor = asElement(
  Cursor({
    cursor: { x: 24, y: 36, mode: "comment", label: "fallback-label" },
    user: { id: "user-1", name: "Ada" },
    color: "#ffffff",
    coordinateSpace: "percent",
  }),
);
assert.equal(labeledCursor.props["data-openrtc-cursor"], "comment");
assert.deepEqual(
  {
    left: (labeledCursor.props.style as ElementProps)["left"],
    top: (labeledCursor.props.style as ElementProps)["top"],
    color: (labeledCursor.props.style as ElementProps)["color"],
  },
  { left: "24%", top: "36%", color: "#ffffff" },
);

const labeledChildren = childrenOf(labeledCursor);
assert.equal(labeledChildren.length, 2);
const label = asElement(labeledChildren[1]);
assert.equal(label.props.children, "Ada");
assert.equal((label.props.style as ElementProps)["color"], "#111827");

const pixelCursor = asElement(
  Cursor({
    cursor: { x: 120, y: 80 },
    coordinateSpace: "pixel",
    showLabel: false,
  }),
);
assert.equal(pixelCursor.props["data-openrtc-cursor"], "pointer");
assert.deepEqual(
  {
    left: (pixelCursor.props.style as ElementProps)["left"],
    top: (pixelCursor.props.style as ElementProps)["top"],
  },
  { left: 120, top: 80 },
);
assert.equal(childrenOf(pixelCursor)[1], null);

const clampedCursor = asElement(
  Cursor({
    cursor: { x: -40, y: 140 },
    coordinateSpace: "percent",
  }),
);
assert.deepEqual(
  {
    left: (clampedCursor.props.style as ElementProps)["left"],
    top: (clampedCursor.props.style as ElementProps)["top"],
  },
  { left: "0%", top: "100%" },
);
