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
  type ChangeEvent,
  type Context,
  type DependencyList,
  type FormEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import {
  OpenRTCClient,
  addCommentMention,
  addCommentReaction,
  applyCommentEventToThreads,
  applyNotificationDeltaToInbox,
  getCursorPeers,
  getPresenceColor,
  getPresenceCursor,
  getPresenceUser,
  roomSubscriptionSettingsInput,
  removeCommentMention,
  removeCommentReaction,
  sortCommentThreads,
  sortInboxNotifications,
  type BroadcastOptions,
  type ConnectionStatus,
  type EnterRoomOptions,
  type JSONPatchOperation,
  type JoinOptions,
  type OpenRTCAdminComment,
  type OpenRTCAdminCommentInput,
  type OpenRTCAdminCommentReaction,
  type OpenRTCAdminCommentUpdate,
  type OpenRTCAdminInboxNotification,
  type OpenRTCAdminInboxNotificationInput,
  type OpenRTCAdminClient,
  type OpenRTCAdminRoomSubscriptionSettings,
  type OpenRTCAdminRoomSubscriptionSettingsInput,
  type OpenRTCAdminThread,
  type OpenRTCAdminThreadInput,
  type OpenRTCAdminThreadListOptions,
  type OpenRTCAdminThreadReadState,
  type OpenRTCAdminThreadUpdate,
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
  type OpenRTCLiveStorageMutationInput,
  type OpenRTCInboxMaterializationOptions,
  type OpenRTCStorageEvent,
  type OpenRTCStorageHistory,
  type OpenRTCStorageMutationOptions,
  type OpenRTCStoragePendingMutation,
  type OpenRTCStorageStatus,
  type PresencePeer,
  type PresenceState,
  type RoomBroadcastInput,
} from "@openrtc/client";

const OpenRTCContext = createContext<OpenRTCClient | null>(null);
const OpenRTCAdminContext = createContext<OpenRTCAdminClient | null>(null);
const OpenRTCRoomContext = createContext<OpenRTCRoom | null>(null);

export type RoomPresenceSelector<T> = (presence: OpenRTCRoomPresence) => T;
export type OtherSelector<T> = (other: PresencePeer) => T;
export type CursorSelector<T> = (cursor: OpenRTCCursorPeer) => T;
export type SelfSelector<T> = (self: PresencePeer) => T;
export type MyPresenceSelector<T> = (presence: PresenceState) => T;
export type StorageSelector<TDocument, TSelected> = (document: TDocument | undefined) => TSelected;
export type StorageSelectorEquality<TSelected> = (previous: TSelected, next: TSelected) => boolean;

export interface OpenRTCRoomProviderProps extends EnterRoomOptions {
  id: string;
  children: ReactNode;
}

export type RoomProviderProps = OpenRTCRoomProviderProps;

export interface StorageSelectorOptions<TSelected> extends JoinOptions {
  isEqual?: StorageSelectorEquality<TSelected>;
}

export interface RoomThreadsOptions extends JoinOptions {
  initialThreads?: readonly OpenRTCAdminThread[];
  admin?: OpenRTCAdminClient | null;
  fetch?: boolean;
  query?: string;
  userId?: string;
}

export interface RoomThreadsState {
  threads: OpenRTCAdminThread[];
  loading: boolean;
  error?: unknown;
  refresh(): Promise<OpenRTCAdminThread[]>;
}

export interface InboxNotificationsOptions extends OpenRTCInboxMaterializationOptions {
  initialNotifications?: readonly OpenRTCAdminInboxNotification[];
  admin?: OpenRTCAdminClient | null;
  fetch?: boolean;
  cursor?: string;
  startingAfter?: string;
}

export interface InboxNotificationsState {
  notifications: OpenRTCAdminInboxNotification[];
  loading: boolean;
  error?: unknown;
  refresh(): Promise<OpenRTCAdminInboxNotification[]>;
}

export interface RoomSubscriptionSettingsOptions extends OpenRTCAdminActionOptions {
  initialSettings?: OpenRTCAdminRoomSubscriptionSettings;
  fetch?: boolean;
}

export interface RoomSubscriptionSettingsState {
  settings?: OpenRTCAdminRoomSubscriptionSettings;
  loading: boolean;
  error?: unknown;
  refresh(): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
  update(settings: OpenRTCAdminRoomSubscriptionSettingsInput): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
  reset(): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
  subscribeAll(): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
  subscribeRepliesAndMentions(): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
  mute(): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>;
}

export interface UserRoomSubscriptionSettingsOptions extends OpenRTCAdminActionOptions {
  initialSettings?: readonly OpenRTCAdminRoomSubscriptionSettings[];
  fetch?: boolean;
  limit?: number;
  cursor?: string;
}

export interface UserRoomSubscriptionSettingsState {
  settings: OpenRTCAdminRoomSubscriptionSettings[];
  loading: boolean;
  error?: unknown;
  refresh(): Promise<OpenRTCAdminRoomSubscriptionSettings[]>;
}

export interface OpenRTCAdminActionOptions {
  admin?: OpenRTCAdminClient | null;
}

export interface CommentReactionActionInput {
  threadId: string;
  commentId: string;
  reaction: OpenRTCAdminCommentReaction;
  currentReactions?: readonly OpenRTCAdminCommentReaction[];
}

export interface CommentMentionActionInput {
  threadId: string;
  commentId: string;
  userId: string;
  currentMentions?: readonly string[];
}

export type CommentsPanelBodyKind = "thread" | "comment";

export interface CommentsPanelBodyContext {
  kind: CommentsPanelBodyKind;
  thread?: OpenRTCAdminThread;
}

export interface CommentsPanelMetadataContext extends CommentsPanelBodyContext {
  text: string;
}

export type CommentsPanelMetadataValue =
  | null
  | boolean
  | number
  | string
  | readonly CommentsPanelMetadataValue[]
  | { readonly [key: string]: CommentsPanelMetadataValue };

export type CommentsPanelMetadata =
  | CommentsPanelMetadataValue
  | ((context: CommentsPanelMetadataContext) => unknown);

export interface CommentsPanelRenderCommentContext {
  thread: OpenRTCAdminThread;
  comment: OpenRTCAdminComment;
  index: number;
  text: ReactNode;
}

export interface CommentsPanelRenderThreadActionsContext {
  thread: OpenRTCAdminThread;
  pending: boolean;
  markRead(): Promise<void>;
  markUnread(): Promise<void>;
  resolve(): Promise<void>;
  unresolve(): Promise<void>;
}

export interface CommentsPanelProps extends RoomThreadsOptions {
  room: string;
  userId: string;
  title?: ReactNode;
  emptyState?: ReactNode;
  className?: string;
  style?: CSSProperties;
  composerPlaceholder?: string;
  replyPlaceholder?: string;
  showComposer?: boolean;
  showResolved?: boolean;
  showReadActions?: boolean;
  showResolveActions?: boolean;
  bodyFromText?: (text: string, context: CommentsPanelBodyContext) => unknown;
  textFromBody?: (body: unknown, context: CommentsPanelBodyContext) => ReactNode;
  threadMetadata?: CommentsPanelMetadata;
  commentMetadata?: CommentsPanelMetadata;
  renderComment?: (context: CommentsPanelRenderCommentContext) => ReactNode;
  renderThreadActions?: (context: CommentsPanelRenderThreadActionsContext) => ReactNode;
  onThreadCreated?: (thread: OpenRTCAdminThread) => void;
  onCommentCreated?: (thread: OpenRTCAdminThread) => void;
  onThreadUpdated?: (thread: OpenRTCAdminThread) => void;
}

export type RoomCommentsPanelProps = Omit<CommentsPanelProps, "room">;

export interface RoomSubscriptionControlsProps extends RoomSubscriptionSettingsOptions {
  room: string;
  userId: string;
  title?: ReactNode;
  className?: string;
  style?: CSSProperties;
  disabled?: boolean;
  showReset?: boolean;
  onSettingsChanged?: (settings: OpenRTCAdminRoomSubscriptionSettings) => void;
}

export type RoomSubscriptionControlsRoomProps = Omit<RoomSubscriptionControlsProps, "room">;

export interface EditCommentMetadataInput {
  threadId: string;
  commentId: string;
  metadata: unknown;
}

export interface EditThreadMetadataInput {
  threadId: string;
  metadata: unknown;
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
  mutateLiveStorage<TDocument = OpenRTCLiveObject>(
    mutation: OpenRTCLiveStorageMutationInput,
    options?: OpenRTCStorageMutationOptions,
  ): Promise<TDocument>;
}

export interface OpenRTCMutationContext<TDocument = unknown> extends StorageMutationContext<TDocument> {
  room: OpenRTCRoom;
  self: PresencePeer | undefined;
  others: PresencePeer[];
  myPresence: PresenceState;
  setMyPresence(state: PresenceState): void;
  updateMyPresence(patch: PresenceState): void;
  broadcastEvent(event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions): string;
  broadcastEventWithAck(
    event: RoomBroadcastInput,
    payload?: unknown,
    options?: BroadcastOptions,
  ): Promise<OpenRTCEvent>;
}

