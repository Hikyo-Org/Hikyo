# Local developer scripts

Fast local feedback for the checks CI actually fails on. Not a full CI mirror —
`act` was assessed and rejected (its runner image differs from `ubuntu-latest`,
`no-egress`/`k8s-e2e`/service jobs don't work under nested Docker, and a full
run is slower than real CI while giving less trustworthy results).

## `fast-feedback.sh` - optional local feedback

Run `scripts/ci/fast-feedback.sh` from a clean checkout before a push, or add
`--working-tree` to explicitly check local edits. Set `FAST_FEEDBACK_BASE` and
`FAST_FEEDBACK_HEAD` to name the comparison base and checked-out revision;
otherwise they default to `hikyo-upstream/main` and `HEAD`. Both refs must exist
and share history. The command prints their resolved IDs, and committed mode
refuses a dirty checkout or a different checked-out revision. `--plan` emits
the selected domains as JSON, without running checks. Its eight domain booleans
retain their existing meanings, including Go scope filtering. The additive
`lanes` object describes all three hosted lanes independently of the requested
local lane/scope: each has its adapter `lane` and `goScope`, ordered `checks`
(stable IDs with prerequisite lists), and the union of its `prerequisites` as
booleans. Planning works before Go, Node, pnpm or jq setup, including on Bash 3.2.
The shared `scripts/ci/feedback-plan.sh` module owns this selection, ordering,
lane placement and prerequisite policy. Hosted setup consumes its prerequisite
fields; the local driver dispatches its check IDs through explicit commands.
CI loads the module from the immutable feedback tooling checkout, never from
the source checkout being examined. These plans describe developer feedback;
the full candidate classifier, queue planner and coverage contract are separate.

The profile checks range whitespace, formatting of changed Go files, compilation
of their owning packages including tests (without running them), pinned
ShellCheck 0.11.0 for changed shell scripts/hooks, and the repository's pinned
Actionlint wrapper for workflow changes. Web changes run the package's existing
typecheck, lint, and unit tests; client changes run client typecheck/tests and
the web checks that consume the generated client. API schema and Node/pnpm
manifest changes also select these frontend checks. Go file deletions select
their owning package when it still contains Go files. Deleting its final Go file
leaves no owner to compile, and unchanged importers are not selected; they can
fail to compile while fast feedback passes. Reverse import dependencies and
complete package deletions are validated by the mandatory full candidate suite.
The default branch's complete CI classifier and inventory still own the full
merge coverage contract.

Changes to documentation policy inputs, docs site files, the upgrade script,
and Node/pnpm configuration also run the docs preparation validator in read-only
mode. It catches unmapped repository links and malformed comments without
generating pages or requiring docs dependencies. CI uses the validator from
its immutable tooling checkout with `--check --source-root` pointing at the
exact checked-out PR head. The full candidate still builds and tests the site.

Go dependency manifest changes select `go mod verify` for cached dependency
integrity; complete dependency compatibility remains part of candidate CI.
Changed Go packages compile together in one build graph, including packages
with the same basename. Compiling them can be expensive from a cold cache; it checks
buildability, and does not establish that any test assertion passed. Database
fixtures and `TestMain` are never executed by this compile-only check.
Test binaries are still linked, so unresolved targets fail feedback. DWARF
debug data is omitted from compiler exports and the discarded binaries. Vet is
omitted in this lane because dependency vet results cannot be pre-seeded; the
full suite still runs `go vet ./...`.

Install matching tools and frozen dependencies before invoking feedback:

```sh
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
scripts/ci/fast-feedback.sh --working-tree
```

Frontend dependencies are only required when their domain is selected. Missing
dependencies or tool version mismatches fail with preparation instructions.
The helper never runs Node dependency installation or generation commands and
never auto-fixes source. Go compilation and pinned Actionlint can download
modules/toolchains and populate normal Go caches, so prepare
its dependencies before measuring a warm run.

Each CI runner starts a 220-second cumulative deadline before checkout and setup,
within its unchanged four-minute job limit. The application and tooling Go
runners restore the bounded compiler seed read-only, keyed by OS, architecture, runner image and pinned
module/tool manifests. Compatible fallback entries are revalidated by Go's
content-addressed build algorithm; every selected package still compiles on a
hit or miss. Feedback skips the bulk module archive and fetches modules
as needed. Cache misses can still exhaust the budget; successful cold hosted
completion is not guaranteed. The immutable integration merge supplies the
comparison base. Application Go, tooling Go and frontend checks run on separate
runners, each with the same cumulative budget. All three jobs must pass for a
successful workflow conclusion. Client checks
still finish before web checks. Every selected check must finish successfully.
An expired budget fails explicitly. Failed checks terminate remaining
descendants in their own runner; the other jobs continue independently. CI supervision requires Bash 5.1 and GNU timeout, both present on the
hosted Linux runner. Without `FAST_FEEDBACK_DEADLINE`, local use retains its
existing tools and sequential lanes with no imposed deadline. The default
`--lane=all` runs every selected check; unscoped `--lane=go` retains the complete
Go/tooling graph. Reproduce the three hosted selections with:

