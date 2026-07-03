import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type CSSProperties,
  type DependencyList,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import {
  OpenRTCClient,
  getCursorPeers,
  getPresenceColor,
  getPresenceCursor,
  getPresenceUser,
  type BroadcastOptions,
  type ConnectionStatus,
  type EnterRoomOptions,
  type JSONPatchOperation,
  type JoinOptions,
  type OpenRTCCursor,
  type OpenRTCCursorOptions,
  type OpenRTCCursorPeer,
  type OpenRTCUserInfo,
  type OpenRTCCommentEvent,
  type OpenRTCEvent,
  type OpenRTCError,
  type OpenRTCDiagnosticEvent,
  type OpenRTCNotificationDelta,
  type OpenRTCRoom,
  type OpenRTCRoomPresence,
  type OpenRTCRoomState,
  type OpenRTCLostConnectionEvent,
  type OpenRTCLiveObject,
  type OpenRTCStorageEvent,
  type OpenRTCStorageMutationOptions,
  type OpenRTCStoragePendingMutation,
  type OpenRTCStorageStatus,
  type PresencePeer,
  type PresenceState,
  type RoomBroadcastInput,
} from "@openrtc/client";

const OpenRTCContext = createContext<OpenRTCClient | null>(null);

export type RoomPresenceSelector<T> = (presence: OpenRTCRoomPresence) => T;
export type OtherSelector<T> = (other: PresencePeer) => T;
export type CursorSelector<T> = (cursor: OpenRTCCursorPeer) => T;
export type SelfSelector<T> = (self: PresencePeer) => T;
export type MyPresenceSelector<T> = (presence: PresenceState) => T;
export type StorageSelector<TDocument, TSelected> = (document: TDocument | undefined) => TSelected;
export type StorageSelectorEquality<TSelected> = (previous: TSelected, next: TSelected) => boolean;

export interface StorageSelectorOptions<TSelected> extends JoinOptions {
  isEqual?: StorageSelectorEquality<TSelected>;
}

export interface StorageMutationContext<TDocument = unknown> {
  storage: TDocument | undefined;
  setStorage(document: TDocument, options?: OpenRTCStorageMutationOptions): Promise<TDocument>;
  patchStorage(operations: JSONPatchOperation[], options?: OpenRTCStorageMutationOptions): Promise<TDocument>;
  setLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    data: TData | OpenRTCLiveObject<TData>,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<OpenRTCLiveObject<TData>>;
  updateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
    patch: Partial<TData>,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<OpenRTCLiveObject<TData>>;
}

export interface CursorOptions extends JoinOptions {
  presenceKey?: string;
}

export type CursorCoordinateSpace = "percent" | "pixel";

export interface CursorProps {
  cursor: OpenRTCCursor;
  user?: OpenRTCUserInfo;
  color?: string;
  mode?: OpenRTCCursor["mode"];
  label?: ReactNode;
  showLabel?: boolean;
  coordinateSpace?: CursorCoordinateSpace;
  className?: string;
  style?: CSSProperties;
}

export interface CursorsProps extends CursorOptions {
  room: string;
  children?: ReactNode;
  className?: string;
  style?: CSSProperties;
  coordinateSpace?: CursorCoordinateSpace;
  trackOwnCursor?: boolean;
  clearOnPointerLeave?: boolean;
  throttleMs?: number;
  mode?: OpenRTCCursor["mode"];
  cursorLabel?: string;
  cursorOptions?: OpenRTCCursorOptions;
  showLabels?: boolean;
  renderCursor?: (cursor: OpenRTCCursorPeer) => ReactNode;
}

export interface AvatarStackProps extends JoinOptions {
  room: string;
  max?: number | null;
  includeSelf?: boolean;
  size?: number;
  gap?: number;
  className?: string;
  style?: CSSProperties;
  renderAvatar?: (peer: PresencePeer, index: number) => ReactNode;
}

interface AvatarPeer {
  peer: PresencePeer;
  user?: OpenRTCUserInfo;
  color: string;
  label: string;
  initials: string;
}

export interface OpenRTCProviderProps {
  client: OpenRTCClient;
  children: ReactNode;
}

export function OpenRTCProvider(props: OpenRTCProviderProps) {
  return createElement(OpenRTCContext.Provider, { value: props.client }, props.children);
}

