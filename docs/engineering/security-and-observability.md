# Security and Observability Standards

## Security

- JWT validation must check issuer, audience, expiry, and signature.
- Tenant prefix enforcement is mandatory at server room boundary.
- Admin API requires scoped service JWT claims.
- WebSocket and Yjs upgrade requests must validate `Origin` against an explicit allowlist in production.
- Room actions require explicit authorization through access-token action claims
  (`join`, `publish`, `presence`), scoped claims, or Redis-backed room access
  grants. Room-grant fallback still enforces tenant prefix isolation.
- Room access grants support `defaultAccesses`, `usersAccesses`, and
  `groupsAccesses`; only `room:write`, `room:read`,
  `room:presence:write`, and `comments:write` are accepted.
- Active-user admin reads require `presence:<pattern>` scope and are derived
  from room membership plus connection metadata; they should be monitored as
  Redis read load if used for dashboards.
- Thread/comment admin APIs require `comments:<pattern>` scope. Comment bodies
  and metadata must be object-rooted, bounded, and stored durably instead of
  sent only through Pub/Sub.
- Inbox notification and notification-settings APIs require
  `notifications:<user-pattern>` scope. Custom notification activity data and
  settings are bounded JSON objects; inbox reads mark durable records instead of
  relying on ephemeral broadcast state.
- Yjs awareness is transported as ephemeral presence under
  `__openrtc_yjs_awareness`; it must stay subject to the same presence
  authorization, payload-size limits, and privacy expectations as other
  presence state.
- Room IDs, event names, connection IDs, JSON payloads, and Yjs frames must be bounded and validated before storage or fan-out.
- Storage documents and JSON Patch requests must be bounded, object-rooted or valid typed storage roots, authorized with `storage:<pattern>`, and applied atomically.
- Typed storage envelopes reserve `liveblocksType` and must validate `LiveObject`,
  `LiveList`, and `LiveMap` `data` shapes before persistence.
- Yjs update persistence must use sequenced logs; only trusted compaction checkpoints from runtime-approved code such as `@openrtc/yjs-compactor` may trim replay updates.
- The Yjs compactor worker should expose Prometheus-format metrics, retry
  transient room failures, and exit after a bounded consecutive-failure count so
  the deployment supervisor can restart it.
- Readiness must fail in cluster mode when Redis is unhealthy.

## Observability

Required structured log fields when available:
- `tenant`
- `room`
- `conn_id`
- `trace_id`

Required instrumentation paths:
- auth pass/fail
- publish path
- fan-out path
- outbound queue overflow
- stale cleanup reconciler

## CI security gates

- language dependency vulnerability scan
- container image vulnerability scan for release artifacts
