# 145 - Disaster-recovery hardening (handoff)

Scheduled exports, RPO health, retention pruning, and an operator-verifiable
restore drill for both SQLite and PostgreSQL. Builds on #76 (the age-encrypted
export/restore foundation) without changing the archive format.

## What shipped

- **State**: migration `00037_backup_state` on both engines - one instance row
  (`id = 1`, like `credential_policy`/`retention_runtime`) holding the latest
  export success/failure, the latest prune, and the latest restore drill.
  sqlc queries + `store.BackupStateRepo`/`BackupStateReader`, plus a
  `values.SampleSecretEntry` instance-scoped read for the drill's decrypt proof.
- **Config** (`internal/config`): `HIKYO_BACKUP_INTERVAL` (24h, min 1h),
  `HIKYO_BACKUP_RPO` (26h, >= interval), `HIKYO_BACKUP_RETAIN_COUNT` (7, min 1),
  `HIKYO_BACKUP_RETAIN_DAYS` (180, max 180), `HIKYO_BACKUP_RTO_TARGET` (30m).
  `Config.BackupScheduled()` is true iff recipients + dir are set. A knob
  outside its range, or a schedule knob with no export policy, is a startup
  error (fail fast). Catalogued in `docs/spec/ops-catalogue.md`.
- **Scheduler jobs** (`internal/app/backup_jobs.go`): `backup_export` and
  `backup_prune`, registered on the existing scheduler only when
  `BackupScheduled()`. Export self-gates on the persisted last success
  (`Backup.Due`), so an hourly tick with a 24h interval exports once/day and a
  slept-through box catches up on boot. Failure records `backup.export_failed`
  + the health row and never falls back to plaintext. Under HA the scheduler's
  lease already makes these singleton (#146).
- **Service** (`internal/service/backup_dr.go`): `TriggerScheduled`,
  `RecordFailure`, `Prune` (+ pure `planPrune`), `ProveValuesReadable`,
  `RecordDrill`, and `BackupHealth`/`backupHealth` folded into
  `service.PruneHealth` so the one operator read carries both.
- **Health surface**: additive `backup` object on `GET
  /instance/retention-health` (no new route, no `pinnedContractSurface`
  change); five label-free `/metrics` gauges; `hikyo doctor` `backup-rpo`
  (error when exceeded) and `restore-drill` (warn when stale/never) findings.
- **Drill verb**: `hikyo restore drill --from ARCHIVE (--identity-file |
  --passphrase-file) --root-key-file PATH --principal ID --project ORG/PROJECT
  (--target-sqlite PATH | --target-postgres-dsn-file PATH) [--cleanup] [-o json]`.
  Restores into an empty scratch target, boots it under the separately supplied
  root key, decrypts one secret, reconciles one human, mints+revokes a
  credential, records the verdict on the LIVE instance. Root key and identity
  are file-only, zeroed after use, absent from output and the audit payload.
- **Audit**: `backup.export_failed` (scheduler-site emitter) and
  `restore.drill_completed` (on the `cli:restore` wire entry).

## Deviations from the ticket handoff (for disposition)

1. **`/healthz` stays liveness-only.** The ticket's implementation notes said
   `/healthz` should 503 on RPO breach; ops-spec section 10 defines `/healthz`
   as "process alive", and the notes themselves say the ADR wins. A stale
   backup restarting the pod is a restart loop that fixes nothing. RPO breach
   surfaces in the health response, the `hikyo_backup_rpo_exceeded` metric, and
   a `doctor` **error** (doctor exits refused, so a cron check fails). This is
   the "health fails when RPO is exceeded" acceptance criterion, minus the LB
   kill.
2. **Prune applies one count/age rule to every complete archive.** The archive
   filename (`hikyo-<engine>-<ts>.age`) carries no trigger, so the ticket's
   "pre-migration artifacts keep the `count > 3` rule" is unimplementable
   without decrypting or cross-referencing the audit trail. All complete
   archives share the `RETAIN_COUNT` / `RETAIN_DAYS` policy; the newest is
   never deleted regardless of age.
3. **Migration is `00037`, not the ticket's `00033`** (00033-00036 landed
   first). **Off-host destination is a mount** (`HIKYO_BACKUP_DIR`), not a
   native object-store client, per ops-spec section 2 - no ADR amendment.

## Gates

- `go build ./... && go vet ./...`; gofmt clean under `$(go env GOROOT)/bin/gofmt`
  (a stale PATH gofmt gives false drift on `config_test.go` - use the GOROOT one).
- `go test ./...` with `HIKYO_TEST_POSTGRES_DSN` set (sqlite-only is blind to PG).
- sqlc + oapi-codegen diff-clean; TS client regenerated; web typecheck + vitest.
- Isolation pins updated: `annotated_queries.json`,
  `TestInvariant11SystemProofEnumeration` (scheduler op set widened, reviewed),
  metric registry (`conformance/metrics_test.go`), backup lifecycle emitters in
  `audit_e2e`/`backup_drill_e2e`.
- Codex adversarial review (R1-R3) is the standing blocking gate before merge.

## Tests

- `internal/service/backup_dr_test.go`: prune planning (keep-newest,
  oldest-first, order-by-name, other-engine/`.partial`/foreign untouched),
  health verdicts.
- `internal/app/backup_jobs_test.go`: export catch-up + interval gating,
  loud+durable failure with no plaintext, prune, schedule guard.
- `internal/config/config_test.go`: knob defaults, bounds, policy-less refusal.
- `internal/cli/doctor_internal_test.go`: RPO/drill findings + refused exit.
- `internal/isolation/backup_drill_e2e_test.go`: `TestRestoreDrillVerb{SQLite,
  Postgres}` drive the real verb; audit lifecycle emits the two new events.
- Metrics contract + conformance pins for the five DR gauges.