export interface OpenRTCRoomContextHooks {
  RoomProvider(props: OpenRTCRoomProviderProps): ReactNode;
  CommentsPanel(props: RoomCommentsPanelProps): ReactNode;
  RoomSubscriptionControls(props: RoomSubscriptionControlsRoomProps): ReactNode;
  useRoom(): OpenRTCRoom;
  useStatus(): ConnectionStatus;
  useOthers(options?: JoinOptions): PresencePeer[];
  useOthersMapped<T>(selector: OtherSelector<T>, options?: JoinOptions): T[];
  useOthersConnectionIds(options?: JoinOptions): string[];
  useOther<T = PresencePeer>(
    connId: string,
    selectorOrOptions?: OtherSelector<T> | JoinOptions,
    options?: JoinOptions,
  ): PresencePeer | T | undefined;
  usePresence(options?: JoinOptions): OpenRTCRoomPresence;
  useSelf<T = PresencePeer>(
    selectorOrOptions?: SelfSelector<T> | JoinOptions,
    options?: JoinOptions,
  ): PresencePeer | T | undefined;
  useMyPresence(options?: JoinOptions): [PresenceState, (patch: PresenceState) => void];
  useMyPresenceSelector<T>(selector: MyPresenceSelector<T>, options?: JoinOptions): T;
  useUpdateMyPresence(): (state: PresenceState) => void;
  usePatchMyPresence(): (patch: PresenceState) => void;
  useBroadcastEvent(): (event: RoomBroadcastInput, payload?: unknown, options?: BroadcastOptions) => string;
  useBroadcastEventWithAck(): (
    event: RoomBroadcastInput,
    payload?: unknown,
    options?: BroadcastOptions,
  ) => Promise<OpenRTCEvent>;
  useStorage<TDocument = unknown>(options?: JoinOptions): TDocument | undefined;
  useStorageSelector<TDocument, TSelected>(
    selector: StorageSelector<TDocument, TSelected>,
    options?: StorageSelectorOptions<TSelected>,
  ): TSelected;
  useStorageStatus(): OpenRTCStorageStatus;
  useStorageSequence(): number | undefined;
  useStoragePendingMutations(): OpenRTCStoragePendingMutation[];
  useHistory(): OpenRTCStorageHistory;
  useUndo<TDocument = unknown>(): (options?: OpenRTCStorageMutationOptions) => Promise<TDocument | undefined>;
  useRedo<TDocument = unknown>(): (options?: OpenRTCStorageMutationOptions) => Promise<TDocument | undefined>;
  useCanUndo(): boolean;
  useCanRedo(): boolean;
  useSetStorage<TDocument = unknown>(): (
    document: TDocument,
    options?: OpenRTCStorageMutationOptions,
  ) => Promise<TDocument>;
  usePatchStorage<TDocument = unknown>(): (
    operations: JSONPatchOperation[],
    options?: OpenRTCStorageMutationOptions,
  ) => Promise<TDocument>;
  useSetLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(): (
    data: TData | OpenRTCLiveObject<TData>,
    options?: OpenRTCStorageMutationOptions,
  ) => Promise<OpenRTCLiveObject<TData>>;
  useUpdateLiveStorage<TData extends Record<string, unknown> = Record<string, unknown>>(): (
    patch: Partial<TData>,
    options?: OpenRTCStorageMutationOptions,
  ) => Promise<OpenRTCLiveObject<TData>>;
  useMutateLiveStorage<TDocument = OpenRTCLiveObject>(): (
    mutation: OpenRTCLiveStorageMutationInput,
    options?: OpenRTCStorageMutationOptions,
  ) => Promise<TDocument>;
  useStorageMutation<TDocument = unknown, Args extends unknown[] = [], TResult = void>(
    mutation: (context: StorageMutationContext<TDocument>, ...args: Args) => TResult,
    deps?: DependencyList,
  ): (...args: Args) => TResult;
  useMutation<TDocument = unknown, Args extends unknown[] = [], TResult = void>(
    mutation: (context: OpenRTCMutationContext<TDocument>, ...args: Args) => TResult,
    deps?: DependencyList,
  ): (...args: Args) => TResult;
  useStorageListener<TDocument = unknown>(callback: (event: OpenRTCStorageEvent<TDocument>) => void): void;
  useEventListener(callback: (event: OpenRTCEvent) => void): void;
  useCommentListener(callback: (event: OpenRTCCommentEvent) => void): void;
  useThreadsState(options?: RoomThreadsOptions): RoomThreadsState;
  useThreads(options?: RoomThreadsOptions): OpenRTCAdminThread[];
  useThread(threadId: string, options?: RoomThreadsOptions): OpenRTCAdminThread | undefined;
  useCommentEvents(limit?: number): OpenRTCCommentEvent[];
  useGetThread(options?: OpenRTCAdminActionOptions): (threadId: string) => Promise<OpenRTCAdminThread>;
  useCreateThread(options?: OpenRTCAdminActionOptions): (thread: OpenRTCAdminThreadInput) => Promise<OpenRTCAdminThread>;
  useEditThread(
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string, update: OpenRTCAdminThreadUpdate) => Promise<OpenRTCAdminThread>;
  useEditThreadMetadata(
    options?: OpenRTCAdminActionOptions,
  ): (input: EditThreadMetadataInput) => Promise<OpenRTCAdminThread>;
  useMarkThreadResolved(options?: OpenRTCAdminActionOptions): (threadId: string) => Promise<OpenRTCAdminThread>;
  useMarkThreadUnresolved(options?: OpenRTCAdminActionOptions): (threadId: string) => Promise<OpenRTCAdminThread>;
  useGetThreadReadState(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string) => Promise<OpenRTCAdminThreadReadState>;
  useMarkThreadRead(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string) => Promise<OpenRTCAdminThreadReadState>;
  useMarkThreadUnread(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string) => Promise<OpenRTCAdminThreadReadState>;
  useDeleteThread(options?: OpenRTCAdminActionOptions): (threadId: string) => Promise<void>;
  useCreateComment(
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string, comment: OpenRTCAdminCommentInput) => Promise<OpenRTCAdminThread>;
  useEditComment(
    options?: OpenRTCAdminActionOptions,
  ): (threadId: string, commentId: string, update: OpenRTCAdminCommentUpdate) => Promise<OpenRTCAdminThread>;
  useEditCommentMetadata(
    options?: OpenRTCAdminActionOptions,
  ): (input: EditCommentMetadataInput) => Promise<OpenRTCAdminThread>;
  useAddReaction(options?: OpenRTCAdminActionOptions): (input: CommentReactionActionInput) => Promise<OpenRTCAdminThread>;
  useRemoveReaction(
    options?: OpenRTCAdminActionOptions,
  ): (input: CommentReactionActionInput) => Promise<OpenRTCAdminThread>;
  useAddCommentMention(
    options?: OpenRTCAdminActionOptions,
  ): (input: CommentMentionActionInput) => Promise<OpenRTCAdminThread>;
  useRemoveCommentMention(
    options?: OpenRTCAdminActionOptions,
  ): (input: CommentMentionActionInput) => Promise<OpenRTCAdminThread>;
  useRoomSubscriptionSettingsState(
    userId: string,
    options?: RoomSubscriptionSettingsOptions,
  ): RoomSubscriptionSettingsState;
  useRoomSubscriptionSettings(
    userId: string,
    options?: RoomSubscriptionSettingsOptions,
  ): OpenRTCAdminRoomSubscriptionSettings | undefined;
  useGetRoomSubscriptionSettings(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): () => Promise<OpenRTCAdminRoomSubscriptionSettings>;
  useUpdateRoomSubscriptionSettings(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): (settings: OpenRTCAdminRoomSubscriptionSettingsInput) => Promise<OpenRTCAdminRoomSubscriptionSettings>;
  useSubscribeRoomThreads(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): () => Promise<OpenRTCAdminRoomSubscriptionSettings>;
  useSubscribeRoomRepliesAndMentions(
    userId: string,
    options?: OpenRTCAdminActionOptions,
  ): () => Promise<OpenRTCAdminRoomSubscriptionSettings>;
  useMuteRoomThreads(userId: string, options?: OpenRTCAdminActionOptions): () => Promise<OpenRTCAdminRoomSubscriptionSettings>;
  useResetRoomSubscriptionSettings(userId: string, options?: OpenRTCAdminActionOptions): () => Promise<void>;
  useLostConnectionListener(callback: (event: OpenRTCLostConnectionEvent) => void): void;
  useRoomReconnect(): () => Promise<void>;
  useSetCursor(): (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => void;
  useCursor(options?: CursorOptions): [
    OpenRTCCursor | null,
    (cursor: OpenRTCCursor | null, options?: OpenRTCCursorOptions) => void,
  ];
  useSelfCursor(options?: CursorOptions): OpenRTCCursor | null;
  useCursors(options?: CursorOptions): OpenRTCCursorPeer[];
  useOtherCursors(options?: CursorOptions): OpenRTCCursorPeer[];
  useCursorsMapped<T>(selector: CursorSelector<T>, options?: CursorOptions): T[];
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

export interface OpenRTCAdminProviderProps {
  admin: OpenRTCAdminClient;
  children: ReactNode;
}

export function OpenRTCProvider(props: OpenRTCProviderProps) {
  return createElement(OpenRTCContext.Provider, { value: props.client }, props.children);
}

export function OpenRTCAdminProvider(props: OpenRTCAdminProviderProps) {
  return createElement(OpenRTCAdminContext.Provider, { value: props.admin }, props.children);
}

export function useOpenRTC(): OpenRTCClient {
  const client = useContext(OpenRTCContext);
  if (!client) {
    throw new Error("useOpenRTC must be used inside OpenRTCProvider");
  }
  return client;
}

export function useOpenRTCAdmin(): OpenRTCAdminClient {
  const admin = useContext(OpenRTCAdminContext);
  if (!admin) {
    throw new Error("useOpenRTCAdmin must be used inside OpenRTCAdminProvider");
  }
  return admin;
}

export function RoomProvider(props: OpenRTCRoomProviderProps): ReactNode {
  return renderRoomProvider(OpenRTCRoomContext, props);
}

export function useCurrentRoom(): OpenRTCRoom {
  return useRoomFromContext(OpenRTCRoomContext, "useCurrentRoom");
}

export function createRoomContext(): OpenRTCRoomContextHooks {
  return createRoomContextHooks(createContext<OpenRTCRoom | null>(null));
}

function createRoomContextHooks(context: Context<OpenRTCRoom | null>): OpenRTCRoomContextHooks {
  function BoundCommentsPanel(props: RoomCommentsPanelProps): ReactNode {
    const room = useRoomFromContext(context, "CommentsPanel").id;
    return createElement(CommentsPanel, { ...props, room });
  }

  function BoundRoomSubscriptionControls(props: RoomSubscriptionControlsRoomProps): ReactNode {
    const room = useRoomFromContext(context, "RoomSubscriptionControls").id;
    return createElement(RoomSubscriptionControls, { ...props, room });
  }

  function useBoundOther(connId: string, options?: JoinOptions): PresencePeer | undefined;
  function useBoundOther<T>(connId: string, selector: OtherSelector<T>, options?: JoinOptions): T | undefined;
  function useBoundOther<T>(
    connId: string,
    selectorOrOptions?: OtherSelector<T> | JoinOptions,
    options: JoinOptions = {},
  ): PresencePeer | T | undefined {
    const room = useRoomFromContext(context, "useOther").id;
    if (typeof selectorOrOptions === "function") {
      return useOther(room, connId, selectorOrOptions, options);
    }
    return useOther(room, connId, selectorOrOptions);
  }

  function useBoundSelf(options?: JoinOptions): PresencePeer | undefined;
  function useBoundSelf<T>(selector: SelfSelector<T>, options?: JoinOptions): T | undefined;
  function useBoundSelf<T>(
    selectorOrOptions?: SelfSelector<T> | JoinOptions,
    options: JoinOptions = {},
  ): PresencePeer | T | undefined {
    const room = useRoomFromContext(context, "useSelf").id;
    if (typeof selectorOrOptions === "function") {
      return useSelf(room, selectorOrOptions, options);
    }
    return useSelf(room, selectorOrOptions);
  }

  return {
    RoomProvider: (props) => renderRoomProvider(context, props),
    CommentsPanel: BoundCommentsPanel,
    RoomSubscriptionControls: BoundRoomSubscriptionControls,
    useRoom: () => useRoomFromContext(context, "useRoom"),
    useStatus: () => useRoomStatus(useRoomFromContext(context, "useStatus").id),
    useOthers: (options = {}) => useOthers(useRoomFromContext(context, "useOthers").id, options),
    useOthersMapped: (selector, options = {}) =>
      useOthersMapped(useRoomFromContext(context, "useOthersMapped").id, selector, options),
    useOthersConnectionIds: (options = {}) =>
      useOthersConnectionIds(useRoomFromContext(context, "useOthersConnectionIds").id, options),
    useOther: useBoundOther,
    usePresence: (options = {}) => usePresence(useRoomFromContext(context, "usePresence").id, options),
    useSelf: useBoundSelf,
    useMyPresence: (options = {}) => useMyPresence(useRoomFromContext(context, "useMyPresence").id, options),
    useMyPresenceSelector: (selector, options = {}) =>
      useMyPresenceSelector(useRoomFromContext(context, "useMyPresenceSelector").id, selector, options),
    useUpdateMyPresence: () => useUpdateMyPresence(useRoomFromContext(context, "useUpdateMyPresence").id),
    usePatchMyPresence: () => usePatchMyPresence(useRoomFromContext(context, "usePatchMyPresence").id),
    useBroadcastEvent: () => useBroadcastEvent(useRoomFromContext(context, "useBroadcastEvent").id),
    useBroadcastEventWithAck: () =>
      useBroadcastEventWithAck(useRoomFromContext(context, "useBroadcastEventWithAck").id),
    useStorage: (options = {}) => useStorage(useRoomFromContext(context, "useStorage").id, options),
    useStorageSelector: (selector, options = {}) =>
      useStorageSelector(useRoomFromContext(context, "useStorageSelector").id, selector, options),
    useStorageStatus: () => useStorageStatus(useRoomFromContext(context, "useStorageStatus").id),
    useStorageSequence: () => useStorageSequence(useRoomFromContext(context, "useStorageSequence").id),
    useStoragePendingMutations: () =>
      useStoragePendingMutations(useRoomFromContext(context, "useStoragePendingMutations").id),
    useHistory: () => useHistory(useRoomFromContext(context, "useHistory").id),
    useUndo: () => useUndo(useRoomFromContext(context, "useUndo").id),
    useRedo: () => useRedo(useRoomFromContext(context, "useRedo").id),
    useCanUndo: () => useCanUndo(useRoomFromContext(context, "useCanUndo").id),
    useCanRedo: () => useCanRedo(useRoomFromContext(context, "useCanRedo").id),
    useSetStorage: () => useSetStorage(useRoomFromContext(context, "useSetStorage").id),
    usePatchStorage: () => usePatchStorage(useRoomFromContext(context, "usePatchStorage").id),
    useSetLiveStorage: () => useSetLiveStorage(useRoomFromContext(context, "useSetLiveStorage").id),
    useUpdateLiveStorage: () => useUpdateLiveStorage(useRoomFromContext(context, "useUpdateLiveStorage").id),
    useMutateLiveStorage: () => useMutateLiveStorage(useRoomFromContext(context, "useMutateLiveStorage").id),
    useStorageMutation: (mutation, deps = []) =>
      useStorageMutation(useRoomFromContext(context, "useStorageMutation").id, mutation, deps),
    useMutation: (mutation, deps = []) => useMutation(useRoomFromContext(context, "useMutation").id, mutation, deps),
    useStorageListener: (callback) =>
      useStorageListener(useRoomFromContext(context, "useStorageListener").id, callback),
    useEventListener: (callback) => useEventListener(useRoomFromContext(context, "useEventListener").id, callback),
    useCommentListener: (callback) =>
      useCommentListener(useRoomFromContext(context, "useCommentListener").id, callback),
    useThreadsState: (options = {}) =>
      useRoomThreadsState(useRoomFromContext(context, "useThreadsState").id, options),
    useThreads: (options = {}) => useRoomThreads(useRoomFromContext(context, "useThreads").id, options),
    useThread: (threadId, options = {}) => useRoomThread(useRoomFromContext(context, "useThread").id, threadId, options),
    useCommentEvents: (limit = 200) => useRoomCommentEvents(useRoomFromContext(context, "useCommentEvents").id, limit),
    useGetThread: (options = {}) => useGetThread(useRoomFromContext(context, "useGetThread").id, options),
    useCreateThread: (options = {}) => useCreateThread(useRoomFromContext(context, "useCreateThread").id, options),
    useEditThread: (options = {}) => useEditThread(useRoomFromContext(context, "useEditThread").id, options),
    useEditThreadMetadata: (options = {}) =>
      useEditThreadMetadata(useRoomFromContext(context, "useEditThreadMetadata").id, options),
    useMarkThreadResolved: (options = {}) =>
      useMarkThreadResolved(useRoomFromContext(context, "useMarkThreadResolved").id, options),
    useMarkThreadUnresolved: (options = {}) =>
      useMarkThreadUnresolved(useRoomFromContext(context, "useMarkThreadUnresolved").id, options),
    useGetThreadReadState: (userId, options = {}) =>
      useGetThreadReadState(useRoomFromContext(context, "useGetThreadReadState").id, userId, options),
    useMarkThreadRead: (userId, options = {}) =>
      useMarkThreadRead(useRoomFromContext(context, "useMarkThreadRead").id, userId, options),
    useMarkThreadUnread: (userId, options = {}) =>
      useMarkThreadUnread(useRoomFromContext(context, "useMarkThreadUnread").id, userId, options),
    useDeleteThread: (options = {}) => useDeleteThread(useRoomFromContext(context, "useDeleteThread").id, options),
    useCreateComment: (options = {}) => useCreateComment(useRoomFromContext(context, "useCreateComment").id, options),
    useEditComment: (options = {}) => useEditComment(useRoomFromContext(context, "useEditComment").id, options),
    useEditCommentMetadata: (options = {}) =>
      useEditCommentMetadata(useRoomFromContext(context, "useEditCommentMetadata").id, options),
    useAddReaction: (options = {}) => useAddReaction(useRoomFromContext(context, "useAddReaction").id, options),
    useRemoveReaction: (options = {}) => useRemoveReaction(useRoomFromContext(context, "useRemoveReaction").id, options),
    useAddCommentMention: (options = {}) =>
      useAddCommentMention(useRoomFromContext(context, "useAddCommentMention").id, options),
    useRemoveCommentMention: (options = {}) =>
      useRemoveCommentMention(useRoomFromContext(context, "useRemoveCommentMention").id, options),
    useRoomSubscriptionSettingsState: (userId, options = {}) =>
      useRoomSubscriptionSettingsState(
        useRoomFromContext(context, "useRoomSubscriptionSettingsState").id,
        userId,
        options,
      ),
    useRoomSubscriptionSettings: (userId, options = {}) =>
      useRoomSubscriptionSettings(useRoomFromContext(context, "useRoomSubscriptionSettings").id, userId, options),
    useGetRoomSubscriptionSettings: (userId, options = {}) =>
      useGetRoomSubscriptionSettings(useRoomFromContext(context, "useGetRoomSubscriptionSettings").id, userId, options),
    useUpdateRoomSubscriptionSettings: (userId, options = {}) =>
      useUpdateRoomSubscriptionSettings(
        useRoomFromContext(context, "useUpdateRoomSubscriptionSettings").id,
        userId,
        options,
      ),
    useSubscribeRoomThreads: (userId, options = {}) =>
      useSubscribeRoomThreads(useRoomFromContext(context, "useSubscribeRoomThreads").id, userId, options),
    useSubscribeRoomRepliesAndMentions: (userId, options = {}) =>
      useSubscribeRoomRepliesAndMentions(
        useRoomFromContext(context, "useSubscribeRoomRepliesAndMentions").id,
        userId,
        options,
      ),
    useMuteRoomThreads: (userId, options = {}) =>
      useMuteRoomThreads(useRoomFromContext(context, "useMuteRoomThreads").id, userId, options),
    useResetRoomSubscriptionSettings: (userId, options = {}) =>
      useResetRoomSubscriptionSettings(
        useRoomFromContext(context, "useResetRoomSubscriptionSettings").id,
        userId,
        options,
      ),
    useLostConnectionListener: (callback) =>
      useLostConnectionListener(useRoomFromContext(context, "useLostConnectionListener").id, callback),
    useRoomReconnect: () => useRoomReconnect(useRoomFromContext(context, "useRoomReconnect").id),
    useSetCursor: () => useSetCursor(useRoomFromContext(context, "useSetCursor").id),
    useCursor: (options = {}) => useCursor(useRoomFromContext(context, "useCursor").id, options),
    useSelfCursor: (options = {}) => useSelfCursor(useRoomFromContext(context, "useSelfCursor").id, options),
    useCursors: (options = {}) => useCursors(useRoomFromContext(context, "useCursors").id, options),
    useOtherCursors: (options = {}) => useOtherCursors(useRoomFromContext(context, "useOtherCursors").id, options),
    useCursorsMapped: (selector, options = {}) =>
      useCursorsMapped(useRoomFromContext(context, "useCursorsMapped").id, selector, options),
  };
}

function renderRoomProvider(context: Context<OpenRTCRoom | null>, props: OpenRTCRoomProviderProps): ReactNode {
  const room = useEnterRoom(props.id, compactEnterRoomOptions(props));
  return createElement(context.Provider, { value: room }, props.children);
}

function useRoomFromContext(context: Context<OpenRTCRoom | null>, hookName: string): OpenRTCRoom {
  const room = useContext(context);
  if (!room) {
    throw new Error(`${hookName} must be used inside RoomProvider`);
  }
  return room;
}

function compactEnterRoomOptions(options: EnterRoomOptions = {}): EnterRoomOptions {
  return {
    ...(options.limit !== undefined ? { limit: options.limit } : {}),
    ...(options.cursor !== undefined ? { cursor: options.cursor } : {}),
    ...(options.afterSequence !== undefined ? { afterSequence: options.afterSequence } : {}),
    ...(options.initialPresence !== undefined ? { initialPresence: options.initialPresence } : {}),
  };
}

function compactThreadListOptions(options: RoomThreadsOptions = {}): OpenRTCAdminThreadListOptions {
  return {
    ...(options.limit !== undefined ? { limit: options.limit } : {}),
    ...(options.cursor !== undefined ? { cursor: options.cursor } : {}),
    ...(options.query !== undefined ? { query: options.query } : {}),
    ...(options.userId !== undefined ? { userId: options.userId } : {}),
  };
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
  const initialPresence = useInitialPresence(room, options.initialPresence);
  const enterOptions = compactEnterRoomOptions({
    ...options,
    ...(initialPresence !== undefined ? { initialPresence } : {}),
  });

  useEffect(() => {
    const entered = client.enterRoom(room, enterOptions);
    return entered.leave;
  }, [client, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence, enterOptions.initialPresence]);

  return roomHandle;
}

export function useRoom(room: string, options: JoinOptions = {}): OpenRTCRoomState {
  const client = useOpenRTC();
  const [state, setState] = useState<OpenRTCRoomState>(() => client.getRoomState(room));
  const enterOptions = compactEnterRoomOptions(options);

  useEffect(() => {
    const entered = client.enterRoom(room, enterOptions);
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
  }, [client, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence]);

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
  const enterOptions = compactEnterRoomOptions(options);

  useEffect(() => {
    const entered = client.enterRoom(room, enterOptions);
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
  }, [client, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence]);

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
  const enterOptions = compactEnterRoomOptions(options);

  useEffect(() => {
    let active = true;
    const entered = client.enterRoom(room, enterOptions);
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
  }, [client, roomHandle, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence]);

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
  const enterOptions = compactEnterRoomOptions(options);

  useEffect(() => {
    const entered = client.enterRoom(room, enterOptions);
    void roomHandle.getStorage<TDocument>().catch(() => undefined);
    return entered.leave;
  }, [client, roomHandle, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence]);

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

export function useStorageSequence(room: string): number | undefined {
  const roomHandle = useRoomHandle(room);
  const [sequence, setSequence] = useState<number | undefined>(() => roomHandle.getStorageSequence());

  useEffect(() => {
    setSequence(roomHandle.getStorageSequence());
    return roomHandle.subscribe("storage-status", () => {
      setSequence(roomHandle.getStorageSequence());
    });
  }, [roomHandle]);

  return sequence;
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

export function useHistory(room: string): OpenRTCStorageHistory {
  return useRoomHandle(room).history;
}

export function useUndo<TDocument = unknown>(
  room: string,
): (options?: OpenRTCStorageMutationOptions) => Promise<TDocument | undefined> {
  const history = useHistory(room);
  return useCallback((options?: OpenRTCStorageMutationOptions) => history.undo<TDocument>(options), [history]);
}

export function useRedo<TDocument = unknown>(
  room: string,
): (options?: OpenRTCStorageMutationOptions) => Promise<TDocument | undefined> {
  const history = useHistory(room);
  return useCallback((options?: OpenRTCStorageMutationOptions) => history.redo<TDocument>(options), [history]);
}

export function useCanUndo(room: string): boolean {
  const roomHandle = useRoomHandle(room);
  return useSyncExternalStore(
    useCallback(
      (onStoreChange) =>
        roomHandle.subscribe("history", () => {
          onStoreChange();
        }),
      [roomHandle],
    ),
    () => roomHandle.history.canUndo(),
    () => false,
  );
}

export function useCanRedo(room: string): boolean {
  const roomHandle = useRoomHandle(room);
  return useSyncExternalStore(
    useCallback(
      (onStoreChange) =>
        roomHandle.subscribe("history", () => {
          onStoreChange();
        }),
      [roomHandle],
    ),
    () => roomHandle.history.canRedo(),
    () => false,
  );
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

export function useMutateLiveStorage<TDocument = OpenRTCLiveObject>(
  room: string,
): (mutation: OpenRTCLiveStorageMutationInput, options?: OpenRTCStorageMutationOptions) => Promise<TDocument> {
  const roomHandle = useRoomHandle(room);

  return useCallback(
    (mutation: OpenRTCLiveStorageMutationInput, options?: OpenRTCStorageMutationOptions) =>
      roomHandle.mutateLiveStorage<TDocument>(mutation, options),
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
        mutateLiveStorage: (liveMutation, options) => roomHandle.mutateLiveStorage(liveMutation, options),
      },
      ...args,
    );
  }, [roomHandle, ...deps]);
}

export function useMutation<TDocument = unknown, Args extends unknown[] = [], TResult = void>(
  room: string,
  mutation: (context: OpenRTCMutationContext<TDocument>, ...args: Args) => TResult,
  deps: DependencyList = [],
): (...args: Args) => TResult {
  const roomHandle = useRoomHandle(room);
  const mutationRef = useRef(mutation);
  mutationRef.current = mutation;

  return useCallback((...args: Args) => {
    return mutationRef.current(
      {
        room: roomHandle,
        self: roomHandle.getSelf(),
        others: roomHandle.getOthers(),
        myPresence: roomHandle.getMyPresence(),
        setMyPresence: (state) => roomHandle.setPresence(state),
        updateMyPresence: (patch) => roomHandle.updatePresence(patch),
        broadcastEvent: (event, payload, options) => roomHandle.broadcastEvent(event, payload, options),
        broadcastEventWithAck: (event, payload, options) => roomHandle.broadcastEventWithAck(event, payload, options),
        storage: roomHandle.getStorageSnapshot<TDocument>(),
        setStorage: (document, options) => roomHandle.setStorage<TDocument>(document, options),
        patchStorage: (operations, options) => roomHandle.patchStorage<TDocument>(operations, options),
        setLiveStorage: (data, options) => roomHandle.setLiveStorage(data, options),
        updateLiveStorage: (patch, options) => roomHandle.updateLiveStorage(patch, options),
        mutateLiveStorage: (liveMutation, options) => roomHandle.mutateLiveStorage(liveMutation, options),
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

export function useRoomThreadsState(room: string, options: RoomThreadsOptions = {}): RoomThreadsState {
  const client = useOpenRTC();
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = options.admin ?? contextAdmin ?? undefined;
  const initialThreads = useInitialThreads(room, options.initialThreads);
  const enterOptions = compactEnterRoomOptions(options);
  const listOptions = compactThreadListOptions(options);
  const shouldFetch = options.fetch ?? admin !== undefined;
  const [threads, setThreads] = useState<OpenRTCAdminThread[]>(() => sortCommentThreads(initialThreads ?? []));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  const refresh = useCallback(async (): Promise<OpenRTCAdminThread[]> => {
    if (!admin) {
      const next = sortCommentThreads(initialThreads ?? []);
      setThreads(next);
      return next;
    }
    setLoading(true);
    setError(undefined);
    try {
      const response = await admin.listThreads(room, listOptions);
      const next = sortCommentThreads(response.data);
      setThreads(next);
      return next;
    } catch (caught) {
      setError(caught);
      throw caught;
    } finally {
      setLoading(false);
    }
  }, [admin, room, initialThreads, listOptions.limit, listOptions.cursor, listOptions.query, listOptions.userId]);

  useEffect(() => {
    const entered = client.enterRoom(room, enterOptions);
    setThreads(sortCommentThreads(initialThreads ?? []));
    const offComments = client.on("comment", (event) => {
      if (event.room === room) {
        setThreads((current) => applyCommentEventToThreads(current, event));
      }
    });
    return () => {
      offComments();
      entered.leave();
    };
  }, [client, room, enterOptions.limit, enterOptions.cursor, enterOptions.afterSequence, initialThreads]);

  useEffect(() => {
    if (!shouldFetch || !admin) {
      return;
    }
    void refresh().catch(() => undefined);
  }, [admin, refresh, shouldFetch]);

  return useMemo(
    () => ({
      threads,
      loading,
      ...(error !== undefined ? { error } : {}),
      refresh,
    }),
    [threads, loading, error, refresh],
  );
}

export function useRoomThreads(room: string, options: RoomThreadsOptions = {}): OpenRTCAdminThread[] {
  return useRoomThreadsState(room, options).threads;
}

export function useRoomThread(
  room: string,
  threadId: string,
  options: RoomThreadsOptions = {},
): OpenRTCAdminThread | undefined {
  const threads = useRoomThreads(room, options);

  return useMemo(() => threads.find((thread) => thread.id === threadId), [threads, threadId]);
}

export function useGetThread(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useGetThread");
  return useCallback((threadId: string) => admin.getThread(room, threadId), [admin, room]);
}

export function useCreateThread(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (thread: OpenRTCAdminThreadInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useCreateThread");
  return useCallback((thread: OpenRTCAdminThreadInput) => admin.createThread(room, thread), [admin, room]);
}

export function useEditThread(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string, update: OpenRTCAdminThreadUpdate) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useEditThread");
  return useCallback(
    (threadId: string, update: OpenRTCAdminThreadUpdate) => admin.updateThread(room, threadId, update),
    [admin, room],
  );
}

export function useEditThreadMetadata(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: EditThreadMetadataInput) => Promise<OpenRTCAdminThread> {
  const editThread = useEditThread(room, options);
  return useCallback(
    (input: EditThreadMetadataInput) => editThread(input.threadId, { metadata: input.metadata }),
    [editThread],
  );
}

export function useMarkThreadResolved(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useMarkThreadResolved");
  return useCallback((threadId: string) => admin.markThreadResolved(room, threadId), [admin, room]);
}

export function useMarkThreadUnresolved(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useMarkThreadUnresolved");
  return useCallback((threadId: string) => admin.markThreadUnresolved(room, threadId), [admin, room]);
}

export function useGetThreadReadState(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThreadReadState> {
  const admin = useAdminActionClient(options, "useGetThreadReadState");
  return useCallback((threadId: string) => admin.getThreadReadState(room, threadId, userId), [admin, room, userId]);
}

export function useMarkThreadRead(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThreadReadState> {
  const admin = useAdminActionClient(options, "useMarkThreadRead");
  return useCallback((threadId: string) => admin.markThreadRead(room, threadId, userId), [admin, room, userId]);
}

export function useMarkThreadUnread(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<OpenRTCAdminThreadReadState> {
  const admin = useAdminActionClient(options, "useMarkThreadUnread");
  return useCallback((threadId: string) => admin.markThreadUnread(room, threadId, userId), [admin, room, userId]);
}

export function useDeleteThread(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string) => Promise<void> {
  const admin = useAdminActionClient(options, "useDeleteThread");
  return useCallback((threadId: string) => admin.deleteThread(room, threadId), [admin, room]);
}

export function useCreateComment(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string, comment: OpenRTCAdminCommentInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useCreateComment");
  return useCallback(
    (threadId: string, comment: OpenRTCAdminCommentInput) => admin.addComment(room, threadId, comment),
    [admin, room],
  );
}

export function useEditComment(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (threadId: string, commentId: string, update: OpenRTCAdminCommentUpdate) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useEditComment");
  return useCallback(
    (threadId: string, commentId: string, update: OpenRTCAdminCommentUpdate) =>
      admin.updateComment(room, threadId, commentId, update),
    [admin, room],
  );
}

export function useEditCommentMetadata(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: EditCommentMetadataInput) => Promise<OpenRTCAdminThread> {
  const editComment = useEditComment(room, options);
  return useCallback(
    (input: EditCommentMetadataInput) =>
      editComment(input.threadId, input.commentId, { metadata: input.metadata }),
    [editComment],
  );
}

export function useAddReaction(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: CommentReactionActionInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useAddReaction");
  return useCallback(
    async (input: CommentReactionActionInput) => {
      const currentReactions =
        input.currentReactions ?? (await loadCommentForAction(admin, room, input.threadId, input.commentId)).reactions;
      return admin.updateComment(room, input.threadId, input.commentId, {
        reactions: addCommentReaction(currentReactions, input.reaction),
      });
    },
    [admin, room],
  );
}

export function useRemoveReaction(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: CommentReactionActionInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useRemoveReaction");
  return useCallback(
    async (input: CommentReactionActionInput) => {
      const currentReactions =
        input.currentReactions ?? (await loadCommentForAction(admin, room, input.threadId, input.commentId)).reactions;
      return admin.updateComment(room, input.threadId, input.commentId, {
        reactions: removeCommentReaction(currentReactions, input.reaction),
      });
    },
    [admin, room],
  );
}

export function useAddCommentMention(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: CommentMentionActionInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useAddCommentMention");
  return useCallback(
    async (input: CommentMentionActionInput) => {
      const currentMentions =
        input.currentMentions ?? (await loadCommentForAction(admin, room, input.threadId, input.commentId)).mentions;
      return admin.updateComment(room, input.threadId, input.commentId, {
        mentions: addCommentMention(currentMentions, input.userId),
      });
    },
    [admin, room],
  );
}

export function useRemoveCommentMention(
  room: string,
  options: OpenRTCAdminActionOptions = {},
): (input: CommentMentionActionInput) => Promise<OpenRTCAdminThread> {
  const admin = useAdminActionClient(options, "useRemoveCommentMention");
  return useCallback(
    async (input: CommentMentionActionInput) => {
      const currentMentions =
        input.currentMentions ?? (await loadCommentForAction(admin, room, input.threadId, input.commentId)).mentions;
      return admin.updateComment(room, input.threadId, input.commentId, {
        mentions: removeCommentMention(currentMentions, input.userId),
      });
    },
    [admin, room],
  );
}

export function useNotificationListener(callback: (event: OpenRTCNotificationDelta) => void): void {
  const client = useOpenRTC();
  const stableCallback = useStableCallback(callback);

  useEffect(() => client.on("notification", stableCallback), [client, stableCallback]);
}

export function useInboxNotificationsState(options: InboxNotificationsOptions = {}): InboxNotificationsState {
  const client = useOpenRTC();
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = options.admin ?? contextAdmin ?? undefined;
  const initialNotifications = useInitialNotifications(options.userId, options.initialNotifications);
  const userId = options.userId;
  const unreadOnly = options.unreadOnly ?? false;
  const limit = options.limit;
  const cursor = options.cursor;
  const startingAfter = options.startingAfter;
  const shouldFetch = options.fetch ?? (admin !== undefined && userId !== undefined);
  const materializationOptions = useMemo<OpenRTCInboxMaterializationOptions>(
    () => ({
      ...(userId !== undefined ? { userId } : {}),
      ...(unreadOnly ? { unreadOnly: true } : {}),
      ...(limit !== undefined ? { limit } : {}),
    }),
    [userId, unreadOnly, limit],
  );
  const [notifications, setNotifications] = useState<OpenRTCAdminInboxNotification[]>(() =>
    sortInboxNotifications(initialNotifications ?? [], materializationOptions),
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  const refresh = useCallback(async (): Promise<OpenRTCAdminInboxNotification[]> => {
    if (!admin || userId === undefined) {
      const next = sortInboxNotifications(initialNotifications ?? [], materializationOptions);
      setNotifications(next);
      return next;
    }
    setLoading(true);
    setError(undefined);
    try {
      const response = await admin.listInboxNotifications(userId, {
        ...(typeof limit === "number" ? { limit } : {}),
        ...(cursor !== undefined ? { cursor } : {}),
        ...(startingAfter !== undefined ? { startingAfter } : {}),
        ...(unreadOnly ? { unread: true } : {}),
      });
      const next = sortInboxNotifications(response.data, materializationOptions);
      setNotifications(next);
      return next;
    } catch (caught) {
      setError(caught);
      throw caught;
    } finally {
      setLoading(false);
    }
  }, [admin, cursor, initialNotifications, limit, materializationOptions, startingAfter, unreadOnly, userId]);

  useEffect(() => {
    setNotifications(sortInboxNotifications(initialNotifications ?? [], materializationOptions));
    return client.on("notification", (delta) => {
      if (userId !== undefined && delta.userId !== userId) {
        return;
      }
      setNotifications((current) => applyNotificationDeltaToInbox(current, delta, materializationOptions));
    });
  }, [client, userId, materializationOptions, initialNotifications]);

  useEffect(() => {
    if (!shouldFetch || !admin || userId === undefined) {
      return;
    }
    void refresh().catch(() => undefined);
  }, [admin, refresh, shouldFetch, userId]);

  return useMemo(
    () => ({
      notifications,
      loading,
      ...(error !== undefined ? { error } : {}),
      refresh,
    }),
    [notifications, loading, error, refresh],
  );
}

export function useInboxNotifications(options: InboxNotificationsOptions = {}): OpenRTCAdminInboxNotification[] {
  return useInboxNotificationsState(options).notifications;
}

export function useUnreadInboxCount(
  options: Omit<InboxNotificationsOptions, "unreadOnly"> = {},
): number {
  return useInboxNotifications({ ...options, unreadOnly: true }).length;
}

export function useTriggerInboxNotification(
  options: OpenRTCAdminActionOptions = {},
): (input: OpenRTCAdminInboxNotificationInput) => Promise<OpenRTCAdminInboxNotification> {
  const admin = useAdminActionClient(options, "useTriggerInboxNotification");
  return useCallback(
    (input: OpenRTCAdminInboxNotificationInput) => admin.triggerInboxNotification(input),
    [admin],
  );
}

export function useMarkInboxNotificationAsRead(
  options: OpenRTCAdminActionOptions = {},
): (notificationId: string) => Promise<OpenRTCAdminInboxNotification> {
  const admin = useAdminActionClient(options, "useMarkInboxNotificationAsRead");
  return useCallback((notificationId: string) => admin.markInboxNotificationRead(notificationId), [admin]);
}

export function useDeleteInboxNotification(
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (notificationId: string) => Promise<void> {
  const admin = useAdminActionClient(options, "useDeleteInboxNotification");
  return useCallback((notificationId: string) => admin.deleteInboxNotification(userId, notificationId), [admin, userId]);
}

export function useDeleteAllInboxNotifications(
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<void> {
  const admin = useAdminActionClient(options, "useDeleteAllInboxNotifications");
  return useCallback(() => admin.deleteAllInboxNotifications(userId), [admin, userId]);
}

export function useRoomSubscriptionSettingsState(
  room: string,
  userId: string,
  options: RoomSubscriptionSettingsOptions = {},
): RoomSubscriptionSettingsState {
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = options.admin ?? contextAdmin ?? undefined;
  const initialSettings = useInitialRoomSubscriptionSettings(room, userId, options.initialSettings);
  const shouldFetch = options.fetch ?? admin !== undefined;
  const [settings, setSettings] = useState<OpenRTCAdminRoomSubscriptionSettings | undefined>(initialSettings);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  const refresh = useCallback(async (): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined> => {
    if (!admin) {
      setSettings(initialSettings);
      return initialSettings;
    }
    setLoading(true);
    setError(undefined);
    try {
      const next = await admin.getRoomSubscriptionSettings(room, userId);
      setSettings(next);
      return next;
    } catch (caught) {
      setError(caught);
      throw caught;
    } finally {
      setLoading(false);
    }
  }, [admin, initialSettings, room, userId]);

  const update = useCallback(
    async (
      input: OpenRTCAdminRoomSubscriptionSettingsInput,
    ): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined> => {
      if (!admin) {
        const caught = new Error("useRoomSubscriptionSettingsState requires an OpenRTCAdminProvider or an admin option");
        setError(caught);
        return undefined;
      }
      setLoading(true);
      setError(undefined);
      try {
        const next = await admin.setRoomSubscriptionSettings(room, userId, input);
        setSettings(next);
        return next;
      } catch (caught) {
        setError(caught);
        throw caught;
      } finally {
        setLoading(false);
      }
    },
    [admin, room, userId],
  );

  const reset = useCallback(async (): Promise<OpenRTCAdminRoomSubscriptionSettings | undefined> => {
    if (!admin) {
      const caught = new Error("useRoomSubscriptionSettingsState requires an OpenRTCAdminProvider or an admin option");
      setError(caught);
      return undefined;
    }
    setLoading(true);
    setError(undefined);
    try {
      await admin.deleteRoomSubscriptionSettings(room, userId);
      const next = await admin.getRoomSubscriptionSettings(room, userId);
      setSettings(next);
      return next;
    } catch (caught) {
      setError(caught);
      throw caught;
    } finally {
      setLoading(false);
    }
  }, [admin, room, userId]);

  useEffect(() => {
    setSettings(initialSettings);
  }, [initialSettings]);

  useEffect(() => {
    if (!shouldFetch || !admin) {
      return;
    }
    void refresh().catch(() => undefined);
  }, [admin, refresh, shouldFetch]);

  return useMemo(
    () => ({
      ...(settings !== undefined ? { settings } : {}),
      loading,
      ...(error !== undefined ? { error } : {}),
      refresh,
      update,
      reset,
      subscribeAll: () => update(roomSubscriptionSettingsInput("all")),
      subscribeRepliesAndMentions: () => update(roomSubscriptionSettingsInput("replies_and_mentions")),
      mute: () => update(roomSubscriptionSettingsInput("none")),
    }),
    [error, loading, refresh, reset, settings, update],
  );
}

export function useRoomSubscriptionSettings(
  room: string,
  userId: string,
  options: RoomSubscriptionSettingsOptions = {},
): OpenRTCAdminRoomSubscriptionSettings | undefined {
  return useRoomSubscriptionSettingsState(room, userId, options).settings;
}

export function useUserRoomSubscriptionSettingsState(
  userId: string,
  options: UserRoomSubscriptionSettingsOptions = {},
): UserRoomSubscriptionSettingsState {
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = options.admin ?? contextAdmin ?? undefined;
  const initialSettings = useInitialUserRoomSubscriptionSettings(userId, options.initialSettings);
  const shouldFetch = options.fetch ?? admin !== undefined;
  const limit = options.limit;
  const cursor = options.cursor;
  const [settings, setSettings] = useState<OpenRTCAdminRoomSubscriptionSettings[]>(() => [...(initialSettings ?? [])]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>();

  const refresh = useCallback(async (): Promise<OpenRTCAdminRoomSubscriptionSettings[]> => {
    if (!admin) {
      const next = [...(initialSettings ?? [])];
      setSettings(next);
      return next;
    }
    setLoading(true);
    setError(undefined);
    try {
      const response = await admin.listRoomSubscriptionSettings(userId, {
        ...(typeof limit === "number" ? { limit } : {}),
        ...(cursor !== undefined ? { cursor } : {}),
      });
      setSettings(response.data);
      return response.data;
    } catch (caught) {
      setError(caught);
      throw caught;
    } finally {
      setLoading(false);
    }
  }, [admin, cursor, initialSettings, limit, userId]);

  useEffect(() => {
    setSettings([...(initialSettings ?? [])]);
  }, [initialSettings]);

  useEffect(() => {
    if (!shouldFetch || !admin) {
      return;
    }
    void refresh().catch(() => undefined);
  }, [admin, refresh, shouldFetch]);

  return useMemo(
    () => ({
      settings,
      loading,
      ...(error !== undefined ? { error } : {}),
      refresh,
    }),
    [error, loading, refresh, settings],
  );
}

export function useUserRoomSubscriptionSettings(
  userId: string,
  options: UserRoomSubscriptionSettingsOptions = {},
): OpenRTCAdminRoomSubscriptionSettings[] {
  return useUserRoomSubscriptionSettingsState(userId, options).settings;
}

export function useGetRoomSubscriptionSettings(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<OpenRTCAdminRoomSubscriptionSettings> {
  const admin = useAdminActionClient(options, "useGetRoomSubscriptionSettings");
  return useCallback(() => admin.getRoomSubscriptionSettings(room, userId), [admin, room, userId]);
}

export function useListRoomSubscriptionSettings(
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (listOptions?: { limit?: number; cursor?: string }) => Promise<OpenRTCAdminRoomSubscriptionSettings[]> {
  const admin = useAdminActionClient(options, "useListRoomSubscriptionSettings");
  return useCallback(
    async (listOptions: { limit?: number; cursor?: string } = {}) => {
      const response = await admin.listRoomSubscriptionSettings(userId, listOptions);
      return response.data;
    },
    [admin, userId],
  );
}

export function useUpdateRoomSubscriptionSettings(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): (settings: OpenRTCAdminRoomSubscriptionSettingsInput) => Promise<OpenRTCAdminRoomSubscriptionSettings> {
  const admin = useAdminActionClient(options, "useUpdateRoomSubscriptionSettings");
  return useCallback(
    (settings: OpenRTCAdminRoomSubscriptionSettingsInput) =>
      admin.setRoomSubscriptionSettings(room, userId, settings),
    [admin, room, userId],
  );
}

export function useSubscribeRoomThreads(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<OpenRTCAdminRoomSubscriptionSettings> {
  const admin = useAdminActionClient(options, "useSubscribeRoomThreads");
  return useCallback(() => admin.subscribeRoomThreads(room, userId), [admin, room, userId]);
}

export function useSubscribeRoomRepliesAndMentions(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<OpenRTCAdminRoomSubscriptionSettings> {
  const admin = useAdminActionClient(options, "useSubscribeRoomRepliesAndMentions");
  return useCallback(() => admin.subscribeRoomRepliesAndMentions(room, userId), [admin, room, userId]);
}

export function useMuteRoomThreads(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<OpenRTCAdminRoomSubscriptionSettings> {
  const admin = useAdminActionClient(options, "useMuteRoomThreads");
  return useCallback(() => admin.muteRoomThreads(room, userId), [admin, room, userId]);
}

export function useResetRoomSubscriptionSettings(
  room: string,
  userId: string,
  options: OpenRTCAdminActionOptions = {},
): () => Promise<void> {
  const admin = useAdminActionClient(options, "useResetRoomSubscriptionSettings");
  return useCallback(() => admin.deleteRoomSubscriptionSettings(room, userId), [admin, room, userId]);
}

export function RoomSubscriptionControls(props: RoomSubscriptionControlsProps): ReactNode {
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = props.admin ?? contextAdmin ?? undefined;
  const state = useRoomSubscriptionSettingsState(props.room, props.userId, props);
  const current = state.settings?.threads ?? "all";
  const disabled = props.disabled || state.loading || !admin;
  const setMode = useCallback(
    async (action: () => Promise<OpenRTCAdminRoomSubscriptionSettings | undefined>) => {
      const next = await action();
      if (next) {
        props.onSettingsChanged?.(next);
      }
    },
    [props],
  );

  return createElement(
    "section",
    {
      className: props.className,
      "aria-label": typeof props.title === "string" ? props.title : "Room subscription settings",
      style: {
        ...roomSubscriptionStyles.root,
        ...props.style,
      } satisfies CSSProperties,
    },
    createElement(
      "div",
      { style: roomSubscriptionStyles.header },
      createElement("div", { style: roomSubscriptionStyles.title }, props.title ?? "Thread notifications"),
      createElement(
        "span",
        { style: roomSubscriptionStyles.status },
        state.loading ? "Updating" : roomSubscriptionStatusText(state.settings),
      ),
    ),
    createElement(
      "div",
      { role: "group", style: roomSubscriptionStyles.segment },
      roomSubscriptionOptionButton({
        label: "All",
        active: current === "all",
        disabled,
        onClick: () => void setMode(state.subscribeAll),
      }),
      roomSubscriptionOptionButton({
        label: "Replies",
        active: current === "replies_and_mentions",
        disabled,
        onClick: () => void setMode(state.subscribeRepliesAndMentions),
      }),
      roomSubscriptionOptionButton({
        label: "Muted",
        active: current === "none",
        disabled,
        onClick: () => void setMode(state.mute),
      }),
    ),
    createElement(
      "div",
      { style: roomSubscriptionStyles.footer },
      createElement(
        "span",
        { style: roomSubscriptionStyles.hint },
        admin ? roomSubscriptionHintText(state.settings) : "Admin client unavailable",
      ),
      props.showReset === false
        ? null
        : createElement(
            "button",
            {
              type: "button",
              disabled,
              onClick: () => void setMode(state.reset),
              style: roomSubscriptionResetStyle(disabled),
            },
            "Reset",
          ),
    ),
    state.error ? createElement("div", { role: "alert", style: roomSubscriptionStyles.error }, roomSubscriptionErrorText(state.error)) : null,
  );
}

function roomSubscriptionOptionButton(props: {
  label: string;
  active: boolean;
  disabled: boolean;
  onClick(): void;
}): ReactNode {
  return createElement(
    "button",
    {
      type: "button",
      "aria-pressed": props.active,
      disabled: props.disabled,
      onClick: props.onClick,
      style: {
        ...roomSubscriptionStyles.segmentButton,
        ...(props.active ? roomSubscriptionStyles.segmentButtonActive : {}),
        opacity: props.disabled ? 0.55 : 1,
        cursor: props.disabled ? "not-allowed" : "pointer",
      } satisfies CSSProperties,
    },
    props.label,
  );
}

function roomSubscriptionStatusText(settings: OpenRTCAdminRoomSubscriptionSettings | undefined): string {
  switch (settings?.threads ?? "all") {
    case "all":
      return "All activity";
    case "replies_and_mentions":
      return "Replies and mentions";
    case "none":
      return "Muted";
  }
}

function roomSubscriptionHintText(settings: OpenRTCAdminRoomSubscriptionSettings | undefined): string {
  switch (settings?.threads ?? "all") {
    case "all":
      return "Notify for new threads, replies, and mentions";
    case "replies_and_mentions":
      return "Notify for replies and direct mentions";
    case "none":
      return "Thread notifications are muted";
  }
}

function roomSubscriptionErrorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  return "Subscription update failed";
}

function roomSubscriptionResetStyle(disabled: boolean): CSSProperties {
  return {
    appearance: "none",
    border: "1px solid #cbd5e1",
    borderRadius: 6,
    background: "#ffffff",
    color: "#334155",
    cursor: disabled ? "not-allowed" : "pointer",
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    fontSize: 12,
    fontWeight: 700,
    lineHeight: 1,
    minHeight: 28,
    opacity: disabled ? 0.55 : 1,
    padding: "7px 9px",
  };
}

const roomSubscriptionStyles = {
  root: {
    width: "100%",
    boxSizing: "border-box",
    border: "1px solid #d7dde8",
    borderRadius: 8,
    background: "#ffffff",
    color: "#111827",
    display: "flex",
    flexDirection: "column",
    gap: 10,
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    padding: 12,
  } satisfies CSSProperties,
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  } satisfies CSSProperties,
  title: {
    color: "#111827",
    fontSize: 14,
    fontWeight: 800,
    lineHeight: 1.2,
  } satisfies CSSProperties,
  status: {
    color: "#64748b",
    fontSize: 12,
    fontWeight: 700,
    lineHeight: 1.2,
    textAlign: "right",
  } satisfies CSSProperties,
  segment: {
    display: "grid",
    gridTemplateColumns: "repeat(3, minmax(0, 1fr))",
    border: "1px solid #cbd5e1",
    borderRadius: 8,
    overflow: "hidden",
  } satisfies CSSProperties,
  segmentButton: {
    appearance: "none",
    border: 0,
    borderRight: "1px solid #cbd5e1",
    background: "#f8fafc",
    color: "#334155",
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    fontSize: 12,
    fontWeight: 800,
    lineHeight: 1,
    minHeight: 34,
    padding: "10px 8px",
  } satisfies CSSProperties,
  segmentButtonActive: {
    background: "#111827",
    color: "#ffffff",
  } satisfies CSSProperties,
  footer: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  } satisfies CSSProperties,
  hint: {
    color: "#64748b",
    fontSize: 12,
    lineHeight: 1.35,
  } satisfies CSSProperties,
  error: {
    border: "1px solid #fecaca",
    borderRadius: 6,
    background: "#fef2f2",
    color: "#991b1b",
    fontSize: 12,
    lineHeight: 1.35,
    padding: "7px 9px",
  } satisfies CSSProperties,
} as const;

export function CommentsPanel(props: CommentsPanelProps): ReactNode {
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = props.admin ?? contextAdmin ?? undefined;
  const showComposer = props.showComposer ?? true;
  const showResolved = props.showResolved ?? true;
  const showReadActions = props.showReadActions ?? true;
  const showResolveActions = props.showResolveActions ?? true;
  const threadsState = useRoomThreadsState(props.room, {
    ...(admin !== undefined ? { admin } : {}),
    ...(props.initialThreads !== undefined ? { initialThreads: props.initialThreads } : {}),
    ...(props.query !== undefined ? { query: props.query } : {}),
    ...(props.limit !== undefined ? { limit: props.limit } : {}),
    ...(props.cursor !== undefined ? { cursor: props.cursor } : {}),
    ...(props.afterSequence !== undefined ? { afterSequence: props.afterSequence } : {}),
    userId: props.userId,
    fetch: props.fetch ?? admin !== undefined,
  });
  const [threadDraft, setThreadDraft] = useState("");
  const [replyDrafts, setReplyDrafts] = useState<Record<string, string>>({});
  const [pendingAction, setPendingAction] = useState<string | undefined>();
  const [actionError, setActionError] = useState<unknown>();
  const threads = useMemo(
    () => (showResolved ? threadsState.threads : threadsState.threads.filter((thread) => !thread.resolved)),
    [showResolved, threadsState.threads],
  );
  const openCount = useMemo(() => threadsState.threads.filter((thread) => !thread.resolved).length, [threadsState.threads]);
  const unreadCount = useMemo(() => threadsState.threads.filter((thread) => thread.unread).length, [threadsState.threads]);
  const disabled = !admin || pendingAction !== undefined;

  const runAction = useCallback(
    async <T,>(key: string, action: (adminClient: OpenRTCAdminClient) => Promise<T>): Promise<T | undefined> => {
      if (!admin) {
        setActionError(new Error("CommentsPanel requires an OpenRTCAdminProvider or an admin prop"));
        return undefined;
      }
      setPendingAction(key);
      setActionError(undefined);
      try {
        const result = await action(admin);
        await threadsState.refresh();
        return result;
      } catch (caught) {
        setActionError(caught);
        return undefined;
      } finally {
        setPendingAction(undefined);
      }
    },
    [admin, threadsState],
  );

  const createThread = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      const text = threadDraft.trim();
      if (!text) {
        return;
      }
      const created = await runAction("thread:create", (adminClient) =>
        adminClient.createThread(props.room, {
          ...commentsPanelMetadataField(props.threadMetadata, { kind: "thread", text }),
          comment: {
            userId: props.userId,
            body: commentsPanelBodyFromText(props, text, { kind: "thread" }),
            ...commentsPanelMetadataField(props.commentMetadata, { kind: "thread", text }),
          },
        }),
      );
      if (created) {
        setThreadDraft("");
        props.onThreadCreated?.(created);
      }
    },
    [props, runAction, threadDraft],
  );

  const createReply = useCallback(
    async (thread: OpenRTCAdminThread, event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      const text = (replyDrafts[thread.id] ?? "").trim();
      if (!text) {
        return;
      }
      const updated = await runAction(`comment:create:${thread.id}`, (adminClient) =>
        adminClient.addComment(props.room, thread.id, {
          userId: props.userId,
          body: commentsPanelBodyFromText(props, text, { kind: "comment", thread }),
          ...commentsPanelMetadataField(props.commentMetadata, { kind: "comment", thread, text }),
        }),
      );
      if (updated) {
        setReplyDrafts((current) => ({ ...current, [thread.id]: "" }));
        props.onCommentCreated?.(updated);
      }
    },
    [props, replyDrafts, runAction],
  );

  const updateThread = useCallback(
    async (thread: OpenRTCAdminThread, action: "read" | "unread" | "resolve" | "unresolve") => {
      const updated = await runAction(`thread:${action}:${thread.id}`, async (adminClient) => {
        switch (action) {
          case "read":
            await adminClient.markThreadRead(props.room, thread.id, props.userId);
            return adminClient.getThread(props.room, thread.id);
          case "unread":
            await adminClient.markThreadUnread(props.room, thread.id, props.userId);
            return adminClient.getThread(props.room, thread.id);
          case "resolve":
            return adminClient.markThreadResolved(props.room, thread.id);
          case "unresolve":
            return adminClient.markThreadUnresolved(props.room, thread.id);
        }
      });
      if (updated) {
        props.onThreadUpdated?.(updated);
      }
    },
    [props, runAction],
  );

  return createElement(
    "section",
    {
      className: props.className,
      "aria-label": typeof props.title === "string" ? props.title : "Comments",
      style: {
        ...commentsPanelStyles.root,
        ...props.style,
      } satisfies CSSProperties,
    },
    createElement(
      "header",
      { style: commentsPanelStyles.header },
      createElement(
        "div",
        { style: commentsPanelStyles.headingGroup },
        createElement("div", { style: commentsPanelStyles.title }, props.title ?? "Comments"),
        createElement(
          "div",
          { style: commentsPanelStyles.metrics },
          createElement("span", { style: commentsPanelStyles.metric }, `${threadsState.threads.length} total`),
          createElement("span", { style: commentsPanelStyles.metric }, `${openCount} open`),
          createElement("span", { style: commentsPanelStyles.metric }, `${unreadCount} unread`),
        ),
      ),
      createElement(
        "button",
        {
          type: "button",
          onClick: () => void threadsState.refresh().catch((caught) => setActionError(caught)),
          disabled: threadsState.loading,
          style: commentsPanelButtonStyle("ghost", threadsState.loading),
        },
        threadsState.loading ? "Refreshing" : "Refresh",
      ),
    ),
    showComposer
      ? createElement(
          "form",
          { onSubmit: createThread, style: commentsPanelStyles.composer },
          createElement("textarea", {
            "aria-label": "New thread",
            placeholder: props.composerPlaceholder ?? "Start a thread",
            value: threadDraft,
            onChange: (event: ChangeEvent<HTMLTextAreaElement>) => setThreadDraft(event.currentTarget.value),
            rows: 3,
            style: commentsPanelStyles.textarea,
          }),
          createElement(
            "div",
            { style: commentsPanelStyles.composerFooter },
            createElement("span", { style: commentsPanelStyles.hint }, admin ? "Visible to room collaborators" : "Admin client unavailable"),
            createElement(
              "button",
              {
                type: "submit",
                disabled: disabled || threadDraft.trim() === "",
                style: commentsPanelButtonStyle("primary", disabled || threadDraft.trim() === ""),
              },
              pendingAction === "thread:create" ? "Posting" : "Post",
            ),
          ),
        )
      : null,
    commentsPanelError(actionError ?? threadsState.error),
    threads.length === 0
      ? createElement("div", { style: commentsPanelStyles.empty }, props.emptyState ?? "No comments yet")
      : createElement(
          "div",
          { style: commentsPanelStyles.threadList },
          threads.map((thread) =>
            commentsPanelThread(props, {
              thread,
              replyText: replyDrafts[thread.id] ?? "",
              pending: pendingAction?.endsWith(`:${thread.id}`) ?? false,
              disabled,
              showReadActions,
              showResolveActions,
              onReplyTextChange: (value) => setReplyDrafts((current) => ({ ...current, [thread.id]: value })),
              onCreateReply: (event) => void createReply(thread, event),
              onMarkRead: () => updateThread(thread, "read"),
              onMarkUnread: () => updateThread(thread, "unread"),
              onResolve: () => updateThread(thread, "resolve"),
              onUnresolve: () => updateThread(thread, "unresolve"),
            }),
          ),
        ),
  );
}

interface CommentsPanelThreadRenderState {
  thread: OpenRTCAdminThread;
  replyText: string;
  pending: boolean;
  disabled: boolean;
  showReadActions: boolean;
  showResolveActions: boolean;
  onReplyTextChange(value: string): void;
  onCreateReply(event: FormEvent<HTMLFormElement>): void;
  onMarkRead(): Promise<void>;
  onMarkUnread(): Promise<void>;
  onResolve(): Promise<void>;
  onUnresolve(): Promise<void>;
}

function commentsPanelThread(props: CommentsPanelProps, state: CommentsPanelThreadRenderState): ReactNode {
  const thread = state.thread;
  const actionControls = props.renderThreadActions
    ? props.renderThreadActions({
        thread,
        pending: state.pending,
        markRead: state.onMarkRead,
        markUnread: state.onMarkUnread,
        resolve: state.onResolve,
        unresolve: state.onUnresolve,
      })
    : commentsPanelDefaultThreadActions(state);

  return createElement(
    "article",
    {
      key: thread.id,
      "data-openrtc-thread": thread.id,
      "data-openrtc-thread-state": thread.resolved ? "resolved" : "open",
      style: {
        ...commentsPanelStyles.thread,
        borderColor: thread.unread ? "#2563eb" : "#d7dde8",
        boxShadow: thread.unread ? "0 0 0 1px rgba(37,99,235,0.16)" : "none",
      } satisfies CSSProperties,
    },
    createElement(
      "div",
      { style: commentsPanelStyles.threadHeader },
      createElement(
        "div",
        { style: commentsPanelStyles.threadMeta },
        createElement(
          "div",
          { style: commentsPanelStyles.threadTitle },
          thread.resolved ? "Resolved thread" : "Open thread",
        ),
        createElement(
          "div",
          { style: commentsPanelStyles.threadSubtle },
          `Updated ${commentsPanelTimestamp(thread.updatedAt)}`,
        ),
      ),
      createElement(
        "div",
        { style: commentsPanelStyles.statusGroup },
        thread.unread
          ? createElement("span", { style: commentsPanelStatusStyle("unread") }, "Unread")
          : createElement("span", { style: commentsPanelStatusStyle("read") }, "Read"),
        thread.resolved
          ? createElement("span", { style: commentsPanelStatusStyle("resolved") }, "Resolved")
          : createElement("span", { style: commentsPanelStatusStyle("open") }, "Open"),
      ),
    ),
    createElement(
      "ol",
      { style: commentsPanelStyles.comments },
      thread.comments.map((comment, index) => commentsPanelComment(props, thread, comment, index)),
    ),
    createElement(
      "div",
      { style: commentsPanelStyles.threadActions },
      actionControls,
    ),
    props.showComposer === false
      ? null
      : createElement(
          "form",
          { onSubmit: state.onCreateReply, style: commentsPanelStyles.replyComposer },
          createElement("textarea", {
            "aria-label": `Reply to thread ${thread.id}`,
            placeholder: props.replyPlaceholder ?? "Reply",
            value: state.replyText,
            onChange: (event: ChangeEvent<HTMLTextAreaElement>) => state.onReplyTextChange(event.currentTarget.value),
            rows: 2,
            style: commentsPanelStyles.textarea,
          }),
          createElement(
            "div",
            { style: commentsPanelStyles.composerFooter },
            createElement("span", { style: commentsPanelStyles.hint }, `${thread.comments.length} comment${thread.comments.length === 1 ? "" : "s"}`),
            createElement(
              "button",
              {
                type: "submit",
                disabled: state.disabled || state.replyText.trim() === "",
                style: commentsPanelButtonStyle("secondary", state.disabled || state.replyText.trim() === ""),
              },
              state.pending ? "Posting" : "Reply",
            ),
          ),
        ),
  );
}

function commentsPanelComment(
  props: CommentsPanelProps,
  thread: OpenRTCAdminThread,
  comment: OpenRTCAdminComment,
  index: number,
): ReactNode {
  const context: CommentsPanelBodyContext = { kind: index === 0 ? "thread" : "comment", thread };
  const text = commentsPanelTextFromBody(props, comment.body, context);
  const rendered = props.renderComment?.({ thread, comment, index, text });
  const defaultComment = createElement(
    "div",
    { style: commentsPanelStyles.commentInner },
    createElement(
      "div",
      { style: commentsPanelStyles.commentHeader },
      createElement("span", { style: commentsPanelStyles.commentAuthor }, comment.userId),
      createElement("span", { style: commentsPanelStyles.threadSubtle }, commentsPanelTimestamp(comment.createdAt)),
      comment.editedAt ? createElement("span", { style: commentsPanelStyles.threadSubtle }, "Edited") : null,
    ),
    createElement("div", { style: commentsPanelStyles.commentBody }, text),
    commentsPanelCommentFooter(comment),
  );

  return createElement(
    "li",
    { key: comment.id, style: commentsPanelStyles.comment },
    rendered !== undefined ? rendered : defaultComment,
  );
}

function commentsPanelCommentFooter(comment: OpenRTCAdminComment): ReactNode {
  const reactions = comment.reactions ?? [];
  const mentions = comment.mentions ?? [];
  if (reactions.length === 0 && mentions.length === 0 && !comment.deletedAt) {
    return null;
  }

  return createElement(
    "div",
    { style: commentsPanelStyles.commentFooter },
    comment.deletedAt ? createElement("span", { style: commentsPanelStyles.metaPill }, "Deleted") : null,
    mentions.map((mention) =>
      createElement("span", { key: `mention:${mention}`, style: commentsPanelStyles.metaPill }, `@${mention}`),
    ),
    reactions.map((reaction, index) =>
      createElement(
        "span",
        { key: `reaction:${reaction.emoji}:${reaction.userId}:${index}`, style: commentsPanelStyles.metaPill },
        `${reaction.emoji} ${reaction.userId}`,
      ),
    ),
  );
}

function commentsPanelDefaultThreadActions(state: CommentsPanelThreadRenderState): ReactNode {
  return createElement(
    "div",
    { style: commentsPanelStyles.actionGroup },
    state.showReadActions
      ? createElement(
          "button",
          {
            type: "button",
            disabled: state.disabled || state.pending,
            onClick: () => void (state.thread.unread ? state.onMarkRead() : state.onMarkUnread()),
            style: commentsPanelButtonStyle("ghost", state.disabled || state.pending),
          },
          state.thread.unread ? "Mark read" : "Mark unread",
        )
      : null,
    state.showResolveActions
      ? createElement(
          "button",
          {
            type: "button",
            disabled: state.disabled || state.pending,
            onClick: () => void (state.thread.resolved ? state.onUnresolve() : state.onResolve()),
            style: commentsPanelButtonStyle("ghost", state.disabled || state.pending),
          },
          state.thread.resolved ? "Reopen" : "Resolve",
        )
      : null,
  );
}

function commentsPanelBodyFromText(
  props: CommentsPanelProps,
  text: string,
  context: CommentsPanelBodyContext,
): unknown {
  return props.bodyFromText ? props.bodyFromText(text, context) : { type: "text", text };
}

function commentsPanelTextFromBody(
  props: CommentsPanelProps,
  body: unknown,
  context: CommentsPanelBodyContext,
): ReactNode {
  if (props.textFromBody) {
    return props.textFromBody(body, context);
  }
  const text = commentsPanelBodyText(body);
  return text === "" ? createElement("span", { style: commentsPanelStyles.mutedText }, "No text") : text;
}

function commentsPanelBodyText(body: unknown, depth = 0): string {
  if (depth > 4 || body === null || body === undefined) {
    return "";
  }
  if (typeof body === "string") {
    return body;
  }
  if (typeof body === "number" || typeof body === "boolean" || typeof body === "bigint") {
    return String(body);
  }
  if (Array.isArray(body)) {
    return body.map((item) => commentsPanelBodyText(item, depth + 1)).filter(Boolean).join(" ");
  }
  if (commentsPanelIsRecord(body)) {
    const directText = body["text"] ?? body["plainText"] ?? body["value"];
    if (typeof directText === "string") {
      return directText;
    }
    const content = body["content"] ?? body["children"];
    if (Array.isArray(content)) {
      const text = content.map((item) => commentsPanelBodyText(item, depth + 1)).filter(Boolean).join(" ");
      if (text !== "") {
        return text;
      }
    }
    if (depth === 0) {
      try {
        return JSON.stringify(body);
      } catch {
        return "";
      }
    }
  }
  return "";
}

function commentsPanelMetadataField(
  input: CommentsPanelMetadata | undefined,
  context: CommentsPanelMetadataContext,
): { metadata?: unknown } {
  if (input === undefined) {
    return {};
  }
  const metadata = typeof input === "function" ? input(context) : input;
  return metadata === undefined ? {} : { metadata };
}

function commentsPanelError(error: unknown): ReactNode {
  if (error === undefined || error === null) {
    return null;
  }
  return createElement(
    "div",
    { role: "alert", style: commentsPanelStyles.error },
    commentsPanelErrorMessage(error),
  );
}

function commentsPanelErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  try {
    return JSON.stringify(error);
  } catch {
    return "Comment action failed";
  }
}

function commentsPanelTimestamp(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(date);
}

function commentsPanelIsRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function commentsPanelButtonStyle(
  variant: "primary" | "secondary" | "ghost",
  disabled: boolean,
): CSSProperties {
  const primary = variant === "primary";
  const secondary = variant === "secondary";
  return {
    appearance: "none",
    border: primary ? "1px solid #111827" : "1px solid #cbd5e1",
    borderRadius: 6,
    background: primary ? "#111827" : secondary ? "#ffffff" : "#f8fafc",
    color: primary ? "#ffffff" : "#1f2937",
    cursor: disabled ? "not-allowed" : "pointer",
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    fontSize: 13,
    fontWeight: 700,
    lineHeight: 1,
    minHeight: 32,
    padding: "8px 10px",
    opacity: disabled ? 0.55 : 1,
  };
}

function commentsPanelStatusStyle(kind: "open" | "resolved" | "read" | "unread"): CSSProperties {
  const palette = {
    open: { background: "#ecfdf5", border: "#a7f3d0", color: "#065f46" },
    resolved: { background: "#f1f5f9", border: "#cbd5e1", color: "#475569" },
    read: { background: "#f8fafc", border: "#d7dde8", color: "#64748b" },
    unread: { background: "#eff6ff", border: "#bfdbfe", color: "#1d4ed8" },
  }[kind];
  return {
    display: "inline-flex",
    alignItems: "center",
    minHeight: 22,
    border: `1px solid ${palette.border}`,
    borderRadius: 999,
    background: palette.background,
    color: palette.color,
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    fontSize: 12,
    fontWeight: 700,
    lineHeight: 1,
    padding: "4px 8px",
  };
}

const commentsPanelStyles = {
  root: {
    width: "100%",
    boxSizing: "border-box",
    border: "1px solid #d7dde8",
    borderRadius: 8,
    background: "#ffffff",
    color: "#111827",
    display: "flex",
    flexDirection: "column",
    gap: 14,
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    padding: 16,
  } satisfies CSSProperties,
  header: {
    display: "flex",
    alignItems: "flex-start",
    justifyContent: "space-between",
    gap: 12,
  } satisfies CSSProperties,
  headingGroup: {
    minWidth: 0,
    display: "flex",
    flexDirection: "column",
    gap: 8,
  } satisfies CSSProperties,
  title: {
    color: "#111827",
    fontSize: 16,
    fontWeight: 800,
    lineHeight: 1.2,
  } satisfies CSSProperties,
  metrics: {
    display: "flex",
    flexWrap: "wrap",
    gap: 6,
  } satisfies CSSProperties,
  metric: {
    border: "1px solid #e2e8f0",
    borderRadius: 999,
    color: "#64748b",
    fontSize: 12,
    fontWeight: 700,
    lineHeight: 1,
    padding: "4px 8px",
  } satisfies CSSProperties,
  composer: {
    display: "flex",
    flexDirection: "column",
    gap: 8,
  } satisfies CSSProperties,
  replyComposer: {
    display: "flex",
    flexDirection: "column",
    gap: 8,
    paddingTop: 2,
  } satisfies CSSProperties,
  textarea: {
    width: "100%",
    boxSizing: "border-box",
    border: "1px solid #cbd5e1",
    borderRadius: 6,
    color: "#111827",
    fontFamily: "Geist, ui-sans-serif, system-ui, sans-serif",
    fontSize: 14,
    lineHeight: 1.45,
    minHeight: 72,
    outline: "none",
    padding: "10px 11px",
    resize: "vertical",
  } satisfies CSSProperties,
  composerFooter: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  } satisfies CSSProperties,
  hint: {
    color: "#64748b",
    fontSize: 12,
    lineHeight: 1.3,
  } satisfies CSSProperties,
  error: {
    border: "1px solid #fecaca",
    borderRadius: 6,
    background: "#fef2f2",
    color: "#991b1b",
    fontSize: 13,
    lineHeight: 1.4,
    padding: "9px 10px",
  } satisfies CSSProperties,
  empty: {
    border: "1px dashed #cbd5e1",
    borderRadius: 8,
    color: "#64748b",
    fontSize: 14,
    lineHeight: 1.4,
    padding: 18,
    textAlign: "center",
  } satisfies CSSProperties,
  threadList: {
    display: "flex",
    flexDirection: "column",
    gap: 12,
  } satisfies CSSProperties,
  thread: {
    border: "1px solid #d7dde8",
    borderRadius: 8,
    display: "flex",
    flexDirection: "column",
    gap: 12,
    padding: 12,
  } satisfies CSSProperties,
  threadHeader: {
    display: "flex",
    alignItems: "flex-start",
    justifyContent: "space-between",
    gap: 12,
  } satisfies CSSProperties,
  threadMeta: {
    minWidth: 0,
    display: "flex",
    flexDirection: "column",
    gap: 3,
  } satisfies CSSProperties,
  threadTitle: {
    color: "#111827",
    fontSize: 14,
    fontWeight: 800,
    lineHeight: 1.25,
  } satisfies CSSProperties,
  threadSubtle: {
    color: "#64748b",
    fontSize: 12,
    lineHeight: 1.35,
  } satisfies CSSProperties,
  statusGroup: {
    display: "flex",
    flexWrap: "wrap",
    justifyContent: "flex-end",
    gap: 6,
  } satisfies CSSProperties,
  comments: {
    display: "flex",
    flexDirection: "column",
    gap: 8,
    listStyle: "none",
    margin: 0,
    padding: 0,
  } satisfies CSSProperties,
  comment: {
    margin: 0,
  } satisfies CSSProperties,
  commentInner: {
    borderLeft: "2px solid #e2e8f0",
    display: "flex",
    flexDirection: "column",
    gap: 5,
    paddingLeft: 10,
  } satisfies CSSProperties,
  commentHeader: {
    display: "flex",
    alignItems: "center",
    flexWrap: "wrap",
    gap: 7,
  } satisfies CSSProperties,
  commentAuthor: {
    color: "#111827",
    fontSize: 13,
    fontWeight: 800,
    lineHeight: 1.2,
  } satisfies CSSProperties,
  commentBody: {
    color: "#1f2937",
    fontSize: 14,
    lineHeight: 1.45,
    overflowWrap: "anywhere",
    whiteSpace: "pre-wrap",
  } satisfies CSSProperties,
  commentFooter: {
    display: "flex",
    flexWrap: "wrap",
    gap: 5,
    paddingTop: 2,
  } satisfies CSSProperties,
  metaPill: {
    border: "1px solid #e2e8f0",
    borderRadius: 999,
    color: "#64748b",
    fontSize: 12,
    fontWeight: 700,
    lineHeight: 1,
    padding: "3px 7px",
  } satisfies CSSProperties,
  threadActions: {
    display: "flex",
    justifyContent: "flex-end",
  } satisfies CSSProperties,
  actionGroup: {
    display: "flex",
    flexWrap: "wrap",
    gap: 8,
  } satisfies CSSProperties,
  mutedText: {
    color: "#94a3b8",
    fontStyle: "italic",
  } satisfies CSSProperties,
} as const;

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

function useInitialThreads(
  room: string,
  initialThreads: readonly OpenRTCAdminThread[] | undefined,
): readonly OpenRTCAdminThread[] | undefined {
  const ref = useRef<{ room: string; threads: readonly OpenRTCAdminThread[] | undefined } | undefined>(undefined);
  if (!ref.current || ref.current.room !== room) {
    ref.current = { room, threads: initialThreads };
  }
  return ref.current.threads;
}

function useInitialNotifications(
  userId: string | undefined,
  initialNotifications: readonly OpenRTCAdminInboxNotification[] | undefined,
): readonly OpenRTCAdminInboxNotification[] | undefined {
  const key = userId ?? "";
  const ref =
    useRef<{ key: string; notifications: readonly OpenRTCAdminInboxNotification[] | undefined } | undefined>(undefined);
  if (!ref.current || ref.current.key !== key) {
    ref.current = { key, notifications: initialNotifications };
  }
  return ref.current.notifications;
}

function useInitialRoomSubscriptionSettings(
  room: string,
  userId: string,
  initialSettings: OpenRTCAdminRoomSubscriptionSettings | undefined,
): OpenRTCAdminRoomSubscriptionSettings | undefined {
  const key = `${room}\n${userId}`;
  const ref =
    useRef<{ key: string; settings: OpenRTCAdminRoomSubscriptionSettings | undefined } | undefined>(undefined);
  if (!ref.current || ref.current.key !== key) {
    ref.current = { key, settings: initialSettings };
  }
  return ref.current.settings;
}

function useInitialUserRoomSubscriptionSettings(
  userId: string,
  initialSettings: readonly OpenRTCAdminRoomSubscriptionSettings[] | undefined,
): readonly OpenRTCAdminRoomSubscriptionSettings[] | undefined {
  const ref =
    useRef<{ userId: string; settings: readonly OpenRTCAdminRoomSubscriptionSettings[] | undefined } | undefined>(
      undefined,
    );
  if (!ref.current || ref.current.userId !== userId) {
    ref.current = { userId, settings: initialSettings };
  }
  return ref.current.settings;
}

function useAdminActionClient(options: OpenRTCAdminActionOptions, hookName: string): OpenRTCAdminClient {
  const contextAdmin = useContext(OpenRTCAdminContext);
  const admin = options.admin ?? contextAdmin;
  if (!admin) {
    throw new Error(`${hookName} requires an OpenRTCAdminProvider or an admin option`);
  }
  return admin;
}

async function loadCommentForAction(
  admin: OpenRTCAdminClient,
  room: string,
  threadId: string,
  commentId: string,
): Promise<{
  mentions?: string[];
  reactions?: OpenRTCAdminCommentReaction[];
}> {
  const response = await admin.listThreads(room);
  const thread = response.data.find((candidate) => candidate.id === threadId);
  const comment = thread?.comments.find((candidate) => candidate.id === commentId);
  if (!comment) {
    throw new Error(`Comment ${commentId} was not found in thread ${threadId}`);
  }
  return comment;
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
