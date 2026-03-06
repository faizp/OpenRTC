# OpenRTC v1 Runtime Config Contract

This document freezes the v1 runtime configuration surface.

## 1. Schema

Canonical schema: [`openrtc.config.schema.json`](./openrtc.config.schema.json)

## 2. Required settings

Always required:
- `mode`: `single` or `cluster`
- `node_id`
- `auth.issuer`
- `auth.audience`
- `auth.jwks_url`
- `tenant.enforce_prefix`
- `tenant.separator`
- `limits.*`

Required only in cluster mode (`mode=cluster`):
- `redis.url`

## 3. Defaults

- `server.host`: `0.0.0.0`
- `server.port`: `8080`
- `server.ws_path`: `/ws`
- `redis.channel_prefix`: `room:`
- `tenant.enforce_prefix`: `true`
- `tenant.separator`: `:`
- `limits.payload_max_bytes`: `16384`
- `limits.envelope_max_bytes`: `20480`
- `limits.rooms_per_connection`: `50`
- `limits.emits_per_second`: `100`
- `limits.outbound_queue_depth`: `256`

## 4. Compatibility rules

- New config keys after beta must be backward-compatible with default behavior.
- Removing or changing semantics of existing keys requires version bump and migration note.
- Unknown keys should fail validation to prevent silent misconfiguration.
