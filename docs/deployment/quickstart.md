# Deployment Quickstart

## Prerequisites

- Docker or Go 1.18+ (for building from source)
- Redis 7+ (required for cluster mode)
- An OIDC provider with JWKS endpoint (Auth0, Keycloak, etc.)

## Local Integration Dev Server

For local SDK and reference-app integration work, `openrtc dev` starts a local
issuer, runtime, admin API, seeded rooms, same-origin proxies, and the browser
dev console. It uses embedded Redis-compatible state by default.

```bash
go run ./server/cmd/openrtc dev

# Optional external Redis:
go run ./server/cmd/openrtc dev --storage redis --redis-url redis://localhost:6379/0
```

Open `http://127.0.0.1:3000`. Use
`http://127.0.0.1:3000/dev/token?pubkey=pk_localdev` for anonymous local client
tokens. The token response also includes the local `config` object and default
`room`, so browser clients can connect without hard-coding dev URLs; the
`@openrtc/client` `createOpenRTCDevClient()` and
`createOpenRTCDevAdminClient()` helpers consume this response directly. Use
`http://127.0.0.1:3000/dev/status` to verify storage backend, Redis protocol health, runtime/admin generation,
seeded-room, and endpoint readiness. Use
`http://127.0.0.1:3000/dev/connections?room=demo:room-1` to inspect room active
users, `http://127.0.0.1:3000/dev/sockets` to inspect the actual local runtime
WebSocket and Yjs sockets, and
`http://127.0.0.1:3000/dev/yjs?room=demo:room-1` to inspect Yjs snapshot/update
metadata without dumping CRDT bytes. Use
`http://127.0.0.1:3000/dev/events?room=demo:room-1` to inspect the bounded room
event log that powers join catch-up. The Ops tab includes dev status,
socket/event inspection, and a runtime reconnect drill that restarts the local
runtime, verifies the generation advanced, and checks reconnect plus presence echo.
Seeded rooms include a small typed `LiveObject` storage document unless storage
already exists, so `http://127.0.0.1:3000/dev/storage?room=demo:room-1` is useful
immediately after startup.

## Option 1: Docker Compose (Recommended for Getting Started)

```bash
# Clone the repo
git clone https://github.com/openrtc/openrtc.git
cd openrtc

# Edit docker-compose.yml to set your auth provider:
#   OPENRTC_AUTH_ISSUER, OPENRTC_AUTH_AUDIENCE, OPENRTC_AUTH_JWKS_URL

# Start all services
docker compose up -d

# Verify
curl http://localhost:8080/healthz   # runtime
curl http://localhost:8090/healthz   # admin
```

Services:
- **Runtime** (WebSocket): `localhost:8080`, WS path `/ws`
- **Runtime** (Yjs): `localhost:8080`, Yjs path `/yjs/{url-escaped-room}`
- **Admin API**: `localhost:8090`
- **Yjs compactor metrics**: `localhost:9102/metrics`
- **Redis**: `localhost:6379`

## Option 2: Kubernetes

Apply manifests from `deploy/k8s/`:

```bash
# Edit configmap.yaml and secret with your values
kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/configmap.yaml
kubectl apply -f deploy/k8s/redis.yaml
kubectl apply -f deploy/k8s/runtime.yaml
kubectl apply -f deploy/k8s/admin.yaml
kubectl apply -f deploy/k8s/yjs-compactor.yaml
kubectl apply -f deploy/k8s/ingress.yaml
```

**Important:** WebSocket connections require sticky sessions and TLS. The ingress
manifest includes nginx annotations for cookie-based affinity and expects an
`openrtc-tls` secret for `openrtc.example.com`. Adjust both for your ingress
controller and certificate manager.

## Option 3: Build from Source

```bash
cd server
go build -o openrtc-runtime ./cmd/openrtc-runtime
go build -o openrtc-admin ./cmd/openrtc-admin

# Run runtime
export OPENRTC_MODE=cluster
export OPENRTC_NODE_ID=node-1
export OPENRTC_REDIS_URL=redis://localhost:6379/0
export OPENRTC_ALLOWED_ORIGINS=https://app.example.com
export OPENRTC_AUTH_ISSUER=https://your-issuer.example.com
export OPENRTC_AUTH_AUDIENCE=openrtc-clients
export OPENRTC_AUTH_JWKS_URL=https://your-issuer.example.com/.well-known/jwks.json
export OPENRTC_ADMIN_AUTH_ISSUER=https://your-issuer.example.com
export OPENRTC_ADMIN_AUTH_AUDIENCE=openrtc-admin
./openrtc-runtime

# Run admin (separate terminal)
export OPENRTC_SERVER_PORT=8090
./openrtc-admin
```

## Configuration Reference

All configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `OPENRTC_MODE` | `single` | `single` or `cluster` |
| `OPENRTC_NODE_ID` | (required) | Unique node identifier |
| `OPENRTC_SERVER_HOST` | `0.0.0.0` | Bind address |
| `OPENRTC_SERVER_PORT` | `8080` | HTTP/WS port |
| `OPENRTC_WS_PATH` | `/ws` | WebSocket endpoint path |
| `OPENRTC_ALLOWED_ORIGINS` | — | Comma-separated WebSocket Origin allowlist. Empty allows all; set in production. |
| `/yjs/{room}` | fixed | Binary Yjs sync endpoint path |
| `OPENRTC_REDIS_URL` | — | Redis connection URL (required in cluster mode) |
| `OPENRTC_AUTH_ISSUER` | (required) | JWT issuer for client tokens |
| `OPENRTC_AUTH_AUDIENCE` | (required) | JWT audience for client tokens |
| `OPENRTC_AUTH_JWKS_URL` | (required) | JWKS endpoint URL |
| `OPENRTC_ADMIN_AUTH_ISSUER` | — | JWT issuer for admin tokens (optional) |
| `OPENRTC_ADMIN_AUTH_AUDIENCE` | — | JWT audience for admin tokens (optional) |
| `OPENRTC_ADMIN_AUTH_JWKS_URL` | `OPENRTC_AUTH_JWKS_URL` | JWKS endpoint for admin tokens |
| `OPENRTC_WEBHOOK_URL` | — | Single absolute HTTP(S) webhook endpoint for admin mutation events |
| `OPENRTC_WEBHOOK_URLS` | — | Comma-separated additional absolute HTTP(S) webhook endpoints |
| `OPENRTC_WEBHOOK_SECRET` | — | Shared signing secret; required when webhook URLs are configured |
| `OPENRTC_WEBHOOK_TIMEOUT_MS` | `2000` | Per-request webhook delivery timeout in milliseconds |
| `OPENRTC_TENANT_ENFORCE_PREFIX` | `true` | Enforce tenant prefix on room names |
| `OPENRTC_TENANT_SEPARATOR` | `:` | Separator between tenant and room name |
| `OPENRTC_LIMIT_PAYLOAD_MAX_BYTES` | `16384` | Max payload size (bytes) |
| `OPENRTC_LIMIT_ENVELOPE_MAX_BYTES` | `20480` | Max envelope size (bytes) |
| `OPENRTC_LIMIT_YJS_MAX_BYTES` | `1048576` | Max binary Yjs frame size (bytes) |
| `OPENRTC_LIMIT_ROOMS_PER_CONNECTION` | `50` | Max rooms per WebSocket connection |
| `OPENRTC_LIMIT_EMITS_PER_SECOND` | `100` | Max emits per connection per second |
| `OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH` | `256` | Outbound message queue depth |
| `OPENRTC_YJS_COMPACTOR_INTERVAL_MS` | `60000` | Compactor polling interval for all rooms |
| `OPENRTC_YJS_COMPACTOR_MIN_UPDATES` | `500` | Minimum sequenced updates before compaction |
| `OPENRTC_YJS_COMPACTOR_MIN_BYTES` | `1048576` | Minimum update bytes before compaction |
| `OPENRTC_YJS_COMPACTOR_ROOM_RETRIES` | `2` | Per-room compaction retries before counting a failure |
| `OPENRTC_YJS_COMPACTOR_RETRY_BACKOFF_MS` | `1000` | Delay between per-room compaction retries |
| `OPENRTC_YJS_COMPACTOR_MAX_CONSECUTIVE_FAILURES` | `10` | Worker exits after this many scan/room failures so the supervisor can restart it |
| `OPENRTC_YJS_COMPACTOR_METRICS_HOST` | `0.0.0.0` | Compactor metrics bind host |
| `OPENRTC_YJS_COMPACTOR_METRICS_PORT` | — | Optional compactor Prometheus metrics port |

## Verifying Your Deployment

```bash
# Health check (should return "ok")
curl http://<runtime-host>:8080/healthz

# Readiness check (returns "ready" when Redis is connected in cluster mode)
curl http://<runtime-host>:8080/readyz

# Prometheus metrics
curl http://<runtime-host>:8080/metrics
curl http://<compactor-host>:9102/metrics

# Admin stats
curl -H "Authorization: Bearer <admin-jwt>" http://<admin-host>:8090/v1/stats

# Room metadata lifecycle. Requires an admin token with a scope such as
# rooms:tenant-a:*.
curl -X POST -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"id":"tenant-a:room-1","metadata":{"name":"Room 1"},"defaultAccesses":["room:read","room:presence:write"],"groupsAccesses":{"editors":["room:write"]}}' \
  http://<admin-host>:8090/v1/rooms

curl -H "Authorization: Bearer <admin-jwt>" \
  "http://<admin-host>:8090/v1/rooms?prefix=tenant-a:&limit=50"

# Active users. Requires an admin token with a scope such as
# presence:tenant-a:*.
curl -H "Authorization: Bearer <admin-jwt>" \
  http://<admin-host>:8090/v1/rooms/tenant-a:room-1/active_users

# Durable room storage. Requires an admin token with a scope such as
# storage:tenant-a:*.
curl -X PUT -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"layers":["base"],"meta":{"title":"Draft"}}' \
  http://<admin-host>:8090/v1/rooms/tenant-a:room-1/storage

# Typed storage envelopes are also accepted and validated.
curl -X PUT -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"liveblocksType":"LiveObject","data":{"items":{"liveblocksType":"LiveList","data":["base"]}}}' \
  http://<admin-host>:8090/v1/rooms/tenant-a:room-1/storage

curl -X PATCH -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '[{"op":"add","path":"/layers/-","value":"foreground"}]' \
  http://<admin-host>:8090/v1/rooms/tenant-a:room-1/storage/json-patch

# Server-side ephemeral presence for agents/backends
curl -X POST -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"room":"tenant-a:room-1","conn_id":"agent-1","state":{"status":"thinking"},"ttl_seconds":60}' \
  http://<admin-host>:8090/v1/presence
```

## Sticky Sessions

WebSocket connections are stateful. A client must hit the same runtime node for
the duration of its session. Configure your load balancer for session affinity:

- **nginx**: `ip_hash` or cookie-based sticky sessions
- **K8s Ingress**: See annotations in `deploy/k8s/ingress.yaml`
- **AWS ALB**: Enable stickiness on target group
- **Cloudflare**: Use session affinity in load balancer settings
