# Local developer scripts

Fast local feedback for the checks CI actually fails on. Not a full CI mirror —
`act` was assessed and rejected (its runner image differs from `ubuntu-latest`,
`no-egress`/`k8s-e2e`/service jobs don't work under nested Docker, and a full
run is slower than real CI while giving less trustworthy results).

## `preflight.sh` — pre-push gate

Runs the cheap, deterministic checks that account for most pre-merge CI reds,
fail-fast in cost order, so you hit a `tsc`/`vet`/DCO error in ~2 min locally
instead of ~8 min into a container job. Pins Node from `.nvmrc` via `fnm` and
gets `pnpm` through Corepack, matching CI's toolchain.

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
