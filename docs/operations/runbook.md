# OpenRTC Operational Runbook

## Health Endpoints

| Endpoint | Service | Purpose |
|----------|---------|---------|
| `GET /healthz` | runtime, admin | Liveness — returns `ok` if process is running |
| `GET /readyz` | runtime, admin | Readiness — returns `ready`; fails (503) if Redis is unreachable in cluster mode |
| `GET /metrics` | runtime, admin | Prometheus metrics in text format |
| `GET /metrics` | yjs-compactor | Prometheus metrics in text format when `OPENRTC_YJS_COMPACTOR_METRICS_PORT` is set |

## Key Metrics

### Runtime

| Metric | Type | Alert Threshold |
|--------|------|-----------------|
| `openrtc_runtime_active_connections` | gauge | Monitor for sudden drops (possible node crash) |
| `openrtc_runtime_active_rooms` | gauge | Informational |
| `openrtc_runtime_joins_total` | counter | — |
| `openrtc_runtime_leaves_total` | counter | — |
| `openrtc_runtime_events_total` | counter | — |
| `openrtc_runtime_presence_updates_total` | counter | — |
| `openrtc_runtime_queue_overflows_total` | counter | Any increment indicates backpressure; investigate slow consumers |

### Admin

| Metric | Type | Alert Threshold |
|--------|------|-----------------|
| `openrtc_admin_publishes_total` | counter | — |

### Yjs Compactor

| Metric | Type | Alert Threshold |
|--------|------|-----------------|
| `openrtc_yjs_compactor_runs_total` | counter | No increase for 2x expected interval |
| `openrtc_yjs_compactor_failures_total` | counter | Any sustained increase |
| `openrtc_yjs_compactor_consecutive_failures` | gauge | `> 0` for 5 minutes |
| `openrtc_yjs_compactor_rooms_scanned_total` | counter | Informational |
| `openrtc_yjs_compactor_rooms_compacted_total` | counter | Informational |
| `openrtc_yjs_compactor_updates_compacted_total` | counter | Informational |
| `openrtc_yjs_compactor_last_success_timestamp_seconds` | gauge | Too old for expected workload |

## Incident Response

### Redis Connection Lost

**Symptoms:**
- `/readyz` returns 503
- No cross-node message delivery
- New JOINs may fail in cluster mode

**Steps:**
1. Check Redis health: `redis-cli ping`
2. Check Redis connectivity from runtime pods: network policies, DNS resolution
3. Check Redis memory: `redis-cli info memory`
4. If Redis restarted: runtime will detect via next heartbeat cycle (15s)
5. Clients on the same node still receive messages (local fan-out works)
6. Monitor `queue_overflows_total` — queue buildup during outage can trigger disconnects

**Recovery:**
- Once Redis is back, runtime reconnects automatically on next operation
- Reconciler (30s cycle) will clean up any stale state from the outage

### High Queue Overflow Rate

**Symptoms:**
- `openrtc_runtime_queue_overflows_total` increasing
- Clients being disconnected with close code 4410 (`QUEUE_OVERFLOW`)

**Cause:** Consumers are not reading messages fast enough. Common reasons:
- Slow client network
- Very large rooms with high message volume
- Client-side processing blocking the read loop

**Steps:**
1. Check room sizes — identify rooms with many members and high emit rates
2. If specific clients: issue is on the consumer side
3. If widespread: consider increasing `OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH`
4. Consider lowering `OPENRTC_LIMIT_EMITS_PER_SECOND` to reduce inbound rate

### Node Crash / Stale Connections

**Symptoms:**
- Clients appear in room member lists but are unreachable
- Presence data shows users who have disconnected

**Recovery (automatic):**
1. Connection `alive` keys have a 45-second TTL
2. Reconciler runs every 30 seconds on each node
3. Reconciler removes stale connections whose `alive` key has expired
4. Expected cleanup time: 45s (TTL) + 30s (reconciler) = ~75s worst case
5. Node crash recovery uses `node:{node_id}:conns` index for faster cleanup

**If cleanup is stuck:**
```bash
# Check for stale connections manually
redis-cli keys "conn:*:alive"

# Check a specific connection's TTL
redis-cli ttl "conn:<conn_id>:alive"

# Force cleanup of a specific connection (use with caution)
redis-cli del "conn:<conn_id>:alive" "conn:<conn_id>:meta"
redis-cli srem "room:<room>:members" "<conn_id>"
redis-cli hdel "room:<room>:presence" "<conn_id>"
```

### Authentication Failures

**Symptoms:**
- Clients getting 401 on WebSocket upgrade
- `invalid bearer token` errors

**Steps:**
1. Verify JWKS endpoint is reachable from runtime: `curl <OPENRTC_AUTH_JWKS_URL>`
2. Check JWT claims match config: `iss` must match `OPENRTC_AUTH_ISSUER`, `aud` must match `OPENRTC_AUTH_AUDIENCE`
3. Check token expiration (`exp` claim)
4. JWKS keys are cached for 5 minutes — if you rotated keys, wait for cache expiry

### Rate Limiting

**Symptoms:**
- Clients receiving ERROR with code `RATE_LIMITED`

**Context:**
- Default: 100 emits per second per connection
- Rate limiter uses a 1-second sliding window
- Configurable via `OPENRTC_LIMIT_EMITS_PER_SECOND`

**Steps:**
1. Determine if client is legitimately sending too fast
2. If needed, increase the limit (consider server capacity)
3. Client should implement backoff on `RATE_LIMITED` errors

## Redis Key Reference

