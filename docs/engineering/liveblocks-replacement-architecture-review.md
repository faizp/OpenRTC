# Liveblocks Replacement Architecture Review

Date: 2026-05-23

## Source-backed baseline

External documentation was refreshed on 2026-05-23 against current primary
sources.

- Liveblocks positions the product around collaborative editing, comments, notifications, presence, rooms, permissions, storage, REST APIs, and managed Yjs/text-editor sync.
- Liveblocks Yjs permanently stores Yjs room data and recommends editor-specific plugins for Tiptap, BlockNote, and Lexical when using those editors.
- Liveblocks text-editor products integrate persisted editor documents with comments, mentions, notifications, server-side editing, multiplayer undo/redo, and version history.
- Liveblocks stores comments, realtime text/storage/Yjs data, notifications, room metadata/accesses, and webhook delivery data as distinct durable product surfaces.
- Liveblocks REST APIs include room CRUD, active users, ephemeral presence, broadcast events, storage read/init/delete/JSON Patch, Yjs document mutation, and notification/subscription settings.
- Yjs is a CRDT layer that automatically merges concurrent updates and has editor bindings plus persistence providers. Presence/awareness should remain ephemeral and separate from persisted document state.
- Redis Pub/Sub has at-most-once delivery semantics. It is appropriate for best-effort live fan-out, not as the source of truth for durable document recovery. Redis Streams or a separate durable log are the better Redis-native fit when replay, acknowledgements, or consumer cursors are required.
- Cloudflare Durable Objects are a useful reference architecture for room-affine coordination and long-lived WebSocket workloads, especially when paired with WebSocket hibernation at the edge.
- OWASP recommends explicit WebSocket Origin allowlists, message-level authorization, input validation, rate limiting, and security logging.

References:
- https://liveblocks.io/docs/ready-made-features/multiplayer-editing/sync-engine/liveblocks-yjs
- https://liveblocks.io/docs/collaboration-features/multiplayer/text-editor
- https://liveblocks.io/docs/ready-made-features/text-editor/lexical
- https://liveblocks.io/docs/platform/sync-datastore/liveblocks-storage
- https://liveblocks.io/docs/platform/data-storage
- https://liveblocks.io/docs/collaboration-features/notifications
- https://liveblocks.io/docs/api-reference/rest-api-endpoints
- https://docs.yjs.dev/
- https://docs.yjs.dev/getting-started/adding-awareness
- https://redis.io/docs/latest/develop/pubsub/
- https://redis.io/docs/latest/develop/use-cases/streaming/
- https://developers.cloudflare.com/durable-objects/best-practices/websockets/
- https://developers.cloudflare.com/durable-objects/
- https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html
- https://www.rfc-editor.org/info/rfc8725/

## Current OpenRTC surface

Implemented:
- WebSocket auth with JWT issuer/audience/signature validation.
- Scoped room authorization through `join`, `publish`, `presence`, scoped claims, and Redis-backed room access grants.
- Tenant-prefix room isolation.
- Presence snapshots, live presence updates, disconnect/offline events, admin active-user reads, and admin-set ephemeral presence with TTL.
- Redis-backed admin room metadata and access-grant lifecycle APIs with scoped `rooms:` authorization.
- Redis-backed admin storage document APIs with scoped `storage:` authorization and atomic JSON Patch.
- Liveblocks-style `LiveObject`, `LiveList`, and `LiveMap` typed storage envelope validation for durable storage documents and patches.
- Redis-backed room threads/comments with scoped `comments:` authorization and
  inbox notifications/settings with scoped `notifications:` authorization.
- Thread list query/pagination for resolved state and thread metadata filters.
- Per-user thread read-state APIs, unread thread list filters, and
  client/React read-state hooks.
- Embeddable React `CommentsPanel` hosted comments surface for durable room
  threads, replies, read/unread state, and resolve/reopen actions.
- First-class client/React room thread subscription workflows over durable
  room subscription settings, including subscribe-all, replies/mentions, mute,
  reset, list, and embeddable controls.
- Room broadcast from clients and admin service.
- Multi-node fan-out through Redis Pub/Sub.
- Binary `/yjs/{room}` endpoint with persisted snapshot/update replay and cross-node fan-out.
- Yjs provider awareness bridge over OpenRTC presence for ephemeral user/cursor state.
- React/client packages plus Yjs and rich-text editor binding/presence helpers.
- Dependency-free rich-text editor canvas controllers for host-owned Tiptap,
  Lexical, BlockNote, and generic editor setup, combining OpenRTC room/Yjs
  lifecycle with durable anchored comment, thread, and subscription actions.
- Runtime room admission controls for JSON and Yjs sockets, with configurable
  per-room caps, retryable `ROOM_CAPACITY` JSON join errors, Yjs HTTP 429
  upgrade rejection, and dev socket room activity/limit inspection.
- Non-Yjs room event delivery ACKs: `@openrtc/client` tracks latest delivered
  `EVENT.meta.seq`, sends bounded reconnect catch-up with `JOIN.meta.after_seq`,
  automatically sends `EVENT_ACK`, Redis-backed runtimes persist per-subject
  room ACK cursors, and runtime/dev socket snapshots expose per-connection ACK
  cursors.
- Origin allowlist, bounded JSON payloads, bounded Yjs frames, bounded admin bodies, and shared room/event/connection ID validation.

Missing for parity:
- Fully managed text-editor product features beyond host-owned editor canvases,
  including server-side editing, multiplayer undo/redo, rich suggestion flows,
  and packaged version-history UI.
- Managed version history beyond local ACK-backed storage undo/redo and Yjs
  compaction snapshots.
- Room-affine placement and Yjs compactor retention alerts tuned against
  production traffic. Per-node JSON/Yjs room admission caps exist, but they are
  not yet a regional placement or autoscaling policy.
