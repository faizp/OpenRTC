# OpenRTC v1 Environment Variable Mapping

Environment variables map into the config contract as follows:

- `OPENRTC_MODE` -> `mode`
- `OPENRTC_NODE_ID` -> `node_id`
- `OPENRTC_SERVER_HOST` -> `server.host`
- `OPENRTC_SERVER_PORT` -> `server.port`
- `OPENRTC_WS_PATH` -> `server.ws_path`
- `OPENRTC_REDIS_URL` -> `redis.url`
- `OPENRTC_REDIS_CHANNEL_PREFIX` -> `redis.channel_prefix`
- `OPENRTC_AUTH_ISSUER` -> `auth.issuer`
- `OPENRTC_AUTH_AUDIENCE` -> `auth.audience`
- `OPENRTC_AUTH_JWKS_URL` -> `auth.jwks_url`
- `OPENRTC_ADMIN_AUTH_ISSUER` -> `admin_auth.issuer`
- `OPENRTC_ADMIN_AUTH_AUDIENCE` -> `admin_auth.audience`
- `OPENRTC_TENANT_ENFORCE_PREFIX` -> `tenant.enforce_prefix`
- `OPENRTC_TENANT_SEPARATOR` -> `tenant.separator`
- `OPENRTC_LIMIT_PAYLOAD_MAX_BYTES` -> `limits.payload_max_bytes`
- `OPENRTC_LIMIT_ENVELOPE_MAX_BYTES` -> `limits.envelope_max_bytes`
- `OPENRTC_LIMIT_ROOMS_PER_CONNECTION` -> `limits.rooms_per_connection`
- `OPENRTC_LIMIT_EMITS_PER_SECOND` -> `limits.emits_per_second`
- `OPENRTC_LIMIT_OUTBOUND_QUEUE_DEPTH` -> `limits.outbound_queue_depth`

## Parsing rules

- Booleans accept only `true` or `false` (case-insensitive).
- Integer fields must parse to base-10 positive integers.
- Empty string values are treated as missing.
- `mode=cluster` without `OPENRTC_REDIS_URL` is invalid.