| Key Pattern | Type | TTL | Purpose |
|-------------|------|-----|---------|
| `conn:{conn_id}:alive` | string | 45s | Heartbeat — existence means connection is alive |
| `conn:{conn_id}:meta` | hash | — | Connection metadata (user, tenant, node, connected_at) |
| `room:{room}:record` | hash | — | Durable room metadata record managed by admin room APIs |
| `room:{room}:storage` | string | — | Durable JSON storage document managed by admin storage APIs |
| `room:{room}:storage:seq` | string | — | Monotonic storage document sequence for conditional realtime writes |
| `room:{room}:threads` | set | — | Durable thread IDs for the room |
| `room:{room}:thread:{thread_id}` | hash | — | Durable thread metadata |
| `room:{room}:thread:{thread_id}:comments` | list | — | Durable ordered comments for a thread |
| `inbox:{notification_id}` | hash | — | Durable inbox notification payload and read timestamp |
| `user:{user_id}:inbox` | sorted set | — | User inbox notification IDs sorted by notified time |
| `user:{user_id}:inbox:unread` | sorted set | — | Unread inbox notification IDs |
| `user:{user_id}:notification_settings` | string | — | User project-level notification settings JSON |
| `user:{user_id}:room_subscription_settings` | sorted set | — | Explicit room subscription setting room IDs |
| `room:{room}:user:{user_id}:subscription_settings` | hash | — | Room-level notification subscription settings |
| `room:{room}:members` | set | — | Active connection IDs in the room |
| `room:{room}:presence` | hash | — | Presence state per connection ID |
| `room:{room}:presence:ephemeral` | set | — | Server-side presence IDs controlled by TTL keys |
| `room:{room}:presence:ephemeral:{conn_id}:alive` | string | requested TTL | Expiry marker for server-side presence |
| `room:{room}:yjs:snapshot` | string | — | Legacy full Yjs snapshot update for the room |
| `room:{room}:yjs:updates` | list | — | Legacy incremental Yjs updates |
| `room:{room}:yjs:snapshot:v2` | string | — | Sequenced snapshot record with compaction checkpoint |
| `room:{room}:yjs:updates:v2` | sorted set | — | Sequenced incremental Yjs update records |
| `room:{room}:yjs:seq` | string | — | Monotonic Yjs update sequence counter |
| `node:{node_id}:conns` | set | — | All connections on a node (for crash cleanup) |
| `stats:node:{node_id}` | hash | — | Per-node aggregate counters |
| `stats:nodes` | set | — | Set of active node IDs |

## Graceful Shutdown

The runtime handles `SIGTERM`:
1. Stops accepting new connections
2. Existing connections finish current operations
3. Server drains with a grace period
4. Redis connections are closed

In Kubernetes, the pod termination grace period (default 30s) should be sufficient.

## Scaling Guidelines

- **Horizontal scaling:** Add more runtime replicas. Each handles independent
  WebSocket connections. Cross-node messaging goes through Redis Pub/Sub.
- **Yjs scaling:** Yjs updates are persisted as a sequenced Redis log and
  replayed on reconnect after the latest snapshot checkpoint. Ordinary client
  snapshots update the baseline snapshot but do not trim logs. Only a trusted
  compactor that has merged known updates should advance a checkpoint and trim
  update records at or below that sequence.
- **Yjs compaction:** Run `@openrtc/yjs-compactor` against the same Redis
  backend as the runtime. The package uses Yjs merge semantics to produce a
  normalized snapshot, writes `room:{room}:yjs:snapshot:v2`, and trims
  `room:{room}:yjs:updates:v2` through the checkpoint sequence. Example:
  `OPENRTC_REDIS_URL=redis://localhost:6379 pnpm --filter @openrtc/yjs-compactor compact:once`.
  Use `--room <room>` for a single room, or run without `--once` as a polling
  worker. The production worker should expose `OPENRTC_YJS_COMPACTOR_METRICS_PORT`,
  retry transient room failures, and exit after
  `OPENRTC_YJS_COMPACTOR_MAX_CONSECUTIVE_FAILURES` so Kubernetes or another
  supervisor can restart it. Do not compact rooms that still have legacy
  `room:{room}:yjs:updates` records until they have been migrated to sequenced
  v2 records.
- **Storage scaling:** Room storage is durable JSON in Redis and JSON Patch is
  applied atomically with a watched transaction. Plain object roots and typed
  `LiveObject`/`LiveList`/`LiveMap` envelopes are validated before persistence.
  Keep documents small enough for `OPENRTC_LIMIT_PAYLOAD_MAX_BYTES`; use Yjs
  document sync for high-frequency collaborative edits.
- **Active users:** `GET /v1/rooms/{room}/active_users` is a read over room
  membership, connection metadata, and presence hashes. Treat it as an admin or
  dashboard endpoint, not a high-frequency client polling path. Yjs awareness
  data appears inside presence under `__openrtc_yjs_awareness`; avoid putting
  sensitive profile data there because it is visible to room peers and admin
  active-user readers.
- **Room access grants:** Room records can include `defaultAccesses`,
  `usersAccesses`, and `groupsAccesses`. Runtime nodes check access-token
  scopes first, then room grants for ID-token-style subject/group auth. Keep
  `room:{room}:record` in the same Redis deployment as runtime nodes or grant
  fallback will deny.
- **Redis is the bottleneck:** All cross-node traffic flows through Redis.
  For very high throughput, consider Redis Cluster or sharding by tenant.
- **Admin API is stateless:** Scale independently based on publish volume.
- **Sticky sessions required:** Each runtime replica must consistently receive
  traffic from the same client for the duration of the WebSocket session.
