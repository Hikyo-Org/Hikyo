# Issue #355 — central Compose stack owner

Issue: https://github.com/Hikyo-Org/Hikyo/issues/355 (parent #326; audit
finding `F-S20-1`).

**State: implemented and locally verified.** One `composeStack` now owns the
machine-authenticated stack identity, projection, config/state/runtime paths,
and the operations that must consume those values consistently.

## Contract

- `openComposeStack` resolves config, machine target, slug, state directory,
  and runtime directory. It never flushes, fetches, creates local keys, opens a
  snapshot, or writes filesystem state.
- A configless `hikyo run` has no state or runtime directory. Snapshot binding
  and offline flushing are no-ops by construction.
- Render and doctor use the same stack owner. Render refuses an unresolved
  runtime directory; doctor carries that error into `runtime_dir_unresolved`.
- Recovery remains before offline flush, and offline flush remains before every
  machine delivery fetch. Loader-control acknowledgement remains before fetch.
- The locked human-session `run` exception remains outside `composeStack`.
- Contract or migration decisions: none. Generated outputs: none.

## Coverage

- `TestOpenComposeStackNoConfig` covers the configless no-state, no-snapshot,
  no-flush invariant.
- `TestOpenComposeStackRuntimeDirUnresolved` covers doctor reporting versus
  render refusal for an unresolved runtime directory.
- Existing render publication, cursor/snapshot, doctor, sync, offline, human
  session, and golden tests retain command-level coverage.

## Validation

- New stack regressions: 2 passed in `internal/cli`.
- Full `internal/cli` package: 302 passed.
- `go test -p 4 -count=1 ./...`: 3,544 passed in 61 packages.
- Standards review: CLEAN. Spec review: CLEAN.
- Exact-head hosted CI is the remaining PR gate.