export function useOpenRTC(): OpenRTCClient {
  const client = useContext(OpenRTCContext);
  if (!client) {
    throw new Error("useOpenRTC must be used inside OpenRTCProvider");
  }
  return client;
}

export function useConnectionStatus(): ConnectionStatus {
  const client = useOpenRTC();
  const [status, setStatus] = useState<ConnectionStatus>(client.status);

  useEffect(() => client.on("status", setStatus), [client]);

  return status;
}

export function useStatus(): ConnectionStatus {
  return useConnectionStatus();
}

export function useRoomHandle(room: string): OpenRTCRoom {
  const client = useOpenRTC();
  return useMemo(() => client.room(room), [client, room]);
}

export function useEnterRoom(room: string, options: EnterRoomOptions = {}): OpenRTCRoom {
  const client = useOpenRTC();
  const roomHandle = useRoomHandle(room);
  const limit = options.limit;
  const cursor = options.cursor;
  const initialPresence = useInitialPresence(room, options.initialPresence);

  useEffect(() => {
    const entered = client.enterRoom(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
      ...(initialPresence !== undefined ? { initialPresence } : {}),
    });
    return entered.leave;
  }, [client, room, limit, cursor, initialPresence]);

  return roomHandle;
}

export function useRoom(room: string, options: JoinOptions = {}): OpenRTCRoomState {
  const client = useOpenRTC();
  const [state, setState] = useState<OpenRTCRoomState>(() => client.getRoomState(room));
  const limit = options.limit;
  const cursor = options.cursor;

  useEffect(() => {
    const entered = client.enterRoom(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
    });
    setState(client.getRoomState(room));

    const syncRoom = (next: OpenRTCRoomState): void => {
      if (next.room === room) {
        setState(next);
      }
    };
    const syncPresence = (event: { room: string }): void => {
      if (event.room === room) {
        setState(client.getRoomState(room));
      }
    };

    const offRoom = client.on("room", syncRoom);
    const offPresence = client.on("presence", syncPresence);
    return () => {
      offRoom();
      offPresence();
      entered.leave();
    };
  }, [client, room, limit, cursor]);

  return state;
}

export function useRoomStatus(room: string): ConnectionStatus {
  const roomHandle = useRoomHandle(room);
  const [status, setStatus] = useState<ConnectionStatus>(roomHandle.getStatus());

  useEffect(() => {
    setStatus(roomHandle.getStatus());
    return roomHandle.subscribe("status", setStatus);
  }, [roomHandle]);

  return status;
}

export function useRoomSelector<T>(room: string, selector: RoomPresenceSelector<T>, options: JoinOptions = {}): T {
  const presence = usePresence(room, options);

  return useMemo(() => selector(presence), [presence, selector]);
}

export function useOthers(room: string, options: JoinOptions = {}): PresencePeer[] {
  return useRoomSelector(room, (presence) => presence.others, options);
}

export function useOthersMapped<T>(room: string, selector: OtherSelector<T>, options: JoinOptions = {}): T[] {
  const others = useOthers(room, options);

  return useMemo(() => others.map(selector), [others, selector]);
}

export function useOthersConnectionIds(room: string, options: JoinOptions = {}): string[] {
  const others = useOthers(room, options);

  return useMemo(() => others.map((other) => other.connId), [others]);
}

export function useCursors(room: string, options: CursorOptions = {}): OpenRTCCursorPeer[] {
  const others = useOthers(room, options);
  const presenceKey = options.presenceKey ?? "cursor";

  return useMemo(() => getCursorPeers(others, presenceKey), [others, presenceKey]);
}

export function useOtherCursors(room: string, options: CursorOptions = {}): OpenRTCCursorPeer[] {
  return useCursors(room, options);
}

export function useCursorsMapped<T>(
  room: string,
  selector: CursorSelector<T>,
  options: CursorOptions = {},
): T[] {
  const cursors = useCursors(room, options);

  return useMemo(() => cursors.map(selector), [cursors, selector]);
}

