# Security and Observability Standards

## Security

- JWT validation must check issuer, audience, expiry, and signature.
- Tenant prefix enforcement is mandatory at server room boundary.
- Admin API requires scoped service JWT claims.
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
