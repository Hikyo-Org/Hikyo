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
- **Server** (migration 00060, both engines): `file_targets` (environment,
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
  response carries `file_target_generation`. API revision 6.
- **CLI**: `hikyo file-target create|list|show|update|delete` and
  `hikyo file-sync render|doctor --config FILE` (help golden, spellings §10).
  The render pass: open and lock the destination, flush pending offline
  records, present the cursor only if the credential fingerprint, config
  digest and an intact destination all match, fetch, render, publish, save the
  sealed snapshot, then the cursor, then report. An unavailable server with
  `offline_serve` renders from the snapshot inside `max_age`, records one
  disclosure per served value first, and never claims "current".
- **Docs**: site page `file-sync.mdx`; proposed amendment banners on
  deployment-adapter, mvp-boundary (item 5) and permission-model.

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
- **Test matrix**: the macOS leg is not wired. The PR's own required gate
  reads `scripts/ci/ci-job-registry.json` from the base commit, so a new job
  must land separately. The package builds and vets for darwin and windows;
  follow-up: a `macos-latest` job running `go test ./internal/filesync
  ./internal/cli -run FileSync`.

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
