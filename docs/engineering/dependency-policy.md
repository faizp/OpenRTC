# Dependency Policy

## Intake model

OpenRTC uses a curated allowlist with explicit review for new third-party packages.

Allowed concern categories:
- Auth/JWT/OIDC
- Redis client
- HTTP client
- Validation/serialization
- Metrics/tracing

## Admission criteria

Every new dependency must include:
1. Active maintenance evidence.
2. Apache-2.0-compatible license.
3. Security posture check.
4. Pinned major version.
5. Lockfile update.
6. Completed dependency request template.

## Deny-by-default cases

- Duplicate libraries for the same concern without approved exception.
- Transitive-heavy package with weak operational benefit.
