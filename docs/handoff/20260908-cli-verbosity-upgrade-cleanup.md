# CLI verbosity and upgrade disk cleanup

> Historical local artifact names below identify the original run only and are not included in this checkout. Recorded results were not rerun for this path cleanup.

## Continuation

Requested in T3 thread `52d45fbe-e5ba-4f2e-a1b3-cd4018719013`, continued on branch
`t3code/verbose-cli-upgrade-cleanup`. The previous thread left signed commit
`274f9c1b` on `t3code/show-hikyo-upgrade-progress`; this branch fast-forwarded to
that exact commit before extending it.

The operator supplied evidence of a 7.8 GiB root filesystem with 467 MiB free:
4.0 GiB in `hikyo-upgrader/downloads`, 1.5 GiB in public `hikyo-upgrade`, and
230 MiB of staged candidates. Existing code retained downloads and successive
route bundles, and copied complete cache hits into scratch for re-verification.
The inherited fix only pruned downloads after success and missed candidate
cleanup on an already-installed result.

## Behavior

Global `-v`, `-vv`, `-vvv` and repeated `--verbose` provide phases, artifact/HTTP
metadata, and timings. Put verbosity before other options: the conservative
parser preserves possible flag values, including a literal `-v`. Everything
after `--` is untouched. Diagnostics use stderr and do not print raw arguments,
environments, request paths, query strings, credentials, headers or bodies.
Levels propagate through the verified coordinator handoff and host operator
subprocess. Help and machine version output remain quiet.

Before downloads, under the operator lock, the coordinator prunes obsolete
private cache work. Completed journals permit pruning obsolete public artifacts
and candidates. Incomplete journals retain every verified release and executable
needed by the route. Ordinary failures clean disposable cache work too, retaining
public recovery material. Invalid trust state fails closed before deletion.
The latest observed release and any explicitly retained target survive cache
pruning, along with unchanged trust floors and the lock inode.

Cache hits verify existing files in place. Automatic-only transient bundle mode
removes superseded private bundles before assembling their replacement; manual
staging keeps its published paths. Generated names are constrained, and cache
removal uses an opened filesystem root. Unpublished public bundles are cleaned
if preparation fails before journal/runtime ownership. Successful/no-op runs
prune candidates. A public cleanup failure after durable completion reports an
error without fencing the healthy service; the next invocation retries cleanup.

Directory names and ownership remain compatible. The Upgrades docs explain
runtime data, installation custody, public evidence, candidates and root-only
coordinator state. Full signed inventories still require temporary disk space;
this is not a change to the signed artifact format or a platform-only download
optimization. No live host was inspected or modified by this continuation.

## Validation

Targeted signed nightly, migration route, cleanup, and CLI regressions cover
cache tampering, trust-state retention, busy locks, symlink confinement,
interrupted route retention, no-download cache retry, transient/manual bundle
lifetimes, stderr/JSON separation, passthrough arguments, help and handoff flags.
The isolated root Linux adapter suite also exercises candidate pruning.

Final command results and review disposition are recorded at completion below.

Completed local checks:

- Full Go suites: `internal/app`, `internal/selfupdate`, `internal/hostupgrade`,
  `internal/diagnostics`, `internal/cli`, `cmd/hikyo` passed. App suite took 134 s.
- `go vet` for those six packages passed; `internal/lint` suite passed.
- Targeted race tests for cache pruning, route assembly, completed-service
  cleanup failures and diagnostic isolation passed.
- `scripts/ci/test-host-upgrade.sh` passed in its isolated root Linux container.
  Its real-systemd-only case is intentionally skipped outside the separate
  real-systemd container; no service lifecycle implementation changed here.
- Docs `check` reported zero errors/warnings, and docs build produced 45 pages.
- Independent review of the final parent cleanup changes returned CLEAN.

Local check logs use `hikyo-verbose-*.log`. This is source-worktree evidence,
not a published nightly or a live host cleanup. No push, merge or deployment has
been performed by this continuation.

Native Codex review completed in three rounds. R1 found four inherited issues:
late cleanup, missed no-op candidates, broad/incomplete cache-name matching, and
broad candidate-name matching. All were fixed. R2 found a new public-bundle leak
when journal publication fails. The final fix checks the persisted journal before
removing a newly staged bundle, distinguishing pre-rename failure from a
post-rename sync failure and preserving data if the journal cannot be read.
Four filesystem-state regressions cover that boundary. R3 returned `CLEAN`.
Review artifacts: `hikyo-verbose-inherited-review-r{1,2,3}.md`.

After the final fix, all automatic-upgrade tests and main verbosity/help tests
passed again (app 46 s, command 2 s). Candidate-name preservation was also
rechecked in the isolated root Linux suite. Final `git diff --check` passed.
The new continuation edits remain uncommitted on top of `274f9c1b`.

## Follow-up: verbosity across command families

The user accepted extending the same diagnostics beyond the upgrader. Added
phase/detail/timing instrumentation to ordinary backup export, restore,
reconciliation and drills; signed upgrade exports/drills; source import,
wizard/replay and artifact/dotenv value application; schema migration; and
server bootstrap, datastore/keyring, managed configuration, providers and
listeners. No new verbosity flags or alternate level meanings were introduced.

Snapshot export and encryption are reported as one streaming operation. Source
import completion states that review artifacts were created and values have not
been applied. Migration emits its applying-SQL phase only when SQL is actually
applied. Server socket preparation and serving readiness are distinct;
maintenance readiness identifies the restricted operational listener.

New details are static phase names and explicit counts/validated enums. They
exclude root and backup keys, recipients, values, secret names, DSNs, paths and
unfiltered errors. Existing command-result and error formats remain unchanged.
The CLI reference documents levels and examples; backup docs link to it.

Focused tests exercise real backup/restore and signed drill fixtures, JSON
export and import, levels 0 through 3, secret sentinels, truncated archives,
parse/apply failures, server readiness/listener failure, migration refusal and
no-op migration. Expanded main-entrypoint tests keep server, migrate, backup
and import help free of diagnostics. Startup/migration independent review was
CLEAN; parent reviewed backup/import changes and their combined contracts.

Follow-up final validation: full `internal/app`, `internal/upgradegate`,
`internal/cli`, `internal/diagnostics`, `cmd/hikyo` and `internal/lint` suites
passed. `go vet` on the five implementation packages passed. Targeted race tests
for startup/migration, backup/restore/drills, source import and value import
passed (app 152 s, CLI 18 s). Docs check reported zero errors/warnings; docs build
produced 45 pages. Final whitespace check passed. All continuation work remains
local and uncommitted; no push, merge, deployment or live-host change.
