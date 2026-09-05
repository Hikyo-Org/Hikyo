# Handoff: #638 legacy remote updater retirement

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/638>, native review findings 1
and 5. Base: `f4175a5d`. Implementation: Codex. This is the urgent retirement
slice, not completion of the signed-upgrade compatibility design. Parent 1.0
work owns ADR/governance disposition, review, signed commit, PR, CI, and merge.

## Why this change exists

Legacy `Executor.Execute` automatically invoked rollback after apply or health
failure. Platform scripts restored the database without a durable,
datastore-owned proof that schema writes had not started. Flux additionally
committed and pushed through ambient Git authority. Warnings or a UI-only
disable would leave direct helper calls and queued work able to execute.

## Result

- Config loading and direct app boot reject nonempty `HIKYO_UPDATER_SOCKET`
  with `updater.ErrRemoteApplyDisabled`. There is no enablement override.
- Release status always reports `apply_supported=false` and the safety reason.
  Apply still requires instance-config, human/MFA authorization, and fresh
  authentication, then atomically records intent and a failed disabled outcome
  before returning HTTP 409. It never contacts a helper. The audit backend enum
  gains only `disabled`; existing backend values and public job schemas remain.
- Helper startup refuses before reading config or touching its socket/journal.
  Protocol capability/submission, executor, and direct command runner refuse
  independently. Compose, systemd, Flux, and the common shell library are
  executable refusal stubs, including for rollback. Removed unreachable
  startup custody code and unsafe phase/rollback implementation.
- The web client preserves release metadata from older remotes while suppressing
  their legacy apply capability. Its mutation hook refuses before network I/O.
  Historical job rendering, polling, journal reads, and outcome acknowledgement
  remain compatible. No existing journal is rewritten by helper startup.
- Operator docs remove helper-install instructions, explain the refusal and
  required shutdown/removal steps, and retain manual signed-artifact and
  maintenance procedures. OpenAPI descriptions and generated comments match.

## Delegated choices and limits

The user authorized recommended choices without questions. The complete
question/options/reason record is in
[`docs/reports/1.0/upgrade-safety.html`](../reports/1.0/upgrade-safety.html).

Retirement was selected over opt-in warnings or an unproven replacement.
Configured enablement fails loudly rather than silently disappearing. Legacy
journals remain evidence rather than being replayed or declared recovered.
Refused requests preserve their audit obligations using a truthful disabled
backend. Older remote advertisements do not re-enable unsafe apply in this UI.

Already running old helper processes or spawned platform commands are outside
the reach of a new binary on disk. Operators must stop and disable those
processes before installation, preserve journals/logs, inspect any interrupted
apply, and remove `HIKYO_UPDATER_SOCKET`. Nothing in this patch proves rollback
safe after a legacy attempt.

This patch does **not** implement signed source/target compatibility, durable
migration phase state, HA writer generation fencing, a readable-backup receipt,
nightly/revoked-key recovery trust, or a replacement Flux controller. It does
not change current automatic-migration behavior. Existing CLI release checks
and in-process verified downloads remain available. No ADR is edited here.

## Verification and changed test obligations

Replaced retired executor success/automatic-rollback/phase-timeout/progress
tests with refusal across all three backends and stable/nightly/dev requests,
direct command-runner no-process checks, and no-side-effect execution of every
phase in all four shell files. Replaced socket setup and Unix enqueue success
with startup/protocol refusal while preserving existing journal bytes and
historical outcome reads/acknowledgement. Service enqueue success now proves
audited refusal with both configured and absent controls. Existing stale-auth,
historical outcome reconciliation/retry, journal integrity, and runtime audit
emitter gates remain.

Passed locally with `GOMAXPROCS=2`, `-p 1`, and Node 26.7.0:

```sh
go test -p 1 ./internal/config ./internal/updater ./internal/service ./internal/server ./internal/app \
  -run 'Test(Update|Executor|DirectLegacy|HelperStartup|UnixControl|Journal|LoadConfig|ExampleProfiles|Retired|InstanceUpdate|Boot)' -count=1
go test -p 1 ./internal/isolation -run '^TestAuditCore$' -count=1
```

Results: 70 targeted Go tests; 31 runtime audit tests/subtests on both SQLite
and PostgreSQL. Scratch PostgreSQL base `hikyo_638` derives
`hikyo_638_isolation`; no existing application schema was reset.

Web `node --run typecheck` and full `node --run test`: 82 files, 666 tests.
Generated client typecheck and 13 tests passed. Both dependency lockfiles were
installed without changes. API comments were regenerated with the pinned Go
and TypeScript generators; no API shape changed.

Full `./api`, `./internal/audit`, `./internal/updater`, and `./internal/config`
package checks passed. Scoped vet and `git diff --check` passed. API parity's
retired apply row now names the unsupported outcome delivered via
`getUpdateStatus`; no new exception class or disabled check was introduced.
The preserved CLI update, release discovery, and verified-download regression
subset passed (20 tests across `internal/cli`, `internal/updatecheck`, and
`internal/selfupdate`).
Parent retains final combined review and exact-head CI.

Parent targeted Go verification passed independently. Spec review found exported protocol Client.Submit/Capability could still contact an old helper. Both now refuse locally; a compatible legacy transport regression proves zero requests while journal reads/acknowledgement remain. Full updater suite passed. Final Spec/security review CLEAN; parent Standards/security review CLEAN.