export function useOther(room: string, connId: string, options?: JoinOptions): PresencePeer | undefined;
export function useOther<T>(
  room: string,
  connId: string,
  selector: OtherSelector<T>,
  options?: JoinOptions,
): T | undefined;
export function useOther<T>(
  room: string,
  connId: string,
  selectorOrOptions?: OtherSelector<T> | JoinOptions,
  maybeOptions: JoinOptions = {},
): PresencePeer | T | undefined {
  const hasSelector = typeof selectorOrOptions === "function";
  const selector = hasSelector ? selectorOrOptions : undefined;
  const options = hasSelector ? maybeOptions : (selectorOrOptions ?? {});
  const others = useOthers(room, options);
  const other = useMemo(() => others.find((candidate) => candidate.connId === connId), [others, connId]);

  return useMemo(() => {
    if (!other) {
      return undefined;
    }
    return selector ? selector(other) : other;
  }, [other, selector]);
}

export function usePresence(room: string, options: JoinOptions = {}): OpenRTCRoomPresence {
  const client = useOpenRTC();
  const [state, setState] = useState<OpenRTCRoomPresence>(() => client.getPresence(room));
  const limit = options.limit;
  const cursor = options.cursor;

  useEffect(() => {
    const entered = client.enterRoom(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
    });
    setState(client.getPresence(room));

    const syncRoom = (next: OpenRTCRoomState): void => {
      if (next.room === room) {
        setState(client.getPresence(room));
      }
    };
    const syncPresence = (event: { room: string }): void => {
      if (event.room === room) {
        setState(client.getPresence(room));
      }
    };

    const offRoom = client.on("room", syncRoom);
    const offPresence = client.on("presence", syncPresence);
    return () => {
      offRoom();
      offPresence();
      entered.leave();
    };
  }, [client, room, limit, cursor]);

  return state;
}

export function useSelf(room: string, options?: JoinOptions): PresencePeer | undefined;
export function useSelf<T>(room: string, selector: SelfSelector<T>, options?: JoinOptions): T | undefined;
export function useSelf<T>(
  room: string,
  selectorOrOptions?: SelfSelector<T> | JoinOptions,
  maybeOptions: JoinOptions = {},
): PresencePeer | T | undefined {
  const hasSelector = typeof selectorOrOptions === "function";
  const selector = hasSelector ? selectorOrOptions : undefined;
  const options = hasSelector ? maybeOptions : (selectorOrOptions ?? {});
  const self = usePresence(room, options).self;

  return useMemo(() => {
    if (!self) {
      return undefined;
    }
    return selector ? selector(self) : self;
  }, [self, selector]);
}

export function useSelfCursor(room: string, options: CursorOptions = {}): OpenRTCCursor | null {
  const self = useSelf(room, options);
  const presenceKey = options.presenceKey ?? "cursor";

  return useMemo(() => getPresenceCursor(self?.state, presenceKey), [self?.state, presenceKey]);
}

export function useBroadcastEvent(
  room: string,
): (event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions) => string {
  const roomHandle = useRoomHandle(room);
  return useCallback(
    (event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions) => {
      return roomHandle.broadcastEvent(event, payload, options);
    },
    [roomHandle],
  );
}

export function useBroadcastEventWithAck(
  room: string,
): (event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions) => Promise<OpenRTCEvent> {
  const roomHandle = useRoomHandle(room);
  return useCallback(
    (event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions) =>
      roomHandle.broadcastEventWithAck(event, payload, options),
    [roomHandle],
  );
}

export function useStorage<TDocument = unknown>(room: string, options: JoinOptions = {}): TDocument | undefined {
  const client = useOpenRTC();
  const roomHandle = useRoomHandle(room);
  const [document, setDocument] = useState<TDocument | undefined>(() => roomHandle.getStorageSnapshot<TDocument>());
  const limit = options.limit;
  const cursor = options.cursor;

  useEffect(() => {
    let active = true;
    const entered = client.enterRoom(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
    });
    setDocument(roomHandle.getStorageSnapshot<TDocument>());

    const offStorage = roomHandle.subscribe("storage", (event) => {
      setDocument(event.document as TDocument);
    });
    void roomHandle.getStorage<TDocument>().then(
      (next) => {
        if (active) {
          setDocument(next);
        }
      },
      () => undefined,
    );

    return () => {
      active = false;
      offStorage();
      entered.leave();
    };
  }, [client, roomHandle, room, limit, cursor]);

  return document;
}

