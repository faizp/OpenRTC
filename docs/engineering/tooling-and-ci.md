# Tooling and CI Standards

## Toolchain baseline

- TypeScript uses `pnpm` workspaces for integration SDK packages.
- Go uses Go modules/workspaces for `server` and `sdk-go`.
- Python uses `uv` with lockfile-driven environments.

## Required command contract

- `make lint`
- `make typecheck`
- `make test`
- `make test-integration`
- `make check`

## CI required checks

CI runs these as required status checks on pull requests:
- lint
- typecheck
- test
- integration smoke

`make` targets must cover:
- `server`: `gofmt`, `go vet`, `go test`, integration tests
- `sdk-go`: `gofmt`, `go vet`, `go test`
- `sdk-ts`: `pnpm` lint/typecheck/test/integration
- `sdk-python`: `ruff`, `mypy`, `pytest`

## Branch protections

Default branch must require:
- all quality jobs passing
- at least one code review
- no bypass for security or contract-affecting checks
