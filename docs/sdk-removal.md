# SDK Removal from Monorepo

**Date:** 2026-03-20
**Decision:** Move SDKs out of the monorepo; they will live in their own repositories.

## What was removed

| Directory | Language | Purpose |
|-----------|----------|---------|
| `sdk-ts/` | TypeScript | WebSocket client + thin admin HTTP SDK |
| `sdk-go/` | Go | Admin HTTP + publisher SDK |
| `sdk-python/` | Python | Admin HTTP + publisher SDK |

## Config and script changes

1. **`go.work`** — Removed `./sdk-go` from the `use` block.
2. **`pnpm-workspace.yaml`** — Removed `sdk-ts` from packages list.
3. **`scripts/lint.sh`** — Removed `sdk-go` gofmt/vet, `sdk-ts` pnpm lint, and `sdk-python` ruff checks.
4. **`scripts/typecheck.sh`** — Removed `sdk-go` go test, `sdk-ts` pnpm typecheck, and `sdk-python` mypy checks.
5. **`scripts/test.sh`** — Removed `sdk-go` go test, `sdk-ts` pnpm test, and `sdk-python` pytest runs.
6. **`scripts/test-integration.sh`** — Removed `sdk-ts` pnpm integration test run and Python placeholder message.

## Rationale

SDKs will be maintained in separate repositories to allow independent versioning, per-language CI pipelines, and cleaner release cycles. The protocol is expected to stabilize after v1, making cross-repo coordination manageable.

## What remains

- `server/` — Go core runtime (unchanged)
- `reference-app/` — Reference application (unchanged)
- `docs/` — Documentation (unchanged)
- All planning files (`plan.md`, `tasks-and-subtasks.md`, etc.) — unchanged
