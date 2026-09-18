# Lint loader memory — 20GB → 1.2GB

## Symptom

`lint.test` showed **20.40 GB** in macOS Activity Monitor's "Memory" column
(1.57 GB "Real Mem"). The custom static analyzers in `internal/lint` load the
whole module through `golang.org/x/tools/go/packages`, and the load was the
hog.

## Root cause

`internal/lint/lint.go` built its `packages.Config.Mode` as:

```
NeedName | NeedFiles | NeedSyntax | NeedTypes | NeedTypesInfo | NeedImports | NeedDeps
```

`NeedDeps` combined with `NeedSyntax|NeedTypesInfo` forces
**source type-checking of the entire transitive dependency closure** (pgx,
x/tools, stdlib): every dependency's AST is parsed, a full `types.Info` is built
for each, and all of it stays resident. That was a ~7GB live set for this
package's test suite.

The 20GB figure is that ~7GB inflated by `-race` shadow memory (sparse, but
counted) plus macOS compression/swap accounting in the "Memory" column — not
real committed RAM.

## Fix

Drop `NeedDeps` from every load. A root package's direct imports still get their
`.Types` from **compiler export data** as a side effect of type-checking the
root — sufficient for analyzers that reason about type identity (`authz.Proof`,
driver handles). The closure is never source-loaded.

- `internal/lint/lint.go` — mode drops `NeedDeps`; `LoadRepo` and the
  negative-fixture `Load` share the single mode. Doc comment records the
  mechanism and the export-data/`flatten` seam.
- `internal/lint/forgeguard.go` — added `if p.TypesInfo == nil { continue }`.
  `CheckProofForgery` is the only analyzer that walks `TypesInfo.Uses` on every
  package rather than selecting its target packages by path first, so under the
  new mode it hit a non-root import (nil `TypesInfo`) and panicked — the
  `badnil` fixture caught it. The other `flatten` walkers (fence, proofsig,
  redaction, grantlock, txresult, appendonly, handles) select roots by path or
  guard on `p.Types`, so they were already safe.
- Deleted `internal/lint/nodeps_test.go` — redundant once every `Catches*`
  fixture test runs through the deps=false path.

## Measurements (peak RSS)

| | original (`NeedDeps`) | intermediate¹ | now |
|---|---|---|---|
| `go test ./internal/lint` | 7GB / 104s | 3.0GB / 20s | **1.20GB / 8s** |
| `-race` ×2 | — | 5.1–5.7GB / 78–120s | **3.4–3.8GB / 22–24s** (both pass) |
| `GOMEMLIMIT=1500MiB` (non-race) | — | 1.99GB / 33s | **1.21GB / 6.3s** |
| `go test ./internal/isolation` (real CI consumer) | — | — | **2.16GB / 194s** |

¹ deps=false in `LoadRepo` only; the negative fixtures still carried the full
closure.

## Parallel-run memory

- `GOMEMLIMIT` is now a **no-op** for non-race lint (1.21 vs 1.20 GB default) —
  the live set already sits under any sane cap. Do **not** export a 1.5Gi cap in
  `scripts/ci/test-race-packages.sh`: under `-race` the live set is ~3.4GB, so
  the cap would thrash CPU instead of saving memory.
- The lever for local parallel runs is `-p N` (CI already pins `-p 2`). `-race`
  carries an inherent ~3× multiplier from its shadow-memory model — inherent,
  not tunable.

## Reproduce

```
/usr/bin/time -l go test ./internal/lint -count=1 2>&1 | grep 'maximum resident'
/usr/bin/time -l go test -race ./internal/lint -count=1 2>&1 | grep 'maximum resident'
```
