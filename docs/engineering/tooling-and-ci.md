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
- `make coverage`
- `make check`
- `make production-check`

## Production release gate

- `make production-check` is the functionality release gate.
- It runs lint, typecheck, Go statement coverage, TypeScript public value export
  coverage, package tests, and integration tests.
- `OPENRTC_GO_COVERAGE_MIN` defaults to `80.0`.
- `OPENRTC_TS_EXPORT_COVERAGE_MIN` defaults to `90.0`.
- TypeScript export coverage counts public runtime value exports from package
  entrypoints and requires package tests/typecheck files to reference them.

## CI required checks

CI runs these as required status checks on pull requests:
- lint
- typecheck
- test
- coverage
- integration smoke

`make` targets must cover:
- `server`: `gofmt`, `go vet`, `go test`, integration tests
- `sdk-ts`: `pnpm` lint/typecheck/test/integration
- production coverage gates: Go statement coverage and TypeScript public value export coverage

## Branch protections

Default branch must require:
- all quality jobs passing
- at least one code review
- no bypass for security or contract-affecting checks
