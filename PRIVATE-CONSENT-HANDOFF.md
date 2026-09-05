# PRIVATE: consent remediation handoff

This record belongs to the private remediation branch. Do not publish before coordinated release. No credentials, raw traces or advisory bodies are included.

Base `33a213ca80bfff4ffb4a808cf916f209fd90138d`. Source/test patch SHA256 `e6a373cd88f65b4145ff536d7dee5a99bf583515ca3639784b0ce0046dd72404` covers 14 source files and excludes this handoff and sibling evidence. `consent-source.json` records individual source hashes and the embedded binary hash. Production/web inputs were frozen before final browser execution; only the server contract test fixture changed afterward.

## Behavior and authority

- Both authenticated summary variants project the stored transaction origin. URL origin/callback fields remain ignored. Consent shows exact origin, operation, environment and every key. Existing allowlist, PKCE, admission and atomic approval/consume authority remain unchanged.
- Missing, malformed, mismatched, consumed and expired summaries fail closed. A local expiry timer removes controls; immediately before approval, a fresh summary must match all displayed immutable fields.
- Existing task identity plus synchronous AuthProvider transition revision fence the actual POST and subsequent redirect. Unmount, changed session and late reauth cannot continue the old operation. Real AuthProvider deferred GET/POST tests exercise both retirement points.
- Passkey reauth receives the exact generated six-operation enum. Submitted TOTP input clears before dispatch and after settlement, is disabled while pending, and the child is keyed to the session epoch.
- Long origin wraps in full; all keys remain in a bounded keyboard-scrollable region. Actual mobile/desktop controls remain reachable.

## Local evidence

- `consent-baseline-expiry.log`: original HEAD component reproducibly offered approval for an expired summary.
- `consent-web-unit-final.log`: **712 tests / 87 files passed**, including 25 consent cases. `consent-web-typecheck-final.log`: passed.
- `consent-client-test-final.log`: **20 passed**, zero skips. Client typecheck and canonical generation passed.
- `consent-backend-race.jsonl`: **21 named / 7 top-level passes**, both SQLite and PostgreSQL, zero skips/failures, **42.514 s**. Includes two-origin/punycode, tampering, expiry, atomic approval/consume and existing scoped step-up tests.
- `consent-server-full.jsonl`: **284 named passes**, zero skips/failures, **1.05 s**. One stale stub required the new origin value; final test additionally asserts exact wire origin.
- `consent-go-vet.log`: service/server/isolation passed. `consent-browser-final.log`: **6 passed**, desktop/mobile. Genuine two-instance popup + both kill switches passed. Long-origin/200-key and expired-summary cases use explicit metadata interception against the actual authenticated embedded app. These are distinct evidence scopes.
- Independent R2 and parent source review CLEAN. No source-bound official release or hosted combined-candidate pass is claimed. Parent owns private integration and delivery.

Evidence remains in the owner-private parent directory, including `consent-proof.json`, `consent-source.json` and `consent-report.html`. Browser app ports closed after teardown. Shared private PostgreSQL remains for parent integration; no active consent processes. Do not commit raw browser traces or synthetic credentials.

Preserved screenshots: `consent-evidence/consent-full-scope-desktop.png` and `consent-evidence/consent-full-scope-mobile.png`, owner-private outside the worktree.

## Combined schema fixture follow-up

The frozen combined candidate `16e49dfa9f284f443ba513bc2538f7e686660a23` exposed two legacy schema-test payloads lacking the now-required `requesting_origin`. This correction changes only `api/spec_test.go`: valid fixtures include a canonical origin; both branches explicitly require it and reject missing or empty origin. Existing operation/environment negative cases retain otherwise-valid payloads. Source-only test diff SHA256 `67e30a468e32591edef08d79b0ca125dd1ab053d22481a82b84df5affa647296`.

`consent-api-fixture-repair.jsonl`: full `go test -json ./api/... -count=1` passed **113 named tests**, zero failed/skipped named tests, API package **1.825 s**; generated `api/apigen` reports no test files. `consent-api-fixture-generation.log`: canonical Go/TypeScript generation left **7 generated files byte-identical**. Diff check passed. The exact failed combined-core log remains intact in `combined-local-go-core-qn8c5id6/04.log`. No integration-worktree mutation or production change.

## Ordered browser fixture isolation follow-up

The combined desktop run exposed three fixture causes beyond the separately repaired recovery display: missing origin in visual consent metadata; an extra project read grant left by the matrix reviewer; and an enabled development approval policy left before the blind publish test. Exact failed evidence remains `combined-local-browser-13k12qbr/01.log`; the publish trace confirmed HTTP202 open approval summary, not a product schema regression.

The two-file test correction on `c79dcc8572e63a014fe6b3d05536bd6dcc69ef5c` has patch SHA256 `9062284abb6ddd05c67073c448fbd3ea79e85eaef7522d8280d529df34d651e2`. Workspace visual metadata now satisfies the generated establishment type and asserts exact visible origin. Matrix approval cases use a per-test finally fixture to restore only their development policy's mutable configuration (or delete their exact newly created ID), read back equality, and revoke only the fresh reviewer's exact project read/publish grants. No broad deletion or weaker schema/inspector/approval assertions. Version/audit metadata remains monotonic through supported APIs.

`consent-browser-fixture-ordered.log`: **16/16 passed**, **3.0 minutes**, desktop/mobile, each in matrix approvals -> members inspector -> blind publish/readback -> workspace dark/light order. `consent-browser-fixture-web-unit.log`: **712 tests /87 files passed**; typecheck and diff check passed. Runtime reused the previously validated `consent-ui`; this patch changes tests only. Default fresh-fixture execution covers newly created policy cleanup; the prior-policy restore branch is source reviewed, not separately seeded here. Filtered execution does not claim full registry closure or combined security acceptance.

`consent-browser-fixture-proof.json` records provenance and cleanup. All owned46801-46806 browser ports closed. No raw traces or credentials belong in the private commit.
