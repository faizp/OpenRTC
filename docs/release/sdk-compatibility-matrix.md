# SDK Compatibility Matrix

Run this matrix before publishing SDK or backend releases. It covers protocol,
admin product APIs, React bindings, rich text, and Yjs compaction compatibility.

## Supported Packages

| Package | Runtime/Admin Contract | Required Checks |
| --- | --- | --- |
| `@openrtc/client` | JSON WebSocket protocol, admin REST API, dev tooling API | `npm run typecheck`, `npm run test` in `packages/client` |
| `@openrtc/react` | `@openrtc/client` room lifecycle and hooks | `npm run typecheck`, `npm run test` in `packages/react` |
| `@openrtc/rich-text` | Yjs document bindings and rich-text admin document API | `npm run typecheck`, `npm run test` in `packages/rich-text` |
| `@openrtc/yjs` | Binary Yjs runtime endpoint | `npm run typecheck`, `npm run test` in `packages/yjs` |
| `@openrtc/yjs-compactor` | Redis Yjs snapshot/update keys | `npm run typecheck`, `npm run test` in `packages/yjs-compactor` |

## Release Matrix

| Backend Change | Client | React | Rich Text | Yjs | Compactor | Required Compatibility Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| Protocol schema only | Required | Required | Optional | Optional | Optional | Protocol schemas updated, client tests pass |
| Admin product API | Required | Optional | Required when rich-text/version APIs change | Optional | Optional | Admin client product tests pass |
| Storage mutation/event behavior | Required | Required | Required | Optional | Optional | Storage history, version snapshot, and browser smoke pass |
| Yjs persistence keys | Optional | Optional | Required | Required | Required | Yjs realtime and compactor tests pass |
| Auth/tenant rules | Required | Required | Required | Required | Optional | Tenant-prefix authorization tests pass |
| Webhook delivery semantics | Required | Optional | Optional | Optional | Optional | Delivery history, retry, and DLQ tests pass |

## Commands

```bash
go test ./server/internal/cluster ./server/internal/admin ./server/internal/runtime
npm run typecheck
npm run test
make production-check
```

For a release candidate, also run the browser probe from the reference app or
`openrtc dev probe --multi-user --realtime --yjs-realtime --reconnect --json`.

## Compatibility Rules

- Adding optional response fields is backward-compatible.
- Removing fields, renaming fields, changing event names, or changing wire
  message semantics requires a major version or explicit migration notes.
- Admin product APIs must keep one-time secret behavior for API keys: the raw
  secret is only returned by create.
- Rich-text document content is treated as host-defined JSON. The server must not
  rewrite it outside of validation and persistence.
- Resume-session records are product-managed recovery state. Runtime clients
  can pass `resumeSession` / `projectId`; Redis-backed runtimes merge stored
  room cursors into reconnect joins and advance them from `EVENT_ACK`.
