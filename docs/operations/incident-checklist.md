# Incident Checklist

Use this checklist when investigating OpenRTC production issues.

## 1. Triage

- [ ] Which service is affected? (runtime / admin / Redis)
- [ ] Scope: single node, all nodes, or specific rooms/tenants?
- [ ] When did it start? Correlate with deploys, config changes, or Redis events.
- [ ] What do health endpoints say?
  ```
  curl http://<host>:8080/healthz
  curl http://<host>:8080/readyz
  ```

## 2. Connectivity

- [ ] Can clients establish WebSocket connections?
- [ ] Are existing connections being dropped?
- [ ] Check `active_connections` metric for sudden changes
- [ ] Check `queue_overflows_total` for backpressure issues
- [ ] Check client-side errors (close code 4410 = queue overflow, 4001 = auth invalid)

## 3. Redis Health

- [ ] Is Redis reachable? `redis-cli -h <host> ping`
- [ ] Redis memory usage: `redis-cli info memory`
- [ ] Redis connected clients: `redis-cli info clients`
- [ ] Redis Pub/Sub channels: `redis-cli pubsub channels "room:*"`
- [ ] Check `/readyz` on all nodes — returns 503 if Redis is down

## 4. Authentication

- [ ] JWKS endpoint reachable from runtime pods?
- [ ] Token issuer/audience match config?
- [ ] Token not expired?
- [ ] Key rotation in last 5 minutes? (cached — may need to wait)

## 5. Message Delivery

- [ ] Are messages delivered on the same node? (local fan-out)
- [ ] Are messages delivered across nodes? (Redis Pub/Sub)
- [ ] Check `events_total` metric is incrementing
- [ ] Check Redis Pub/Sub is active: `redis-cli pubsub numsub "room:<room>"`

## 6. Stale State

- [ ] Stale members in room? Check: `redis-cli smembers "room:<room>:members"`
- [ ] Connection alive? Check: `redis-cli exists "conn:<conn_id>:alive"`
- [ ] Reconciler running? Stale entries should clear within ~75s
- [ ] Node connection index: `redis-cli smembers "node:<node_id>:conns"`

## 7. Recovery Actions

- [ ] Restart affected runtime pods (if not self-healing)
- [ ] Verify Redis is stable and accepting connections
- [ ] Monitor metrics for 10 minutes post-recovery
- [ ] Check no stale connections remain: `redis-cli keys "conn:*:alive"`
- [ ] Document timeline and root cause