export function useStorageSelector<TDocument, TSelected>(
  room: string,
  selector: StorageSelector<TDocument, TSelected>,
  options: StorageSelectorOptions<TSelected> = {},
): TSelected {
  const client = useOpenRTC();
  const roomHandle = useRoomHandle(room);
  const selectorRef = useRef(selector);
  const isEqualRef = useRef<StorageSelectorEquality<TSelected>>(Object.is);
  const selectedRef = useRef<{
    room: string;
    document: TDocument | undefined;
    selector: StorageSelector<TDocument, TSelected>;
    value: TSelected;
  } | null>(null);
  selectorRef.current = selector;
  isEqualRef.current = options.isEqual ?? Object.is;
  const limit = options.limit;
  const cursor = options.cursor;

  useEffect(() => {
    const entered = client.enterRoom(room, {
      ...(limit !== undefined ? { limit } : {}),
      ...(cursor !== undefined ? { cursor } : {}),
    });
    void roomHandle.getStorage<TDocument>().catch(() => undefined);
    return entered.leave;
  }, [client, roomHandle, room, limit, cursor]);

  const subscribe = useCallback(
    (onStoreChange: () => void) => roomHandle.subscribe("storage", onStoreChange),
    [roomHandle],
  );

  const getSnapshot = useCallback(() => {
    const document = roomHandle.getStorageSnapshot<TDocument>();
    const currentSelector = selectorRef.current;
    const previous = selectedRef.current;
    if (previous?.room === room && previous.document === document && previous.selector === currentSelector) {
      return previous.value;
    }
    const next = currentSelector(document);
    if (previous?.room === room && isEqualRef.current(previous.value, next)) {
      selectedRef.current = { room, document, selector: currentSelector, value: previous.value };
      return previous.value;
    }
    selectedRef.current = { room, document, selector: currentSelector, value: next };
    return next;
  }, [roomHandle, room]);

  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

export function useStorageStatus(room: string): OpenRTCStorageStatus {
  const roomHandle = useRoomHandle(room);
  const [status, setStatus] = useState<OpenRTCStorageStatus>(roomHandle.getStorageStatus());

  useEffect(() => {
    setStatus(roomHandle.getStorageStatus());
    return roomHandle.subscribe("storage-status", setStatus);
  }, [roomHandle]);

  return status;
}

export function useStoragePendingMutations(room: string): OpenRTCStoragePendingMutation[] {
  const roomHandle = useRoomHandle(room);
  const [mutations, setMutations] = useState<OpenRTCStoragePendingMutation[]>(() =>
    roomHandle.getStoragePendingMutations(),
  );

  useEffect(() => {
    setMutations(roomHandle.getStoragePendingMutations());
    return roomHandle.subscribe("storage-status", () => {
      setMutations(roomHandle.getStoragePendingMutations());
    });
  }, [roomHandle]);

  return mutations;
}

export function useSetStorage<TDocument = unknown>(
  room: string,
): (document: TDocument, options?: OpenRTCStorageMutationOptions) => Promise<TDocument> {
  const roomHandle = useRoomHandle(room);

  return useCallback(
    (document: TDocument, options?: OpenRTCStorageMutationOptions) => roomHandle.setStorage<TDocument>(document, options),
    [roomHandle],
  );
}

export function usePatchStorage<TDocument = unknown>(
  room: string,
): (operations: JSONPatchOperation[], options?: OpenRTCStorageMutationOptions) => Promise<TDocument> {
  const roomHandle = useRoomHandle(room);

  return useCallback(
    (operations: JSONPatchOperation[], options?: OpenRTCStorageMutationOptions) =>
      roomHandle.patchStorage<TDocument>(operations, options),
    [roomHandle],
  );
}

export function useSetLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
  room: string,
): (
  data: TData | OpenRTCLiveObject<TData>,
  options?: OpenRTCStorageMutationOptions,
) => Promise<OpenRTCLiveObject<TData>> {
  const roomHandle = useRoomHandle(room);

  return useCallback(
    (data: TData | OpenRTCLiveObject<TData>, options?: OpenRTCStorageMutationOptions) =>
      roomHandle.setLiveStorage<TData>(data, options),
    [roomHandle],
  );
}

export function useUpdateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(
  room: string,
): (patch: Partial<TData>, options?: OpenRTCStorageMutationOptions) => Promise<OpenRTCLiveObject<TData>> {
  const roomHandle = useRoomHandle(room);

  return useCallback(
    (patch: Partial<TData>, options?: OpenRTCStorageMutationOptions) =>
      roomHandle.updateLiveStorage<TData>(patch, options),
    [roomHandle],
  );
}

