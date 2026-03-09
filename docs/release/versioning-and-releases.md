# OpenRTC Versioning and Release Strategy

## Versioning model

- Versioning follows SemVer (`MAJOR.MINOR.PATCH`).
- v1 beta tags use prerelease suffixes (`v1.0.0-beta.N`).
- Protocol and config compatibility are guaranteed for beta+ unless a major version changes.

## Tagging and changelog automation

- Release PR and changelog generation are handled by `release-please`.
- Final releases are cut from versioned release PRs on `main`.
- Git tags are the source of truth for release artifacts.

## Commit and PR expectations

- Use Conventional Commit prefixes for predictable release notes:
  - `feat:` user-visible capability
  - `fix:` correctness or reliability fix
  - `docs:` documentation-only updates
  - `chore:` internal non-user-visible change

## Release checklist (M1 baseline)

1. `make check` passes in CI.
2. Protocol/config changes include contract docs and schema updates.
3. Error/limits changes include migration notes.
4. `CHANGELOG.md` is updated by automation before tag cut.
5. Go backend image builds once and can run both `openrtc-runtime` and `openrtc-admin`.
6. Release notes call out any backend/runtime contract change separately from SDK-only changes.
