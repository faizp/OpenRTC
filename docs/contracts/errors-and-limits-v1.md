# OpenRTC v1 Error and Limits Contract

This document freezes OpenRTC v1 runtime limits and error semantics for beta compatibility.

## 1. Stable error code catalog

The canonical machine code list is in [`error-codes.v1.json`](./error-codes.v1.json).

Codes:
- `AUTH_INVALID`
- `AUTH_EXPIRED`
- `ROOM_FORBIDDEN`
- `RATE_LIMITED`
- `PAYLOAD_TOO_LARGE`
- `QUEUE_OVERFLOW`
- `BAD_REQUEST`
- `INTERNAL`

## 2. WebSocket close reason mapping

Runtime should close with stable reason text matching code when protocol allows:

- `4001 AUTH_INVALID`
- `4002 AUTH_EXPIRED`
- `4403 ROOM_FORBIDDEN`
- `4408 RATE_LIMITED`
- `4409 PAYLOAD_TOO_LARGE`
- `4410 QUEUE_OVERFLOW`
- `4400 BAD_REQUEST`
- `4500 INTERNAL`

## 3. v1 default limits

- Payload max size: `16 KB`.
- Envelope max size: `20 KB`.
- Max rooms per connection: `50`.
- Max emits per connection: `100/sec`.
- Max outbound queue depth per connection: `256` messages.

## 4. Limit boundary behavior

- Oversized payload/envelope: reject request with `PAYLOAD_TOO_LARGE`; retain socket unless gateway policy chooses close.
- Rooms per connection exceeded: reject join with `BAD_REQUEST` and explanatory message.
- Emit rate exceeded: reject emit with `RATE_LIMITED`; keep session active.
- Outbound queue overflow: close connection with `QUEUE_OVERFLOW` and remove membership/presence records through normal disconnect cleanup.

## 5. Correlation and error payload shape

Errors must include:

- `code` (stable machine code)
- `message` (human-readable detail)
- `request_id` (if source message contained `id`)

This shape applies consistently across WS errors, admin API errors, and SDK error mapping.