export function useStorageMutation<TDocument = unknown, Args extends unknown[] = [], TResult = void>(
  room: string,
  mutation: (context: StorageMutationContext<TDocument>, ...args: Args) => TResult,
  deps: DependencyList = [],
): (...args: Args) => TResult {
  const roomHandle = useRoomHandle(room);
  const mutationRef = useRef(mutation);
  mutationRef.current = mutation;

  return useCallback((...args: Args) => {
    return mutationRef.current(
      {
        storage: roomHandle.getStorageSnapshot<TDocument>(),
        setStorage: (document, options) => roomHandle.setStorage<TDocument>(document, options),
        patchStorage: (operations, options) => roomHandle.patchStorage<TDocument>(operations, options),
        setLiveStorage: (data, options) => roomHandle.setLiveStorage(data, options),
        updateLiveStorage: (patch, options) => roomHandle.updateLiveStorage(patch, options),
      },
      ...args,
    );
  }, [roomHandle, ...deps]);
}

export function useStorageListener<TDocument = unknown>(
  room: string,
  callback: (event: OpenRTCStorageEvent<TDocument>) => void,
): void {
  const roomHandle = useRoomHandle(room);
  const stableCallback = useStableCallback(callback);

  useEffect(
    () =>
      roomHandle.subscribe("storage", (event) => {
        stableCallback(event as OpenRTCStorageEvent<TDocument>);
      }),
    [roomHandle, stableCallback],
  );
}

export function useEventListener(room: string, callback: (event: OpenRTCEvent) => void): void {
  const roomHandle = useRoomHandle(room);
  const stableCallback = useStableCallback(callback);

  useEffect(() => roomHandle.subscribe("event", stableCallback), [roomHandle, stableCallback]);
}

export function useCommentListener(room: string, callback: (event: OpenRTCCommentEvent) => void): void {
  const roomHandle = useRoomHandle(room);
  const stableCallback = useStableCallback(callback);

  useEffect(() => roomHandle.subscribe("comments", stableCallback), [roomHandle, stableCallback]);
}

export function useNotificationListener(callback: (event: OpenRTCNotificationDelta) => void): void {
  const client = useOpenRTC();
  const stableCallback = useStableCallback(callback);

  useEffect(() => client.on("notification", stableCallback), [client, stableCallback]);
}

export function useLostConnectionListener(
  room: string,
  callback: (event: OpenRTCLostConnectionEvent) => void,
): void {
  const roomHandle = useRoomHandle(room);
  const stableCallback = useStableCallback(callback);

  useEffect(() => roomHandle.subscribe("lost-connection", stableCallback), [roomHandle, stableCallback]);
}

export function useErrorListener(callback: (error: OpenRTCError) => void): void {
  const client = useOpenRTC();
  const stableCallback = useStableCallback(callback);

  useEffect(() => client.on("error", stableCallback), [client, stableCallback]);
}

export function useRoomReconnect(room: string): () => Promise<void> {
  const roomHandle = useRoomHandle(room);

  return useCallback(() => roomHandle.reconnect(), [roomHandle]);
}

export function useUpdateMyPresence(room: string): (state: PresenceState) => void {
  const client = useOpenRTC();
  return useCallback(
    (state: PresenceState) => {
      client.updatePresence(room, state);
    },
    [client, room],
  );
}

export function usePatchMyPresence(room: string): (patch: PresenceState) => void {
  const client = useOpenRTC();
  return useCallback(
    (patch: PresenceState) => {
      client.patchPresence(room, patch);
    },
    [client, room],
  );
}

export function useMyPresence(room: string, options: JoinOptions = {}): [PresenceState, (patch: PresenceState) => void] {
  const presence = usePresence(room, options);
  const patchPresence = usePatchMyPresence(room);

  return useMemo(
    () => [presence.self?.state ?? {}, patchPresence],
    [presence.self?.state, patchPresence],
  );
}

export function useMyPresenceSelector<T>(
  room: string,
  selector: MyPresenceSelector<T>,
  options: JoinOptions = {},
): T {
  const [presence] = useMyPresence(room, options);

  return useMemo(() => selector(presence), [presence, selector]);
}

