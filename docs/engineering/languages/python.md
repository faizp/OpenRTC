# Python Engineering Rules

Python is limited to integration SDKs and supporting tools. It must not own backend runtime, cluster state, or server authorization logic.

## Coding rules

- Public functions/classes require type annotations.
- Network calls require explicit timeouts.
- SDK retry behavior is limited to transport-level request failures.
- Preserve machine error code and response metadata in exceptions.

## Required commands

- `uv run ruff check .`
- `uv run mypy src`
- `uv run pytest -q`
