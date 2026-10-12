# Handoff: #164 generic file synchronization (client-pull atomic rendering)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/164. Base: `338251a` (main,
2026-09-26). Both halves ship in one PR: the server's pull-class file target
and the `hikyo file-sync` client. The browser surface is tracked in #819.

## What landed

- **Client library `internal/filesync`** (no network):
  - `ParseConfig`: strict `hikyo-file-sync.yaml` (KnownFields; `token`,
    `token_file`, `credential` refused at any depth). Absolute, clean
    destination directory; single-segment file names (case-insensitively
    unique, no leading dot); formats `dotenv|json|yaml|raw`; `raw` takes
    exactly one key; mode at most `0640`; `on_removed: refuse|retain|prune`;
    `refresh.mode: oneshot|poll` (no `watch`); snapshot `max_age` may only
    lower 7 d.
  - `RenderAll`/`Render`: dotenv is `internal/dotenv.Encode` (standard grammar,
    byte-exact through `dotenv.Parse`); JSON is a flat string object, sorted,
    no HTML escaping; YAML double-quotes every key and value and escapes C0,
    DEL, NEL, U+2028/2029 and the BOM; raw is the bytes with no newline.
    Refusals are by file and key name (not delivered, presence-only secret, no
    value, NUL, invalid UTF-8 for JSON/YAML, unacknowledged loader-control
    key for dotenv). Any refusal returns no files at all.
  - `Destination`: `os.OpenRoot` on the directory (never created, never a
    symlink, identity re-checked after open), non-blocking `flock` on the
    directory's own descriptor (`ErrBusy`), optional tmpfs requirement
    (Linux statfs; refuses elsewhere). `Publish` writes an immutable
    generation `.hikyo-gen/<stamp>-<rand>/` (each file `O_EXCL` 0600, chown
    and chmod before any byte, fsync; `.complete` last; directory fsync), then
    renames a fresh symlink over `.hikyo-gen/current` (the single commit
    point), then creates missing `<name> -> .hikyo-gen/current/<name>` links,
    prunes or retains dropped names, and collects all but the new and the
    previous generation. `recover` removes torn generations and stale temp
    links. `Intact` re-derives the keyed stamp from the bytes on disk.
- **Server** (migration 00069, both engines): `file_targets` (environment,
  name, service account, principal, generation, last report) and
  `file_target_keys` (membership by key id). Cascades: service account,
  environment and key deletion. Operations `file-target.create|update|delete`
  (`manage-adapters ∧ manage-identities` at project), `file-target.inspect`
  (`manage-adapters`), `file-target.report` (`report-delivery-status` at the
  environment). Audit `file_target.configured|inspected|applied`; a repeat
  report emits nothing. Delivery takes `target=<id>`: a bound principal must
  name its target and is narrowed to the selection before any value is
  opened; an unbound caller naming a target, or a bound one naming another,
  is the uniform 404. The fetch audit record carries `file_target`; the
  response carries `file_target_generation`. API revision 7.
- **CLI**: `hikyo file-target create|list|show|update|delete` and
  `hikyo file-sync render|doctor --config FILE` (help golden, spellings §10).
  The render pass: open and lock the destination, flush pending offline
  records, present the cursor only if the credential fingerprint, config
  digest and an intact destination all match, fetch, render, publish, save the
  sealed snapshot, then the cursor, then report. An unavailable server with
  `offline_serve` renders from the snapshot inside `max_age`, records one
  disclosure per served value first, and never claims "current".
- **Docs**: site page `file-sync.mdx`; proposed amendment banners on
  deployment-adapter, mvp-boundary (declared amendment 9) and permission-model.

## Deviations from the handoff comment, stated

- **Decision 2 (a `file` row in `adapter_targets`)**: not done. Every adapter
  path assumes a provider module, origin, credential, destination id, outbox,
  ledger, move and ceremony; a push-less kind would have needed a refusal in
  each. File targets are their own resource and reuse the #157 key-selection
  resolver. `internal/app/adapter_provider.go` is untouched.
- **Decision 1 (verb)**: a new noun `file-sync` instead of a `files:` section
  in `hikyo-compose.yaml`. The binding is to a server target, not an
  environment plus key ids, and a file host need not run Compose.
- **Decision 3 (per-file renames plus a completion marker)**: replaced with a
  generation directory and one symlink swap, for every binding. Renames alone
  cannot make a set of files change atomically, and the acceptance criterion
  asks for "never a mixed set after crash or cancellation".
- **Decision 5 (credential minted by the target)**: the target binds an
  existing workload service account (`--sa`). Minting inside target creation
  would have composed three separately authorized operations (account, grant,
  mint with its ceremony) into one. Revocation still follows: deleting the
  account deletes the target, and deleting the target is refused while the
  account can authenticate, so the binding can never be removed to widen it.
- **Decision 8 (a new report route plus `target show`)**: done, but the
  report rides the existing `report-delivery-status` atom (a declared,
  proposed amendment) instead of a new one.
- **Test matrix**: the macOS leg rides the existing `scan-xplat` job (a new
  job cannot gate its own PR: the required gate reads
  `scripts/ci/ci-job-registry.json` from the base commit). On `macos-latest`
  it runs `./internal/filesync`, the `FileSync` tests of `./internal/cli`, and
  `TestFileSyncCLISQLite` from `./internal/isolation`. Linux runs the same
  tests, plus the PostgreSQL end-to-end run, in `test_core` and the isolation
  shards. `TestRequireTmpfs` proves the refusal on macOS, where tmpfs cannot
  be detected.
- **Watch mode**: not built. Acceptance criterion 5 is met as `oneshot` and
  `poll`; the resident watcher and `--watch` rows of mvp-boundary § 4 stand
  (declared amendment 9). Continuous refresh is a timer running the client.

