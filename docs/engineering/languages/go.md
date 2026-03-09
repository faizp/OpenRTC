# Go Engineering Rules

Go is the only backend/runtime implementation language for OpenRTC. `server/` owns deployed runtime and admin services. `sdk-go` is a thin integration SDK and must not contain runtime/cluster logic.

## Coding rules

- `context.Context` is the first parameter for request-scoped operations.
- Wrap errors with `%w` for causal chains.
- No unbounded goroutines/channels on hot paths.
- No panics in request handling paths.
- Backend binaries must preserve frozen wire/config/error contracts.

## Required commands

- `gofmt -l .` must return empty output.
- `go vet ./...`
- `go test ./...`
