# TypeScript Engineering Rules

TypeScript is limited to integration SDKs and supporting tools. It must not own backend runtime, cluster state, or server authorization logic.

## Compiler baseline

`tsconfig` must keep:
- `strict: true`
- `noUncheckedIndexedAccess: true`
- `exactOptionalPropertyTypes: true`

## Coding rules

- No `any` in exported/public APIs.
- Validate untrusted input at all boundaries.
- I/O APIs must accept cancellation and explicit timeout options.
- Preserve stable machine error codes in SDK outputs.
- Treat REST and WebSocket endpoints as the only backend integration boundaries.

## Required commands

- `pnpm -r --if-present lint`
- `pnpm -r --if-present typecheck`
- `pnpm -r --if-present test`
- `pnpm -r --if-present test:integration`