## Gotchas for the next reader

- `ListFileTargetKeys` was a JOIN; the predicate analyzer (invariant 08)
  cannot prove it, so keys are read as ids plus the project catalogue and
  joined in Go.
- Any new migration invalidates `internal/buildcompat/development.json`;
  regenerate it against postgres:18 (see the #744 handoff) or every store test
  fails with "embedded migration bytes differ from verified build".
- `fileSyncSleep` is the poll seam; the e2e uses a real 5 s interval.
- A reader that needs a consistent SET must resolve `.hikyo-gen/current`
  once; a per-name reader gets per-file atomicity only across a swap.

## Gates run

`go build ./...`, `go vet ./...`, `go test ./...` with
`HIKYO_TEST_POSTGRES_DSN` against postgres:18 (both engines), sqlc and
oapi-codegen regenerated, `pnpm --dir clients/ts run verify`,
`GOOS=darwin|windows go vet ./internal/filesync`.


## PR #830 reconciliation and review

Merged current main without rewriting signed PR commits. File-target migration
is reserved as 00069 in both engines; API revision is 7. Regenerated API outputs
and the development compatibility declaration from a dedicated scratch database.

Review fixes: compare committed generation stamps before reusing files; reject
reports older than the stored timestamp; reuse one project catalogue across a
list; create stable symlinks atomically without overwriting concurrent foreign
entries. Documentation now distinguishes inspection privileges, secret setup,
offline reporting and failures before/after the publication commit point.
Regression coverage includes stamp-key replacement, foreign-file preservation,
report ordering and independent target selections. Cross-provider review was
skipped by the coordinating agent because quota was unconfirmed; ordinary
adversarial review was performed locally.

## 2026-10-12 implementation follow-up

Rechecked issue #164 against `main` at
`868703c75cb5ee65b073f09b1790d874378dc0f0`. The implementation from #830 is
already present. The current follow-up fixes strict config intake:
`ParseConfig` previously accepted the first YAML document and silently ignored
later documents, including one containing a forbidden credential field. It now
requires end-of-input after the first document and refuses additional or
malformed trailing documents before any destination is opened.

### Cross-platform evidence

[PR #836](https://github.com/Hikyo-Org/Hikyo/pull/836) merged on 2026-09-29 as
`6a1f3bf1e362c1d2e461285a9283a2bd0d23ecd9`. Its
[main push CI run](https://github.com/Hikyo-Org/Hikyo/actions/runs/36527505613)
succeeded. The
[macOS job](https://github.com/Hikyo-Org/Hikyo/actions/runs/36527505613/job/109273725306)
explicitly passed `File synchronization tests`: the filesync package, CLI
`FileSync` tests and `TestFileSyncCLISQLite`. Linux `test_core` and all three
SQLite/PostgreSQL isolation shards also passed. This resolves the missing macOS
CI evidence identified in the issue's 2026-09-28 audit. These are historical
merged-main results, not CI evidence for this follow-up change.

### Owner ratification

On 2026-10-12, Marc explicitly selected "Ratify with a #164-only review waiver"
in the implementation conversation. The approved question named the shipped
design: separate file targets, the `file-sync` CLI, the existing report
permission and one-shot/poll modes with timer-driven refresh. A resident
watcher stays out. The deviations listed above remain part of the record.

Amendment 9 in `docs/adr/mvp-boundary.md` and the related banners in
`deployment-adapter.md` and `permission-model.md` now record that ratification.
The cross-provider review requirement from the locked-ADR amendment procedure
is explicitly waived for this amendment only. This is not a general governance
change and does not claim that cross-provider review ran or passed.

The prior audit's owner and macOS evidence gates are resolved. The initial
local-commit phase is complete. On 2026-10-12, Marc extended authorization to
push and open a PR, wait for PR #891 to merge, then rebase and merge this
follow-up after the required checks pass. Issue closure follows that merge.

### Follow-up validation and review

Validated locally on macOS with Go 1.27.2 and an isolated PostgreSQL 18 server:

- Reproduced the ignored second document before the fix. Regression coverage
  now includes additional config, credential, empty and malformed documents,
  credential-safe errors and valid explicit document endings.
- `go build ./...` and `go vet ./internal/filesync ./internal/cli` passed.
- Final-source `go test -count=1 ./internal/filesync ./internal/cli` passed.
  The file-sync SQLite end-to-end test also passed in the focused run; the
  exhaustive isolation runs include both SQLite and PostgreSQL file-sync flows.
- The broad `go test ./...` run passed every package except `internal/app`
  and `internal/isolation`, which reached the default ten-minute cumulative
  package deadline while executing fixtures. One subsequent isolation batch
  also reached that limit. No assertion failure was reported in those runs.
  All 173 app test entry points then completed in three successful batches;
  all 580 isolation entry points completed in five successful batches. The
  complete inventories were partitioned without dropping tests, and every
  batch kept the ten-minute limit. External-provider tests retained their
  normal environment-gated local skips, including the unconfigured AWS
  Secrets Manager emulator. These are not file-sync acceptance gaps.
- The documentation status check verified all 35 ledger entries;
  `git diff --check` passed and the structural graph was refreshed.
- Standards review: zero remaining findings. Its initial finding, trailing
  YAML parser errors exposing a credential-like anchor name, was reproduced
  and fixed with a static refusal and regression coverage.
- Spec review: zero findings, including the owner-approved ratification and
  the explicitly scoped review waiver. Cross-provider review was waived,
  not performed or reported as passing.

At the end of local validation, no remote CI run, push or merge had been
performed for this follow-up. Remote delivery evidence belongs to the PR;
the historical main-branch evidence above is identified separately. Release
and deployment remain outside this follow-up.
