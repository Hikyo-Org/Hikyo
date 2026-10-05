# Verified code cleanup, 2026-10-05

## Scope and authority

The user approved addressing F01-F16 from the [audit](../reports/code-cleanup-audit-2026-10-05.md), committing, opening a PR, fixing CI and review findings, and merging on green. The [independent verification](../reports/code-cleanup-verification-2026-10-05.md) narrowed F06/F08/F09/F15. These boundaries remain part of the implementation contract.

## Result by finding

| Finding | Implementation | Regression or preserved check |
|---|---|---|
| F01 | Remote settings links navigate to the remote origin; local links use the router | Mounted Matrix, HistoryDrawer and WorkspaceSettingsLink tests |
| F02 | Prerelease dispatch precedes stable archive download | Signed invalid/fresh/cached nightly Apply and executable replaceability tests |
| F03 | Shared Hikyo namespace predicate ignores case | Portable connector child-process and restoration tests |
| F04 | Generator-owned HTTP/error/SSE adaptation preserves integer tokens before Zod | Actual generated runtimes and generated Zod, int64 bounds, malformed JSON and prototype keys |
| F05 | Adapter requests check safe integers and field minimums | Real request wrappers refuse unsafe input before network dispatch |
| F06 | Strict SAN decoding before settlement; reads recover names from a bound signed leaf; display errors cannot undo revocation | Stored corruption, signature/serial/key binding, custody rollback, revoke/audit/CRL and audit-failure rollback tests |
| F07 | Bundle classifier shares transport/contract handling and uncertain apply guidance | Actual panel malformed-successful-response test plus classifier cases |
| F08 | Removed only same-source generated wire comparisons | Generator freshness/negative fixtures and actual router/formula/MFA/audit checks retained |
| F09 | One private canonical proof verifier returns the verified proof | Event proof rejection table and existing store proof tests |
| F10 | Bodyless operations always own the session epoch | Negative typing and in-flight stale-session regression |
| F11-F12 | Removed unused SCIM accounting/argument and corrected comments | Org-scoped session and audit behavior retained |
| F13 | Removed public readiness argument and unused allocation | Operational readiness checker/listener retained |
| F14 | Removed unused BootstrapPending wrapper | Actual bootstrap guards and queries retained |
| F15 | Removed retired execution policy parser and unread fields | Refusal entrypoints, historical formats, journal reads/acks and installed refusal scripts retained |
| F16 | Removed unused helper inputs and updated callers | Challenge/reset/window/dynamic behavior unchanged |

## Parser ownership and review corrections

`clients/ts/scripts/install-json-parser.ts` runs after hey-api generation, adds the owned decoder import, and replaces the three HTTP/error/SSE parser sites. It fails on upstream template drift. Regeneration owns the committed generated files; manual edits are unnecessary.

The decoder first validates native JSON syntax, discarding those rounded values. This also prevents the fallback from accepting unquoted keys. It then uses the JSON parser reviver source token, with the @ungap/raw-json ponyfill providing that context on older browsers. BigNumber reads every original numeric token, including short exponent forms. Exact unsafe integers become bigint; safe integers and ordinary fractions retain the number shape expected by generated schemas. Fractions that would round or underflow to integers and nonfinite values fail before operation validation. Native object construction retains own-property semantics, including literal prototype keys. Operation-specific generated Zod schemas still validate every consumed response. A child-process regression removes native rawJSON support to exercise the browser fallback.

Independent review rejected the first decoder implementation because it could fabricate inherited fields and accept malformed JSON. The replacement has explicit HTTP/SSE regressions for both. Review also found missing phase forwarding in the ordinary apply dialog; the actual panel now verifies refresh-before-retry guidance. Reviewed sensitivity fingerprints were refreshed only for the three changed mutation-capable source files; no guard or sensitivity ownership was weakened.

## Reproducible validation

Run from the repository root, using Node 26.7.0 and pinned pnpm:

```sh
rtk proxy go test -p 2 -count=1 ./...
rtk proxy go run ./internal/authz/gen -check
rtk proxy pnpm --dir clients/ts run verify
rtk proxy pnpm --dir web run typecheck
rtk proxy pnpm --dir web run lint
rtk proxy pnpm --dir web exec vitest run --project unit --maxWorkers=1
rtk proxy pnpm --dir web run e2e
rtk proxy git diff --check
```

Local PostgreSQL-specific cases require `HIKYO_TEST_POSTGRES_DSN`; absent it, those cases skip. Windows child-process execution is covered by the portable test source and the Windows CI lane. Source tests for remote settings verify exact destination ownership; a real two-origin browser flow is separate evidence. CI, review, merge and post-merge status belong to the live PR and must be checked against its exact head.

## Delivery state

Implementation and independent runtime/standards review are complete. Local web validation passed 1,500 unit tests, typecheck and lint; client verification passed 33 tests and regeneration/typecheck. Go vet, generator checks and import formatting passed. Broad Go and browser coverage continues. Qodo review corrections preserve definitive 4xx bundle details and recover corrupt SAN display metadata from an issuer-signed leaf whose serial/public key match the stored record. If both SAN and DER are unreadable, authorized revocation and its audit still commit while the service reports a display error; the HTTP boundary redacts it to 500. Authorization, storage and audit failures still roll back. List remains an explicit failure if a record cannot be trusted; it does not omit records or fabricate empty names. Under concurrent host load, the monolithic isolation package hit its cumulative 10-minute deadline; complete coverage is being rerun as eight disjoint canonical shards without increasing test timeouts. PostgreSQL and Windows execution require their CI lanes. Exact-head CI, reviews and merge status are recorded on [PR #857](https://github.com/Hikyo-Org/Hikyo/pull/857); this document is an implementation snapshot, not a merged claim.

The HTML report remains the pre-fix investigation snapshot. It is served only from the isolated report export when requested; no repository files or credentials are served by that export.