```sh
scripts/ci/fast-feedback.sh --lane=go --go-scope=application
scripts/ci/fast-feedback.sh --lane=go --go-scope=tooling
scripts/ci/fast-feedback.sh --lane=frontend
```

The application scope owns Go packages outside `scripts/` and module verification.
The tooling scope owns Go packages under `scripts/`, ShellCheck, Actionlint and
read-only documentation validation. Their package and check unions match the
unscoped Go lane. Unknown lanes/scopes fail, and frontend refuses Go scopes.
Changes to `scripts/ops/audit-collector/` or its
`docs/operations/audit-collector.md` runbook select the external receiver's
standard-library Python rehearsal in the tooling lane (`auditCollector` in the
plan). It uses disposable local fixtures, never a configured source or destination.
The mandatory
full-candidate lint job runs every refusal and deadline supervisor fixture;
per-commit feedback spends its budget on the selected source checks.

A pass means the listed checks completed. It does not cover full Go tests,
race/fuzz, generated freshness, Storybook, browser closure, services, native
releases, or Linux/platform acceptance. Full current-main candidate CI remains
required before merge. Runtime depends on scope, dependency readiness, and cold
compiler caches; the remote workflow owns its deadline. A five-minute local
completion guarantee is not claimed. Keep the existing signature pre-push hook;
this command is optional, with no additional mandatory expensive hooks.

Feedback uses the separate `go-feedback-v2` compact cache, prepared from trusted
main by `ci-feedback-backend-cache.yml`. Its application, CLI and tooling exports
use the same `-gcflags=all=-dwarf=false` compiler ABI as feedback. The existing
version-1 cache and its production-fixture consumers retain their compiler
defaults. Both publishers enforce the existing expanded size and file bounds.
PR jobs cannot publish either main cache. After merging a cache bootstrap, a
maintainer can prepare the new seed with:

```sh
gh workflow run ci-feedback-backend-cache.yml --repo Hikyo-Org/Hikyo --ref main
```

Verify the main-only seed job succeeds and publishes its exact revision key
before relying on a subsequent feedback cache restore. Publication and a warm
local run do not establish hosted feedback acceptance or merge.

## `preflight.sh` — pre-push gate

Runs the cheap, deterministic checks that account for most pre-merge CI reds,
fail-fast in cost order, so you hit a `tsc`/`vet`/DCO error in ~2 min locally
instead of ~8 min into a container job. Pins Node from `.nvmrc` via `fnm` and
gets `pnpm` through Corepack, matching CI's toolchain.

Before running it, fetch canonical main into `hikyo-upstream/main` using the
signing setup in `CONTRIBUTING.md`. Preflight refuses a missing canonical base
instead of comparing against a fork's `origin/main` or skipping DCO.

```
scripts/dev/preflight.sh          # DCO, gofmt, imports, build, vet, api-freeze, TS typechecks
scripts/dev/preflight.sh --full   # + generated-freshness + supply-chain fixtures
```

Continuous loop while editing:

```
while true; do scripts/dev/preflight.sh; read -rp 'enter to re-run'; done
```

## `ci-failed.sh` — triage a red run

Prints the failed leaf jobs and the tail of each failed step's log, so a red
22-min race shard doesn't mean clicking through the Actions UI.

```
scripts/dev/ci-failed.sh            # latest failed ci.yml run on this branch
scripts/dev/ci-failed.sh <run-id>
```

## Why this list (CI-failure data)

Sampled recent `ci.yml` failures. On feature branches (the reds a pre-check can
catch), deduped leaf failures were dominated by fast deterministic jobs:

| Failure          | Real cause                          | Caught by                          |
|------------------|-------------------------------------|------------------------------------|
| web              | `tsc --noEmit` type errors          | web typecheck stage                |
| dco              | missing `git commit -s`             | DCO stage                          |
| supply-chain     | chart / release fixture drift       | `--full` supply-chain stage        |
| client           | generate / typecheck / test         | clients/ts stage                   |
| lint             | shellcheck / actionlint / gofmt     | gofmt + imports stages             |
| generated        | stale generated files               | `--full` generated stage           |

## Deliberately excluded

The expensive or flaky tails are **not** in `preflight.sh`; run them on demand:

- **Playwright browser suite** (`web` job) — slow; the common failure is `tsc`,
  already covered. `cd web && pnpm run e2e` when you need it.
- **`go test -race ./...`** — the longest CI job. Its recurring failures are
  load flakes (see #747), not reproducible pre-checks.
- **`test_core`** (postgres-backed) — needs a `postgres:18` service; run with
  `HIKYO_TEST_POSTGRES_DSN=...` + `scripts/ci/test-core-packages.sh`.
- **`k8s-e2e`** — spins a kind cluster (`scripts/ci/k8s-e2e.sh`).
