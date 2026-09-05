# F4 recovery integration working handoff

Current owned implementation lives in `/tmp/hikyo-runtime-fence`. Use these newer files over the original F4 tree.

- `internal/app/backup_upgrade_drill.go`: full authenticated archive, exact source restore, active migration exclusion, opaque ScratchAdmission and RecoveryDB. No ordinary runtime Open.
- `internal/service/recovery.go`: bounded status, one-principal reconciliation, protected-value proof and machine mint/revoke. Private extracted helpers retain actual runtime authorization, policy and audit logic. Ordinary services still take only runtime DB.
- `internal/store/keyring/recovery.go`: explicit recovery transaction adapter, refuses creating missing master hierarchy. Existing purpose creation retains hierarchy generation and active-master checks.
- `internal/store/upgrade/restore_destination.go`, `internal/store/restore_destination.go`, destination wrappers in `internal/store/tx/restore.go`: separate v2 authenticated archive and v1 data-only PostgreSQL import capabilities. Both bind active migration owner, physical configuration, signed source schema and exact migration set. Import locks and checks every expected table before COPY, verifies actual imported source, invalidates credentials/incarnation before commit, and refuses expired owner. V1 makes no receipt or serving claim.
- `internal/app/backup.go`: only PostgreSQL `checkRestorable` emptiness block changed by F4; read-only fresh-schema inspection replaces opening a runtime DB. Parent owns other CLI edits.

## Evidence

Actual both-engine legacy/config-only/private-custody/applied-ledger DrillUpgrade suite passed 19.123s after adapters. New capability and destination negatives plus actual Cosign CLI are running under race; no final pass claim yet. First fixture run named nonexistent control tables; corrected to canonical `upgrade_pending`, `upgrade_nonces`, `upgrade_control`. Fixture seeds through genuine gate, then test-only removes new control tables to model historical pre-ledger source; no runtime access occurs after removal. A compile started during v1 seam edits saw intermediate types; rerun required after source stabilized.

## Decisions

Question: How can a populated pre-ledger source export before runtime admission exists? Options: admit ordinary runtime; narrow export capability. Choice: narrow PreparationDB under owned migration session.

Question: How can scratch recovery prove real service behavior while restored serving remains blocked? Options: temporarily enable runtime; separate RecoveryDB using the same private business rules. Choice: separate capability, with owner/archive expiry checked at transaction admission and commit.

Question: How should ordinary v1 restore coexist with mandatory v2 upgrade proof? Options: infer a receipt from v1; separate data-only import followed by new current-incarnation export/drill and gate evidence. Choice: data-only import. Parent owns final new proof and serving reactivation.

No commit or push. Parent owns combined verification and signed delivery.

## Completed combined validation and native review

- New actual both-engine upgrade/capability/destination/Cosign CLI race: PASS127.428s, required local Cosign configured (no skip).
- Ordinary v1 public-admission and data-only restore actual both engines: race PASS18.199s before parent R1 race fixes.
- Independent recovery adapter review by approval_acceptance: CLEAN; full ordinary CLI integration excluded from that verdict.
- Combined boundary PASS9.741s, lint PASS66.201s. Earlier broad load timed out during concurrent builds; that run was not counted as passed.
- Parent R1 identified schema-initialization race and pre-trust SQLite creation. Both fixed: Session.ApplyRestoreSchema runs the exact source prefix on its owned physical connection after a same-lock fresh-catalog check; public trust and read-only source inspection occur before any WithLock creation. V1 and v2 PG use that same session through import. Added occupied schema43/no-row-change and invalid production trust/no-file regressions. Latest race rerun pending.
