# Release floor acceptance

Marc's [2026-08-20 amendment to #79](https://github.com/Hikyo-Org/Hikyo/issues/79#issuecomment-5354009024) makes constrained native arm64 CI the release floor. Physical Pi calibration is optional. A macOS arm64 test process, an unconstrained Linux job, and a Pi 5 result do not satisfy that gate by themselves.

## Run the recovery lane

Dispatch `.github/workflows/floor-acceptance.yml` at the reviewed release-candidate branch or tag. It checks out that exact revision, uses `ubuntu-24.04-arm`, and runs `scripts/ci/floor-acceptance.sh` under native arm64 Docker cgroup v2 limits: 4 CPUs, 4 GiB RAM, zero swap. Compilation happens before the measured container. The measured container has no external network, a read-only image, and disposable SQLite data on tmpfs.

Configure two **dedicated disposable TEST** GitHub repository secrets before dispatch. Never reuse production custody or release signing keys:

| Secret | Format | Mounted files |
|---|---|---|
| `FLOOR_BACKUP_CUSTODY` | JSON object with `identity` (the private X25519 age identity) and `recipient` (its matching public recipient) | `/custody/backup/identity`, `/custody/backup/recipient` |
| `FLOOR_ROOT_CUSTODY` | Independent random 32-byte root key encoded as 64 hexadecimal characters | `/custody/root/rootkey` |

The secrets are fetched independently and written as mode-0600 files in separate read-only mounts. Missing/malformed inputs fail; the gate cannot generate replacements or silently use ordinary test custody. These GitHub secrets exercise the specifically approved CI custody arrangement. They do not demonstrate independently administered production escrow systems. Both must be rotated independently if compromised.

The same script runs locally with those two environment variables and a new evidence directory as its only argument. It checks the Docker daemon architecture, Linux and cgroup v2; the test reads the effective cgroup limits again inside the measured runtime. Local native Linux arm64 Docker is harness validation; record a GitHub run at the release candidate for release acceptance.

## What the artifact proves

`floor-backup-restore-<commit>` includes:

- `provenance.json`: exact source commit, dirty-source marker, locally built image identity and run URL. Release evidence requires a clean source tree and the release candidate's exact commit.
- `result.json`: successful K2/K3 fixture and CLI runbook, effective CPU/RAM/swap limits, cgroup peak memory and OOM events, start time and elapsed time below the 30-minute RTO.
- `cli-drill.json`: the real binary's report including archive digest, schema version, value readability, per-principal reconciliation, machine credential mint/revoke, and RTO verdict.
- `tests.log` and `container-state.json`: full named fixture results and container exit/OOM status. A failed run can upload diagnostics but cannot produce a passing verdict.

The existing K2/K3 fixture seeds every named credential class and secret data, exports, explicitly destroys, restores, and verifies credential refusal, surviving data, truncation refusal, custody separation and individual reconciliation. A separate disposable database runs actual `hikyo backup export`, `restore drill`, `restore run`, `restore status`, and `restore reconcile` commands. The rehearsal checks decrypt and mint/revoke; the destructive restore leaves every other principal unreconciled.

## Separate evidence still required

This lane does not mark `OBL-OPERATOR-PI-FIT` complete. That gate requires a real controller reconciling under load inside the 200m CPU / 128 MiB operator limit. It also does not invent Pi CPU derating factors or replace the scanner's committed physical-Pi artifact (`internal/scanning/testdata/bench/pi-result.json`). The amendment mentions an O2 `floor-bench` job and committed derating factors, but neither is present at the inspected baseline `f4175a5dbffc63a5d1e34bf450a7a52956a54668`. Those benchmark/calibration claims must retain their own evidence.

A release reviewer must link the successful exact-candidate run, verify every named subtest executed, and reassert the self-hoster checklist. Workflow availability alone is not acceptance.