- Full durable resumable session protocol beyond the current bounded Redis
  replay plus durable per-subject room ACK cursor. Remaining work includes a
  resumable socket/session token window and stronger delivery guarantees after
  Redis retention expires.
- Region/tenant placement, room-affine routing, load-shedding, and horizontal scale tests at product scale.
- Published API coverage thresholds for Go and TypeScript.

## Architecture decision

Keep Redis Pub/Sub as the low-latency fan-out bus, but do not treat it as durable collaboration state. Durable state needs a room/document store with sequence IDs, snapshots, retention, and compaction. For massive scale, route each room to a single active coordinator shard or edge object, and use durable storage for replay. That keeps the runtime light while avoiding split-brain document persistence and unbounded Redis lists.

Recommended target shape:

1. Runtime edge/coordinator
   - Owns active sockets for a room shard.
   - Applies auth, Origin checks, rate limits, size limits, and message validation.
   - Fans out ephemeral events immediately.

2. Durable document service
   - Stores Yjs updates with monotonically increasing sequence IDs.
   - Periodically compacts updates into snapshots.
   - Keeps safe retention windows and supports replay from a sequence checkpoint.

3. Product APIs
   - Room CRUD and permissions.
   - Storage JSON/Patch APIs.
   - Comments, mentions, inbox notifications, notification settings, webhook delivery.
   - Admin presence and broadcast APIs.

4. SDKs
   - Core client and React hooks.
   - Yjs provider.
   - Editor adapters for Tiptap, BlockNote, Lexical, Slate/Quill/CodeMirror where practical.
   - Auth-token refresh and reconnect/resume helpers.

## Changes made from this review

- Added explicit WebSocket Origin allowlist support.
- Required explicit action grants for every room action.
- Added separate admin JWKS configuration.
- Added common validation for room IDs, event names, message IDs, connection IDs, JSON payloads, admin request bodies, and Yjs room paths.
- Added configurable `OPENRTC_LIMIT_YJS_MAX_BYTES`.
- Applied per-connection rate limiting to Yjs write frames before storage or fan-out.
- Added a sequenced Redis Yjs update log and trusted compaction primitive so merged snapshots can safely checkpoint and trim known updates.
- Added `@openrtc/yjs-compactor`, a trusted TypeScript compactor that uses Yjs merge logic to compute checkpoint snapshots and trim sequenced Redis update logs.
- Added `@openrtc/rich-text` Yjs binding helpers for Tiptap, Lexical, and
  BlockNote-style editor integrations alongside selection presence adapters.
- Added executable `@openrtc/rich-text` integration wrappers and remote
  selection helpers for Tiptap, Lexical, and BlockNote-style editor setup and
  cleanup without importing editor packages into OpenRTC.
- Added dependency-free `@openrtc/rich-text` editor canvas controllers for
  Tiptap, Lexical, BlockNote, and generic host editors. These combine room/Yjs
  lifecycle, selection-derived comment anchors, durable create/reply,
  resolve/reopen, and room subscription actions while leaving editor packages
  in the host application.
- Added runtime room admission/load-shedding controls:
  `OPENRTC_LIMIT_ROOM_CONNECTIONS` and
  `OPENRTC_LIMIT_YJS_ROOM_CONNECTIONS`, retryable `ROOM_CAPACITY` JSON join
  errors, Yjs HTTP 429 upgrade rejection, and `/dev/sockets` room activity plus
  limit metadata for integration debugging.
- Added Redis-backed room metadata CRUD/list admin APIs with `rooms:` scoped authorization.
- Added Liveblocks-style `defaultAccesses`, `usersAccesses`, and `groupsAccesses` room grants. Runtime and admin room data actions honor existing access-token scopes first, then fall back to room grants for ID-token-style subject/group authorization in cluster mode.
- Added Redis-backed storage get/set/delete and atomic JSON Patch admin APIs with `storage:` scoped authorization and room-grant fallback.
- Added typed storage validation for `LiveObject`, `LiveList`, and `LiveMap` envelopes so PUT and JSON Patch cannot persist malformed typed storage documents.
- Added Redis-backed active-user room reads with `presence:` scoped authorization and room-grant fallback.
- Added Redis-backed room thread/comment APIs with `comments:` scoped
  authorization, room-grant fallback, object-rooted comment bodies/metadata,
  and durable per-room thread records.
- Added thread lifecycle APIs for single-thread reads, metadata/resolved updates,
  deletion, realtime thread update/delete events, webhooks, SDK helpers, and
  React action hooks.
- Added Redis-backed inbox notifications, notification settings, and room
  subscription settings with `notifications:` scoped authorization and durable
  read/reset state.
- Added an embeddable React `CommentsPanel` hosted comments surface on top of
  the durable thread/comment, read-state, and resolve/reopen APIs.
- Added first-class SDK/React thread subscription workflows and
  `RoomSubscriptionControls` on top of durable room subscription settings.
- Added a lightweight Yjs awareness-compatible SDK bridge over existing
  OpenRTC presence so user/cursor state remains ephemeral and separate from
  persisted Yjs document updates.
- Added a deployable Yjs compactor worker shape with Docker/Kubernetes/Compose
  wiring, per-room retries, Prometheus-format metrics, and supervisor-friendly
  failure exits.
- Hardened JWKS fetches to reject non-2xx responses and cap response size.

## Remaining scaling risk

The current Yjs persistence now has sequence IDs, a safe compaction primitive,
and an operational compactor worker with metrics and retry behavior. The
remaining scale risk is proving the thresholds under production room traffic,
adding deployment-specific alerts, and adding room-affine placement or sharding
so hot rooms do not overload a shared Redis deployment.
