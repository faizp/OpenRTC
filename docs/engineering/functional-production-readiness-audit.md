# Functional Production Readiness Audit

This audit tracks OpenRTC's production-readiness status for functionality only.
It intentionally excludes regional placement, autoscaling, and product-scale
load work.

## 2026-07-05 Product Surface Completion

Implemented in this run:

- API key bearer authentication now works for admin REST and runtime
  WebSocket/Yjs authentication. Secrets are verified through the Redis product
  store by prefix lookup plus hash comparison, revoked keys are rejected, and
  verified keys synthesize the same scoped claims used by JWT authorization.
- Runtime resume sessions are no longer admin-only records. Clients can pass
  `projectId` and `resumeSession`; Redis-backed runtimes create/load the
  session, merge stored room cursors into reconnect `JOIN.meta.after_seq`, and
  advance cursors from `EVENT_ACK`.
- Runtime usage metering now records project-scoped connections, room
  joins/leaves, emitted/ACKed events, presence updates, storage get/set/patch,
  and Yjs frame activity when a project id is available.
- Admin webhook delivery history now has a background retry worker for failed
  deliveries whose `nextAttemptAt` is due, in addition to manual retry and
  dead-letter actions.
- The reference console Production tab now includes a managed contenteditable
  rich-text surface with formatting controls, HTML sanitization before display,
  and managed rich-text document persistence.

Validation run:

- `go test ./server/internal/cluster ./server/internal/admin ./server/internal/runtime`
- `npm run test` in `packages/client`
- `npm run typecheck` in `packages/client`
- Reference app embedded script parse check with Node

Current functional status:

- The five previously listed productization gaps now have working functionality
  in backend, SDK, reference UI, and docs.
- Remaining production work is operational depth rather than core feature
  absence: scale tuning, multi-node retry coordination, richer packaged editor
  UX, and broader end-to-end browser smoke coverage for the new Production tab.

## 2026-07-05 Customer Console Completion

Implemented in this run:

- Added customer-facing product dashboard, status, and support debug bundle
  admin endpoints. They aggregate tenant/project records, API keys, rooms,
  active users, recent room events, storage, usage, audit logs, webhook
  deliveries, resume sessions, rich-text documents, version snapshots, status
  checks, public status-page copy, and safe runtime configuration.
- Extended the `@openrtc/client` admin SDK with typed `dashboard`, `status`,
  and `supportDebugBundle` methods.
- Expanded the reference console Production tab with local signup/login
  session state, org/workspace creation mapped to tenant/project records,
  environment and region metadata, API key creation, dashboard, logs,
  customer-visible errors, status, and support debug bundle actions.

Validation run:

- Focused elevated `go test ./server/internal/admin -run TestAdminProductionProductHandlers -count=1`
- `npm run test` in `packages/client`
- `npm run typecheck` in `packages/client`
- Reference app embedded script parse check with Node

## 2026-07-04 Run

Baseline before changes:

- `main` was clean and matched `origin/main`.
- Full `make check` passed before implementation.

Implemented in this run:

- Added dependency-free `@openrtc/rich-text` adapters for Slate, Quill, and
  CodeMirror.
- Each adapter now has Yjs binding helpers, selection-presence binding helpers,
  owning OpenRTC integration/session helpers, and hosted editor canvas helpers
  for comment anchors and thread/subscription actions.
- Updated root README, rich-text README, and the Liveblocks replacement
  architecture review so editor-adapter coverage matches the shipped SDK
  surface.
- Added `openrtc dev probe --multi-user` for two-user runtime validation:
  presence fan-out, custom event fan-out, event ACK/ACKED, storage patch ACK,
  and cross-user storage update fan-out.
- Added `make coverage` and `make production-check` release gates. Coverage
  now enforces Go statement coverage and TypeScript public value export
  coverage before integration checks.
- Made bounded Redis room event replay retention configurable with
  `OPENRTC_REDIS_EVENT_LOG_MAX_ENTRIES` / `redis.event_log_max_entries` and
  documented the operational runbook.

Validation run:

- `./scripts/pnpm.sh --filter @openrtc/rich-text typecheck`
- `./scripts/pnpm.sh --filter @openrtc/rich-text test`
- `go test ./server/internal/devserver -count=1`
- `node scripts/api-coverage.mjs 90`
- `./scripts/coverage.sh`
- `go test ./server/internal/config ./server/internal/cluster ./server/internal/runtime ./server/internal/admin ./server/internal/devserver -count=1`
- `make production-check`
- `go run ./server/cmd/openrtc dev probe --base-url http://127.0.0.1:3100 --reconnect --realtime --multi-user --yjs-realtime --json --timeout 20s`
- Browser smoke with Playwright against `http://127.0.0.1:3100`: clicked the
  reference console `Probe all` path, issued tokens, joined a room, observed
  presence, admin calls, events, storage, threads, notifications, and Yjs frame
  counters. Screenshot:
  `output/playwright/openrtc-reference-probe.png`.
- `git diff --check`
- `make check`

Current functional status:

- Core runtime/admin/client/React/Yjs/rich-text functionality is green under
  the repository's full validation gate.
- Host-owned editor coverage now includes Tiptap, Lexical, BlockNote, Slate,
  Quill, CodeMirror, and generic editor canvases.
- The local production gate now covers lint, typecheck, Go coverage,
  TypeScript API export coverage, package tests, and integration tests.
- Local user-like validation has a two-user CLI path for presence, event ACK,
  storage ACK/update, reconnect, and Yjs realtime probes.

Historical pending functional production work before the 2026-07-05 update:

- Full resumable socket/session token window beyond bounded event replay and
  durable per-subject ACK cursors.
- Managed version-history product surface beyond local storage history and Yjs
  compaction snapshots.
- Fully managed rich-text product features beyond host-owned editor canvases,
  including server-side editing, multiplayer editor undo/redo, rich suggestion
  flows, and packaged version-history UI.
- Deployment-specific alert thresholds for event-log retention and Yjs
  compaction must still be tuned from real production traffic.
