# Design Review Checklist

## Scope

- Milestone/task mapping documented.
- v1 vs v2 scope guard confirmed.

## Contract safety

- Protocol/admin/SDK schema changes documented.
- Backward compatibility impact documented.
- Error code and correlation behavior preserved.

## Risk zones

- Auth changes reviewed (iss/aud/exp/signature).
- Routing and room authorization changes reviewed.
- Redis membership/presence cleanup impact reviewed.
- Queue/bounds/timeout behavior reviewed.

## Operational readiness

- Logs/metrics/traces added for changed paths.
- Health/readiness behavior reviewed.
- Test plan updated for affected layers.
