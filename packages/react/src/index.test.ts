import assert from "node:assert/strict";
import { isValidElement, type ReactElement } from "react";
import {
  Cursor,
  RoomProvider,
  createRoomContext,
  useCommentListener,
  useCurrentRoom,
  useMutation,
  useNotificationEvents,
  useNotificationListener,
  useInboxNotifications,
  useMutateLiveStorage,
  usePatchStorage,
  useRoomThread,
  useRoomThreads,
  useRoomCommentEvents,
  useSetLiveStorage,
  useSetStorage,
  useStorage,
  useStorageListener,
  useStorageMutation,
  useStoragePendingMutations,
  useStorageSequence,
  useStorageSelector,
  useStorageStatus,
  useUpdateLiveStorage,
  useUnreadInboxCount,
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
assert.equal(typeof useCommentListener, "function");
assert.equal(typeof useRoomCommentEvents, "function");
assert.equal(typeof useNotificationListener, "function");
assert.equal(typeof useNotificationEvents, "function");
assert.equal(typeof useRoomThreads, "function");
assert.equal(typeof useRoomThread, "function");
assert.equal(typeof useInboxNotifications, "function");
assert.equal(typeof useUnreadInboxCount, "function");
assert.equal(typeof RoomProvider, "function");
assert.equal(typeof useCurrentRoom, "function");

const roomContext = createRoomContext();
assert.equal(typeof roomContext.RoomProvider, "function");
assert.equal(typeof roomContext.useRoom, "function");
assert.equal(typeof roomContext.useOthers, "function");
assert.equal(typeof roomContext.useStorage, "function");
assert.equal(typeof roomContext.useMutation, "function");
assert.equal(typeof roomContext.useThreads, "function");
assert.equal(typeof roomContext.useThread, "function");
assert.equal(typeof roomContext.useCommentEvents, "function");

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
