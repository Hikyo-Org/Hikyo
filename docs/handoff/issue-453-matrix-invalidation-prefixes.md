# Handoff: #453 matrix invalidation prefixes

Issue: https://github.com/Hikyo-Org/Hikyo/issues/453. Base:
`5543b70eae3f3851247ce34f842e976c60ad02cf`.

## Contract

- `api/keys.ts` owns the project-wide prefixes for values, matrix signals, and
  pending drafts.
- Each full environment query key is built from its project-wide prefix, so a
  prefix rename cannot drift from the corresponding full key.
- Matrix publish uses the three project-wide builders without changing its
  intentional all-environment invalidation behavior.
- Revision restore refreshes values, signals, pending drafts, revision history,
  and revision pins after success.

## Coverage

- Key tests prove each project-wide key equals the first three elements of its
  full environment key.
- Restore-hook coverage proves the exact five cache invalidations.
- Local validation was deferred before the first push because host memory
  pressure was high; pull-request CI is the first full runner.
- Generated outputs: none.
