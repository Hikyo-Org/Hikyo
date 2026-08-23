# Handoff: #362 matrix query state

Issue: https://github.com/Hikyo-Org/Hikyo/issues/362 (parent #326; audit finding
`F-S28-2`). Implementation base: `428dd6a50c26512a634ece820794562241724e2f`.

## Contract

- Each environment query is mapped once at the API boundary to exactly one of
  `pending`, `error`, `stale` with data, or `ready` with data.
- Each matrix row owns one worst-family readiness. Loading retains precedence
  over a simultaneous initial error; otherwise error precedes stale data.
- A stale pending-drafts query now contributes to the background-refresh
  warning, preserving its loaded data while making the refresh failure visible.
- A signal exposes pending work as one optional `{ versionId, operation }`
  value. The wire boundary still refuses either field without the other.

## Coverage

- `matrix.test.ts` covers all four query mappings, row readiness precedence,
  stale pending drafts, signal parsing, and pending-preview lookup.
- `matrix-hook.test.tsx` preserves keyed query ownership through environment
  reorder and removal.
- Generated outputs: none. Wire schemas and generated clients are unchanged.
- Local validation: web typecheck passed; focused matrix tests passed 22/22;
  full web Vitest passed 312/312 after review fixes.