export function useSetCursor(room: string): (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => void {
  const client = useOpenRTC();
  return useCallback(
    (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => {
      client.setCursor(room, cursor, options);
    },
    [client, room],
  );
}

export function useCursor(
  room: string,
  options: CursorOptions = {},
): [OpenRTCCursor | null, (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => void] {
  const cursor = useSelfCursor(room, options);
  const setCursor = useSetCursor(room);

  return useMemo<
    [OpenRTCCursor | null, (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => void]
  >(() => [cursor, setCursor], [cursor, setCursor]);
}

export function Cursor(props: CursorProps): ReactNode {
  const color = props.color ?? props.user?.color ?? "#4fd1b6";
  const showLabel = props.showLabel ?? true;
  const label = props.label ?? props.user?.name ?? props.user?.id ?? props.cursor.label ?? "Collaborator";
  const mode = props.mode ?? props.cursor.mode ?? "pointer";
  const positionStyle = cursorPositionStyle(props.cursor, props.coordinateSpace ?? "percent");

  return createElement(
    "div",
    {
      className: props.className,
      "data-openrtc-cursor": mode,
      style: {
        ...positionStyle,
        pointerEvents: "none",
        zIndex: 30,
        color,
        filter: "drop-shadow(0 10px 18px rgba(0,0,0,0.28))",
        transition: "left 90ms linear, top 90ms linear, transform 140ms ease-out",
        ...props.style,
      } satisfies CSSProperties,
    },
    createElement("div", {
      style: {
        width: 0,
        height: 0,
        borderLeft: "10px solid currentColor",
        borderTop: "8px solid transparent",
        borderBottom: "8px solid transparent",
        transform: "rotate(42deg)",
        transformOrigin: "2px 2px",
      } satisfies CSSProperties,
    }),
    showLabel
      ? createElement(
          "div",
          {
            style: {
              marginLeft: 14,
              marginTop: 10,
              width: "max-content",
              maxWidth: 220,
              padding: "5px 9px",
              borderRadius: 6,
              background: color,
              color: readableTextColor(color),
              fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
              fontSize: 12,
              fontWeight: 700,
              lineHeight: 1,
              boxShadow: "0 12px 26px rgba(0,0,0,0.22)",
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            } satisfies CSSProperties,
          },
          label,
        )
      : null,
  );
}

export function Cursors(props: CursorsProps): ReactNode {
  const {
    room,
    children,
    className,
    coordinateSpace = "percent",
    trackOwnCursor = true,
    clearOnPointerLeave = true,
    throttleMs = 16,
    mode = "pointer",
    cursorLabel,
    cursorOptions,
    showLabels = true,
    renderCursor,
    style,
  } = props;
  const cursors = useCursors(room, props);
  const setCursor = useSetCursor(room);
  const lastSentAt = useRef(0);

  const handlePointerMove = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      if (!trackOwnCursor) {
        return;
      }
      const now = Date.now();
      if (throttleMs > 0 && now - lastSentAt.current < throttleMs) {
        return;
      }
      lastSentAt.current = now;
      const rect = event.currentTarget.getBoundingClientRect();
      const point = pointerPoint(event, rect, coordinateSpace);
      setCursor(
        {
          ...point,
          mode,
          ...(cursorLabel ? { label: cursorLabel } : {}),
        },
        {
          ...(cursorOptions ?? {}),
          ...(cursorOptions?.mode === undefined ? { mode } : {}),
        },
      );
    },
    [coordinateSpace, cursorLabel, cursorOptions, mode, setCursor, throttleMs, trackOwnCursor],
  );

  const handlePointerLeave = useCallback(() => {
    if (trackOwnCursor && clearOnPointerLeave) {
      setCursor(null, cursorOptions);
    }
  }, [clearOnPointerLeave, cursorOptions, setCursor, trackOwnCursor]);

  return createElement(
    "div",
    {
      className,
      onPointerMove: handlePointerMove,
      onPointerLeave: handlePointerLeave,
      style: {
        position: "relative",
        overflow: "hidden",
        touchAction: trackOwnCursor ? "none" : undefined,
        ...style,
      } satisfies CSSProperties,
    },
    children,
    createElement(
      "div",
      {
        "aria-hidden": true,
        style: {
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
        } satisfies CSSProperties,
      },
      cursors.map((cursor) =>
        createElement(
          "div",
          { key: cursor.connId },
          renderCursor
            ? renderCursor(cursor)
            : createElement(Cursor, {
                cursor: cursor.cursor,
                showLabel: showLabels,
                coordinateSpace,
                ...(cursor.user ? { user: cursor.user } : {}),
                ...(cursor.color ? { color: cursor.color } : {}),
                ...(cursor.mode ? { mode: cursor.mode } : {}),
              }),
        ),
      ),
    ),
  );
}

export function AvatarStack(props: AvatarStackProps): ReactNode {
  const presence = usePresence(props.room, props);
  const peers = useMemo(() => {
    const candidates = props.includeSelf && presence.self ? [presence.self, ...presence.others] : presence.others;
    return candidates.map(toAvatarPeer);
  }, [presence.others, presence.self, props.includeSelf]);
  const max = props.max === null ? peers.length : Math.max(1, Math.floor(props.max ?? 5));
  const visiblePeers = peers.slice(0, max);
  const overflow = Math.max(0, peers.length - visiblePeers.length);
  const size = Math.max(20, Math.floor(props.size ?? 32));
  const gap = Math.floor(props.gap ?? -8);

  return createElement(
    "div",
    {
      className: props.className,
      "aria-label": `${peers.length} active collaborator${peers.length === 1 ? "" : "s"}`,
      style: {
        display: "flex",
        alignItems: "center",
        minHeight: size,
        ...props.style,
      } satisfies CSSProperties,
    },
    visiblePeers.map((avatar, index) =>
      createElement(
        "div",
        {
          key: avatar.peer.connId,
          title: avatar.label,
          style: {
            marginLeft: index === 0 ? 0 : gap,
            zIndex: visiblePeers.length - index,
          } satisfies CSSProperties,
        },
        props.renderAvatar
          ? props.renderAvatar(avatar.peer, index)
          : createElement(
              "div",
              {
                style: {
                  width: size,
                  height: size,
                  borderRadius: "50%",
                  display: "grid",
                  placeItems: "center",
                  background: avatar.color,
                  color: readableTextColor(avatar.color),
                  border: "2px solid rgba(255,255,255,0.92)",
                  boxShadow: "0 8px 18px rgba(0,0,0,0.18)",
                  fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
                  fontSize: Math.max(10, Math.floor(size * 0.34)),
                  fontWeight: 800,
                  letterSpacing: 0,
                  lineHeight: 1,
                } satisfies CSSProperties,
              },
              avatar.initials,
            ),
      ),
    ),
    overflow > 0
      ? createElement(
          "div",
          {
            title: `${overflow} more active collaborator${overflow === 1 ? "" : "s"}`,
            style: {
              width: size,
              height: size,
              marginLeft: gap,
              borderRadius: "50%",
              display: "grid",
              placeItems: "center",
              background: "#111827",
              color: "#ffffff",
              border: "2px solid rgba(255,255,255,0.92)",
              boxShadow: "0 8px 18px rgba(0,0,0,0.18)",
              fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
              fontSize: Math.max(10, Math.floor(size * 0.32)),
              fontWeight: 800,
              lineHeight: 1,
            } satisfies CSSProperties,
          },
          `+${overflow}`,
        )
      : null,
  );
}

export function useRoomEvents(room: string, limit = 200): OpenRTCEvent[] {
  const client = useOpenRTC();
  const [events, setEvents] = useState<OpenRTCEvent[]>([]);
  const maxEvents = normalizeEventLimit(limit, 200);

  useEffect(() => {
    return client.on("event", (event) => {
      if (event.room === room) {
        setEvents((current) => [...current.slice(1 - maxEvents), event]);
      }
    });
  }, [client, room, maxEvents]);

  return useMemo(() => events, [events]);
}

export function useRoomCommentEvents(room: string, limit = 200): OpenRTCCommentEvent[] {
  const client = useOpenRTC();
  const [events, setEvents] = useState<OpenRTCCommentEvent[]>([]);
  const maxEvents = normalizeEventLimit(limit, 200);

  useEffect(() => {
    return client.on("comment", (event) => {
      if (event.room === room) {
        setEvents((current) => [...current.slice(1 - maxEvents), event]);
      }
    });
  }, [client, room, maxEvents]);

  return useMemo(() => events, [events]);
}

export function useNotificationEvents(limit = 200): OpenRTCNotificationDelta[] {
  const client = useOpenRTC();
  const [events, setEvents] = useState<OpenRTCNotificationDelta[]>([]);
  const maxEvents = normalizeEventLimit(limit, 200);

  useEffect(() => {
    return client.on("notification", (event) => {
      setEvents((current) => [...current.slice(1 - maxEvents), event]);
    });
  }, [client, maxEvents]);

  return useMemo(() => events, [events]);
}

function useInitialPresence(room: string, initialPresence: PresenceState | undefined): PresenceState | undefined {
  const ref = useRef<{ room: string; presence: PresenceState | undefined } | undefined>(undefined);
  if (!ref.current || ref.current.room !== room) {
    ref.current = { room, presence: initialPresence };
  }
  return ref.current.presence;
}

function useStableCallback<Args extends unknown[], ReturnValue>(
  callback: (...args: Args) => ReturnValue,
): (...args: Args) => ReturnValue {
  const ref = useRef(callback);
  ref.current = callback;

  return useCallback((...args: Args) => ref.current(...args), []);
}

export function useDiagnostics(limit = 200): OpenRTCDiagnosticEvent[] {
  const client = useOpenRTC();
  const [events, setEvents] = useState<OpenRTCDiagnosticEvent[]>([]);
  const maxEvents = normalizeEventLimit(limit, 200);

  useEffect(() => {
    return client.on("diagnostic", (event) => {
      setEvents((current) => [...current.slice(1 - maxEvents), event]);
    });
  }, [client, maxEvents]);

  return useMemo(() => events, [events]);
}

function cursorPositionStyle(cursor: OpenRTCCursor, coordinateSpace: CursorCoordinateSpace): CSSProperties {
  return {
    position: "absolute",
    left: coordinateSpace === "percent" ? `${clamp(cursor.x, 0, 100)}%` : cursor.x,
    top: coordinateSpace === "percent" ? `${clamp(cursor.y, 0, 100)}%` : cursor.y,
    transform: "translate3d(0, 0, 0)",
  };
}

function pointerPoint(
  event: ReactPointerEvent<HTMLDivElement>,
  rect: { left: number; top: number; width: number; height: number },
  coordinateSpace: CursorCoordinateSpace,
): { x: number; y: number } {
  const x = event.clientX - rect.left;
  const y = event.clientY - rect.top;
  if (coordinateSpace === "pixel") {
    return { x: Math.round(x), y: Math.round(y) };
  }
  return {
    x: round2(clamp((x / Math.max(1, rect.width)) * 100, 0, 100)),
    y: round2(clamp((y / Math.max(1, rect.height)) * 100, 0, 100)),
  };
}

function toAvatarPeer(peer: PresencePeer): AvatarPeer {
  const user = getPresenceUser(peer.state);
  const color = getPresenceColor(peer.state) ?? colorFromString(user?.id ?? user?.name ?? peer.connId);
  const label = user?.name ?? user?.id ?? peer.connId;
  return {
    peer,
    ...(user ? { user } : {}),
    color,
    label,
    initials: initialsFor(label),
  };
}

function initialsFor(label: string): string {
  const words = label.trim().split(/[\s._@-]+/).filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  const initials = words.slice(0, 2).map((word) => word[0]?.toUpperCase() ?? "");
  return initials.join("") || "?";
}

function colorFromString(value: string): string {
  const palette = ["#4fd1b6", "#7aa7ff", "#ffd166", "#ff7a90", "#a78bfa", "#f97316", "#22c55e"];
  let hash = 0;
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 31 + value.charCodeAt(index)) >>> 0;
  }
  return palette[hash % palette.length] ?? palette[0]!;
}

function readableTextColor(color: string): string {
  const hex = color.trim().replace(/^#/, "");
  if (hex.length !== 3 && hex.length !== 6) {
    return "#ffffff";
  }
  const normalized =
    hex.length === 3
      ? hex
          .split("")
          .map((char) => char + char)
          .join("")
      : hex;
  const value = Number.parseInt(normalized, 16);
  if (!Number.isFinite(value)) {
    return "#ffffff";
  }
  const r = (value >> 16) & 255;
  const g = (value >> 8) & 255;
  const b = value & 255;
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  return luminance > 0.62 ? "#111827" : "#ffffff";
}

function round2(value: number): number {
  return Math.round(value * 100) / 100;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function normalizeEventLimit(limit: number, fallback: number): number {
  if (!Number.isFinite(limit) || limit <= 0) {
    return fallback;
  }
  return Math.max(1, Math.floor(limit));
}
