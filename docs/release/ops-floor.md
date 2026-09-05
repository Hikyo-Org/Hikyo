# Operations floor acceptance

The [approved 2026-08-20 amendment](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5354009024) makes constrained native ARM64 CI release evidence. Physical Pi calibration is optional. O2 evidence combines the ordinary exact-candidate Go and race jobs, which execute the bound registry and both-engine refusal fixtures, with this explicitly measured operations lane. This lane does not claim every registry bound was benchmarked inside its container.

## Run both native architectures

`.github/workflows/ops-floor.yml` runs on pull requests touching its harness and supports manual dispatch at the reviewed release-candidate branch or tag. Pull requests check out `pull_request.head.sha`, not the synthetic merge commit. Both matrix legs must pass at the reviewed candidate:

| Native runner | Effective CPU quota | Memory | Swap | SQLite storage |
|---|---|---|---|---|
| `ubuntu-24.04-arm` | 4 CPUs | 4 GiB | zero | Private disposable host-backed directory |
| `ubuntu-24.04` | 2 CPUs | 4 GiB | zero | Private disposable host-backed directory |

The local equivalent is `scripts/ci/ops-floor.sh arm64 /absolute/new/evidence-directory`, or `amd64` on a native x86 Docker daemon. Docker must report Linux, the requested native architecture and cgroup v2. The measured runner independently verifies its architecture and effective cgroup CPU/memory/swap limits. Emulation, unlimited resources, skipped selected tests, OOM events and stale evidence destinations refuse acceptance.

Compilation happens outside the measured container. The source-built non-race CLI, isolation fixture and admission tests execute inside the same constrained container. Race validation remains a separate ordinary CI requirement. The image is read-only, runs as the caller's UID/GID and has no external network. Database and test credentials live only in a new mode-0700 disposable directory, which is removed after the owned container exits. Disk-backed SQLite lets the kernel reclaim file cache instead of charging persistent database and WAL bytes as unreclaimable tmpfs memory.

## Exact measured scope

The doctor checklist now includes all twelve baseline families: retention, project storage, backup RPO, restore drill, adapters, datastore volume, current root escrow, pin expiry, root rotation, re-encryption, database durability and Argon2 policy. The fixture runs the actual local escrow verification command with a distinct private custody copy before its healthy state. Both-engine isolation tests additionally prove escrow epoch/incarnation invalidation, pin tier boundaries and actual re-encryption completion. Passing this lane alone does not close the remaining deployment and bound-registry evidence requirements.

`TestOpsFloorDoctor` boots the actual application using its existing signed development fixture and invokes the real `hikyo doctor` binary with a genuine privately persisted human session. It uses the production Argon2 floor, admission budget and backup policy defaults. The following states come from persisted service data, not mocked HTTP responses:

| State | Required finding | Severity and CLI result |
|---|---|---|
| Healthy | All twelve baseline finding families | All `ok`, exit 0 |
| Stale prune | `retention-prune` | `warn`, exit 0 |
| Backup beyond the default 26-hour RPO | `backup-rpo` | `error`, exit 4 |
| Failed restore drill | `restore-drill` | `warn`, exit 0 |
| Failed adapter target | `adapter-targets` | `warn`, exit 0 |
| Expired SAML metadata | `metadata_expired` | `error`, exit 4 |
| Project payload at the real 1 GiB threshold | `project-storage` | `warn`, exit 0 |
| Recovered | All twelve baseline finding families | All `ok`, exit 0 |

The SQLite instance-wide aggregates project byte lengths before grouping. `LIMIT -1 OFFSET 0` preserves every row and prevents SQLite from flattening that projection into a sorter carrying whole ciphertext blobs ([SQLite optimizer rule 14](https://www.sqlite.org/optoverview.html#flattening)). The pinned SQLC parser cannot generate `AS MATERIALIZED`; the projection barrier keeps generation reproducible. Tenant grouping, totals, PostgreSQL queries and transaction deadlines remain unchanged.

The 1 GiB state uses 16,384 synthetic 64 KiB accounting rows, inserted in bounded batches. These rows are never decrypted and do not represent valid published ciphertext. The test clears them and checks that the warning clears through the actual service and CLI. Backup health timestamps are synthetic status records; this lane does not export or restore a real archive. The separate [recovery lane](floor-acceptance.md) proves that behavior.

`TestOpsFloorStorageRefusal` also runs the existing SQLite project storage refusal fixture. That fixture uses an injected small refusal threshold to keep the test bounded; the ordinary bound registry pins the production 4 GiB value. The measured admission subset checks derived concurrency, boot refusal when one verification cannot fit, semaphore concurrency, queue depth, per-IP window, account backoff and bounded limiter state. Remaining named bounds, PostgreSQL coverage and race checks retain their ordinary CI evidence.

## Review the artifact

`ops-floor-<architecture>-<commit>` contains `provenance.json`, `result.json`, `doctor.json`, named test logs, runner diagnostics and container exit/OOM state. Provenance records the checked-out commit, source-dirty marker, binary hashes, image identity, native architecture, storage medium and workflow run URL. The result records effective limits, peak memory, elapsed time and every selected test/state. The verifier checks individual finding severities and requires every mutable fixture state to recover; an aggregate status alone is insufficient. It independently measures the actual SQLite filesystem. A local host already above 80/90 percent retains its real volume warning/error and corresponding CLI result, even after fixture recovery. Such a run proves correct degraded-host behavior and cannot be presented as healthy capacity. No threshold is lowered and PostgreSQL capacity remains explicitly unknown in its separate deployment check.

Only a clean-source successful hosted run at the exact reviewed candidate is release acceptance. A local dirty-tree run is a harness rehearsal even when its cgroup checks pass. Failed runs may upload diagnostics but cannot produce a successful result. No credentials, database or custody files belong in the artifact.

This evidence does not assert physical Pi identity, a CPU derating factor, production release signatures, K2/K3 recovery, or the separate 200m CPU / 128 MiB Kubernetes operator fit. Scanner calibration retains its committed [physical Pi artifact](../../internal/scanning/testdata/bench/pi-result.json). Link the two successful native workflow legs together with the ordinary exact-candidate jobs in the release review; workflow availability alone is not acceptance.
