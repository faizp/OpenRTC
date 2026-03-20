# Deployment Quickstart

## Prerequisites

- Docker or Go 1.18+ (for building from source)
- Redis 7+ (required for cluster mode)
- An OIDC provider with JWKS endpoint (Auth0, Keycloak, etc.)

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
- **Admin API**: `localhost:8090`
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
kubectl apply -f deploy/k8s/ingress.yaml
```

**Important:** WebSocket connections require sticky sessions. The ingress manifest
includes nginx annotations for cookie-based affinity. Adjust for your ingress
controller.

## Option 3: Build from Source

```bash
cd server
go build -o openrtc-runtime ./cmd/openrtc-runtime
go build -o openrtc-admin ./cmd/openrtc-admin

# Run runtime
export OPENRTC_MODE=cluster
export OPENRTC_NODE_ID=node-1
export OPENRTC_REDIS_URL=redis://localhost:6379/0
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
| `OPENRTC_REDIS_URL` | — | Redis connection URL (required in cluster mode) |
| `OPENRTC_AUTH_ISSUER` | (required) | JWT issuer for client tokens |
| `OPENRTC_AUTH_AUDIENCE` | (required) | JWT audience for client tokens |
| `OPENRTC_AUTH_JWKS_URL` | (required) | JWKS endpoint URL |
| `OPENRTC_ADMIN_AUTH_ISSUER` | — | JWT issuer for admin tokens (optional) |
| `OPENRTC_ADMIN_AUTH_AUDIENCE` | — | JWT audience for admin tokens (optional) |
| `OPENRTC_TENANT_ENFORCE_PREFIX` | `true` | Enforce tenant prefix on room names |
| `OPENRTC_TENANT_SEPARATOR` | `:` | Separator between tenant and room name |
| `OPENRTC_LIMIT_PAYLOAD_MAX_BYTES` | `16384` | Max payload size (bytes) |
| `OPENRTC_LIMIT_ENVELOPE_MAX_BYTES` | `20480` | Max envelope size (bytes) |
| `OPENRTC_LIMIT_ROOMS_PER_CONNECTION` | `50` | Max rooms per WebSocket connection |
| `OPENRTC_LIMIT_EMITS_PER_SECOND` | `100` | Max emits per connection per second |
| `OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH` | `256` | Outbound message queue depth |

## Verifying Your Deployment

```bash
# Health check (should return "ok")
curl http://<runtime-host>:8080/healthz

# Readiness check (returns "ready" when Redis is connected in cluster mode)
curl http://<runtime-host>:8080/readyz

# Prometheus metrics
curl http://<runtime-host>:8080/metrics

# Admin stats
curl -H "Authorization: Bearer <admin-jwt>" http://<admin-host>:8090/v1/stats
```

## Sticky Sessions

WebSocket connections are stateful. A client must hit the same runtime node for
the duration of its session. Configure your load balancer for session affinity:

- **nginx**: `ip_hash` or cookie-based sticky sessions
- **K8s Ingress**: See annotations in `deploy/k8s/ingress.yaml`
- **AWS ALB**: Enable stickiness on target group
- **Cloudflare**: Use session affinity in load balancer settings
