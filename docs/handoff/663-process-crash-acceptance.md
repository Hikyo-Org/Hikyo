# Issue 663 subprocess crash acceptance

## Added acceptance

- `internal/store/upgrade/migrate_session_test.go`: a genuine signed fresh-source route with three embedded SQL migrations. Child runs maintained goose on the owned physical connection, wrapped only by a private test driver that waits after an actual successful COMMIT. Parent sends SIGKILL immediately after committed versions 1 and 2. Independent inspection must observe exactly that goose prefix and exactly that many domain marker rows, unchanged schema-write-started control state, and maintenance. Same source resumes to all three rows and the exact signed target catalog.
- `internal/upgradegate/gate_populated_process_test.go`: external-package tests use the real `app.DrillUpgrade`, avoiding a copied drill implementation or import cycle. Each source boots through a genuinely signed release, gets an admitted human/grant/project plus a sealed protected value, exports an actual age archive, and proves full authenticated restoration, hierarchy readability, existing-secret readability, reconciliation, machine credential mint and revoke. Only the actual drill's output is signed and supplied to the normal gate verifier.
- Populated direct route crashes after SQL completion. A two-hop route crashes after first-hop Healthy and separately after final-hop SchemaApplied. Both engines must retain maintenance, exact route and generation, refuse stale runtime admission and prior binary without changing control state, resume the prescribed binary, preserve the original protected value, and restart without consuming evidence again.
- Existing six-boundary fresh gate process matrix remains intact and is rerun with the new cases.

## Test seam and custody

`Session.wrapMigrationDriver` is an unexported per-session fixture field. It cannot be selected through environment/configuration or another package. Production sessions leave it nil. The wrapper preserves the maintained driver's interfaces and real goose execution; it waits only after driver COMMIT succeeds. No production environment bypass or exported mutation hook was added.

Gate-to-app bridges exist only in `_test.go` and only compose the existing authenticating fixture runner and external-kill helper. Child custody stays in owned mode-0600 files; process arguments contain no DSN, root key, or backup identity. Populated child receives public evidence and a fresh owned ciphertext copy, never the age private identity. Fixture root bytes are local synthetic custody.

## Decisions and scope

Question: mock attestation acceptance to reach populated migration? Options: forged test evidence; actual export/drill followed by maintained signature generation. Choice: real export/drill and normal public verifier.

Question: how to target a particular migration commit? Options: delay/sleep and race the process; test-only wrapper around actual driver COMMIT. Choice: explicit persisted-version checkpoint followed by parent SIGKILL.

The populated route fixture releases deliberately preserve the same domain schema. This isolates route/evidence/health/admission recovery. Separate embedded three-file migrations verify actual individual SQL commit/replay behavior. The new tests do not claim every combination of populated source, schema-changing multihop route, and individual migration boundary has been enumerated.

## Validation

Runner: `/tmp/hikyo-runtime-fence-evidence/crash_acceptance.py`.
Sanitized output and summary: `/tmp/hikyo-runtime-fence-evidence/crash-acceptance-race.jsonl` and `crash-acceptance-race-summary.json`.
Focused actual both-engine race validation passed with zero skips:

- Gate matrix: 18 leaf scenarios (12 existing fresh boundaries plus 6 new populated route crashes), 20 pass events including parent tests. Package completed in 151.921s under concurrent build load. Evidence in `crash-acceptance-race.jsonl`; that combined runner reports failure only because its initial individual-commit PostgreSQL fixture wrapper incorrectly required SQLite's optional `driver.Validator` interface.
- Corrected commit observer preserves optional driver interfaces without inventing PostgreSQL ones. Both-engine individual-commit rerun: 4 leaf scenarios, 7 pass events including parent tests, package 3.827s. Zero failures/skips. Evidence in `migration-commit-race.jsonl` and `migration-commit-race-summary.json`.
- Total completed acceptance: 22 distinct crash/restart scenarios across both engines. All executed real process kills.
- `git diff --check` passed for the owned files. Parent owns final combined validation and delivery.

No commit or push from this worker.
