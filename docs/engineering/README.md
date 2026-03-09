# OpenRTC Engineering Handbook

## Purpose

This handbook defines enforceable engineering standards for OpenRTC across TypeScript, Go, and Python.

## Non-negotiable defaults

- Go owns backend/runtime code under `server/`.
- TypeScript and Python packages are integration SDKs only.
- Reliability-first: explicit timeouts, bounded resources, no hidden retries.
- Contract-first: schema-versioned wire/admin/SDK boundaries.
- Structured logs and machine error codes.
- Backward-compatible protocol/config changes after beta unless version bump is approved.

## Language guides

- [TypeScript](./languages/typescript.md)
- [Go](./languages/go.md)
- [Python](./languages/python.md)

## Policy docs

- [Dependency Policy](./dependency-policy.md)
- [Tooling and CI](./tooling-and-ci.md)
- [Security and Observability](./security-and-observability.md)

## Templates

- [Dependency Request](./templates/dependency-request.md)
- [Design Review Checklist](./templates/design-review-checklist.md)
