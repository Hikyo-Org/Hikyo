# #575 and #576: browser definitions and instance directory

Base: `e114b56e2423a0996fa266fafbb59b561712d35d`. Implementation: Codex.
Parent owns combined review, signed commit, PR, exact-head CI and authorized merge.

## Result

Project settings Policy now offers canonical and portable same-origin GET downloads
and a file dialog for check, immutable plan creation/read and atomic apply. The
review shows drift, per-key/environment/group changes, immutable digest/base/expiry,
protected environments, concrete deletion impact and explicit deletion consent.
Apply confirms the exact plan with the existing consequence ceremony and sends its
digest. The server checks publish authorization per environment. Stale, masked
permission, scanning and Git governance refusals render as text. Scanner overrides
reuse the existing redacted, content-bound review dialog.

Git mode allows check/plan only and rechecks governance immediately before apply.
It names the repository CLI mechanism and last applied ref if present; the API has
no repository URL. Files exist only in mounted component state. Closing, changing
files or changing project invalidates pending continuations. Files over 1 MiB,
invalid JSON and unsupported shapes are refused. Integer conversion refuses unsafe
JSON numeric precision instead of silently changing constraints.

Remotes shows This instance using serveDirectory itself, refreshing every 20 seconds
like connected directories. Identity, version, names and counts follow the exact
instance-directory authorization. A refused refresh hides previously cached metadata.
The parity registry now claims six direct browser outcomes, without surrogate verbs.

## Real-browser backend defects closed

1. A normal plan returned `reveal_required: null`, violating the contract and
   preventing generated Zod parsing. Normalize required list fields at the existing
   wire boundary; contract tests now include nil service slices.
2. Plans left otherwise empty projects permanently undeletable. Project deletion
   now removes only the project's own operational plan/provenance ledger under
   its existing proof and transaction. Audit rows remain. A failed nonempty delete
   rolls the plan removal back. SQLC regenerated; no migration or blanket cascade.

## Design decisions

The complete options, recommendations, choices and reasons are in
[`web-parity.html`](../reports/1.0/web-parity.html), with screenshots.
New UI reuses locked settings rows, native centred dialogs, consequence confirmation,
scanner review and Remotes fact/list cards. No prototype fixture was changed.

Definitions apply is structural schema fan-out. Its server path does not consume
value-draft passkey intents, so the browser does not fabricate an unrelated WebAuthn
binding. Secret-to-config transitions still refuse and direct the operator to the
separate key-detail declassification ceremony. This does not weaken publish/reveal
checks or change the API contract.

## Validation

- `web`: `node --run typecheck`; `node --run test` (83 files, 677 tests); `node --run build`.
- `go test ./api`: 100 passed.
- `go test ./internal/server -run TestDefinitions -count=1`: 5 passed.
- Both engines: `go test -p 1 ./internal/isolation -run TestDefinitionsProjectDeletion -count=1` passed.
- Isolation proof, predicate, driver and result-confinement invariants passed.
- Embedded Playwright settings/workspace focused flows: all 6 desktop/mobile passed
  (3.1 minutes), dark/light axe plus dialog/panel overflow checks. Eight screenshots
  are in `docs/reports/1.0/evidence/web-parity/`.
- Independent Standards review: CLEAN after strict JSON numeric input fix;
  focused parser suite 11/11 and final TypeScript check passed.
- Both-engine complete definitions, project deletion and tenant boundary regression subset
  passed (16.579s). `go vet ./internal/store ./internal/server` and SQLC regeneration passed.

PostgreSQL validation used scratch base `hikyo_web_parity`, harness-created sibling
`hikyo_web_parity_isolation`; unrelated application databases were not reset.
The browser harness used disposable real embedded instances on 45789-45795.
Refusal rendering uses controlled HTTP 403/404 fixtures; successful download, check,
plan, apply, stale-base and Git policy paths use the real API.

File parsing rejects numeric coercion before network I/O: revisions and direct/union
min/max require actual JSON integers in the safe-number range. Final UI reset also clears
the native file picker after successful apply, allowing the same filename to be selected again.

Parent Spec/security review: CLEAN after verifying proof-scoped project deletion, preserved audit, immutable plan and per-environment publish semantics, transient file lifetime, Git browser refusal and exact direct parity claims. Parent reran API/server/definitions/isolation invariants on both engines; all passed. Parent visually inspected retained mobile plan and desktop directory screenshots; containment and existing shell primitives match the intended treatment. Final combined five-family prototype proof remains a release gate.

## CodeQL report renderer correction

At candidate `519d1218bb9e0eeb370303dccf21f87d7c5aa122`, CodeQL identified DOM selector values interpolated into `innerHTML` in the prototype comparison report. The renderer now uses `createElement`, `textContent`, DOM property assignment and `replaceChildren`; viewport/theme values are narrowed to the supported choices. No suppression was added.

Chromium validation passed all four viewport/theme combinations, eight sections and sixteen images per combination, all 64 expected image paths, and a hostile injected option value. The hostile value produced neither injected DOM nor script execution; invalid choices used desktop/dark defaults. No page JavaScript errors occurred. This is report-only validation; product web code did not change. Parent retains independent preview/review and exact-head CodeQL confirmation.
