# Contributing

## Required local command contract

Run these commands before opening a PR:

- `make lint`
- `make typecheck`
- `make test`
- `make test-integration`
- `make check`

## Scope and compatibility guardrails

- Go is the only backend runtime implementation language.
- `server/` builds `openrtc-runtime` and `openrtc-admin`.
- SDK packages call public APIs only; they do not own backend routing or cluster logic.
- v1 keeps at-most-once, non-durable semantics.
- Room names must remain tenant-prefixed at server boundary.
- Reconnect semantics remain fresh-session (no resume window).
- Sticky sessions are required for supported cluster behavior.

## Contract-change checklist

If a change touches protocol, admin API, auth, routing, or Redis membership:

1. Update relevant docs under `docs/`.
2. Update schemas and contract tests in the same PR.
3. Document operational impact (logs, metrics, cleanup).
4. Keep backward compatibility unless version bump is planned.
