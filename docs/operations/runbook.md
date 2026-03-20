# OpenRTC Operational Runbook

## Health Endpoints

| Endpoint | Service | Purpose |
|----------|---------|---------|
| `GET /healthz` | runtime, admin | Liveness — returns `ok` if process is running |
| `GET /readyz` | runtime, admin | Readiness — returns `ready`; fails (503) if Redis is unreachable in cluster mode |
| `GET /metrics` | runtime, admin | Prometheus metrics in text format |

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
| `room:{room}:members` | set | — | Active connection IDs in the room |
| `room:{room}:presence` | hash | — | Presence state per connection ID |
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
- **Redis is the bottleneck:** All cross-node traffic flows through Redis.
  For very high throughput, consider Redis Cluster or sharding by tenant.
- **Admin API is stateless:** Scale independently based on publish volume.
- **Sticky sessions required:** Each runtime replica must consistently receive
  traffic from the same client for the duration of the WebSocket session.
